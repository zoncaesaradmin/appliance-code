package identity

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const SessionCookieName = "__Host-app_session"

// RequestLimiter must enforce an independent limit for each action and key.
type RequestLimiter interface {
	Allow(context.Context, string, string) (bool, error)
}

// ClientKey obtains a rate-limit key from a trusted transport boundary. It
// must not trust browser-supplied forwarding headers without a trusted proxy.
type ClientKey func(*http.Request) (string, error)

type HTTPHandler struct {
	Service       *Service
	Limiter       RequestLimiter
	ClientKey     ClientKey
	AllowedOrigin string
	InternalKey   []byte
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/internal/v1/application-principal/resolve" {
		h.resolveChannelPrincipal(w, r)
		return
	}
	if r.URL.Path == "/api/shared/v1/onboarding/nearby" {
		h.nearbyZones(w, r)
		return
	}
	if r.URL.Path == "/api/shared/v1/onboarding/join" {
		h.joinZone(w, r)
		return
	}
	if r.URL.Path == "/api/shared/v1/onboarding/zone-requests" {
		h.requestZoneCreation(w, r)
		return
	}
	if r.URL.Path != "/api/shared/v1/auth/otp/challenges" && r.URL.Path != "/api/shared/v1/auth/otp/verify" && r.URL.Path != "/api/shared/v1/auth/logout" && r.URL.Path != "/api/shared/v1/auth/session" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/api/shared/v1/auth/session" {
		h.sessionStatus(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	if h.Service == nil || h.Limiter == nil || h.ClientKey == nil || h.AllowedOrigin == "" {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	if r.Header.Get("Origin") != h.AllowedOrigin {
		authError(w, http.StatusForbidden, "Request origin is not allowed.")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		authError(w, http.StatusUnsupportedMediaType, "Send JSON content.")
		return
	}
	if r.URL.Path == "/api/shared/v1/auth/logout" {
		var input struct{}
		if !readAuthJSON(w, r, &input) {
			return
		}
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil && !errors.Is(err, http.ErrNoCookie) {
			authError(w, http.StatusBadRequest, "Invalid session cookie.")
			return
		}
		if cookie != nil {
			if err := h.Service.SignOut(r.Context(), cookie.Value); err != nil {
				authError(w, http.StatusServiceUnavailable, "Could not sign out. Try again.")
				return
			}
		}
		http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		writeAuthJSON(w, http.StatusOK, struct{}{})
		return
	}
	key, err := h.ClientKey(r)
	if err != nil || key == "" {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	allowed, err := h.Limiter.Allow(r.Context(), key, r.URL.Path)
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	if !allowed {
		authError(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
		return
	}
	if r.URL.Path == "/api/shared/v1/auth/otp/challenges" {
		var input struct {
			Phone string `json:"phone"`
		}
		if !readAuthJSON(w, r, &input) {
			return
		}
		id, err := h.Service.Begin(r.Context(), input.Phone, key)
		if errors.Is(err, ErrInvalidPhone) {
			authError(w, http.StatusBadRequest, "Enter a valid mobile number with country code.")
			return
		}
		if errors.Is(err, ErrRateLimited) {
			authError(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
			return
		}
		if err != nil {
			authError(w, http.StatusServiceUnavailable, "Could not send a code. Try again.")
			return
		}
		writeAuthJSON(w, http.StatusCreated, struct {
			ChallengeID string `json:"challenge_id"`
		}{ChallengeID: id})
		return
	}
	var input struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if !readAuthJSON(w, r, &input) {
		return
	}
	token, expires, err := h.Service.Verify(r.Context(), input.ChallengeID, input.Code)
	if errors.Is(err, ErrInvalidChallenge) {
		authError(w, http.StatusUnauthorized, "The code is invalid or expired.")
		return
	}
	if errors.Is(err, ErrRateLimited) {
		authError(w, http.StatusTooManyRequests, "Too many attempts. Try again later.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Could not verify the code. Try again.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: expires,
	})
	writeAuthJSON(w, http.StatusOK, struct{}{})
}

// resolveChannelPrincipal is deliberately outside the public /api/shared
// surface. It requires a target-generated key and accepts only the generic
// channel contract; provider-specific validation remains in the adapter.
func (h HTTPHandler) resolveChannelPrincipal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	if len(h.InternalKey) < 32 || !validInternalKey(r.Header.Get("X-Application-Identity-Key"), h.InternalKey) {
		authError(w, http.StatusUnauthorized, "Internal identity authentication required.")
		return
	}
	if h.Service == nil {
		authError(w, http.StatusServiceUnavailable, "Identity resolution is unavailable.")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		authError(w, http.StatusUnsupportedMediaType, "Send JSON content.")
		return
	}
	var input ChannelSessionRequest
	if !readAuthJSON(w, r, &input) {
		return
	}
	token, principal, discoveryURL, err := h.Service.ResolveChannelDiscovery(r.Context(), input)
	if errors.Is(err, ErrUnknownChannel) || errors.Is(err, ErrSubjectNotLinked) {
		authError(w, http.StatusForbidden, "The channel subject is not eligible.")
		return
	}
	if errors.Is(err, ErrSubjectBlocked) {
		authError(w, http.StatusForbidden, "The channel subject is blocked.")
		return
	}
	if errors.Is(err, ErrInvalidPhone) {
		authError(w, http.StatusBadRequest, "The identity phone is invalid.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Identity resolution is unavailable.")
		return
	}
	writeAuthJSON(w, http.StatusCreated, struct {
		SessionToken string    `json:"session_token"`
		Principal    Principal `json:"principal"`
		DiscoveryURL string    `json:"discovery_url,omitempty"`
	}{SessionToken: token, Principal: principal, DiscoveryURL: discoveryURL})
}

func (h HTTPHandler) onboardingRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return false
	}
	if h.Service == nil || h.AllowedOrigin == "" || r.Header.Get("Origin") != h.AllowedOrigin {
		authError(w, http.StatusForbidden, "Request origin is not allowed.")
		return false
	}
	if h.Limiter == nil || h.ClientKey == nil {
		authError(w, http.StatusServiceUnavailable, "Onboarding is unavailable.")
		return false
	}
	key, err := h.ClientKey(r)
	if err != nil || key == "" {
		authError(w, http.StatusServiceUnavailable, "Onboarding is unavailable.")
		return false
	}
	allowed, err := h.Limiter.Allow(r.Context(), key, r.URL.Path)
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Onboarding is unavailable.")
		return false
	}
	if !allowed {
		authError(w, http.StatusTooManyRequests, "Too many requests. Try again later.")
		return false
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		authError(w, http.StatusUnsupportedMediaType, "Send JSON content.")
		return false
	}
	return true
}

func (h HTTPHandler) onboardingCredentials(r *http.Request, input struct {
	DiscoveryToken string `json:"discovery_token"`
}) string {
	if input.DiscoveryToken != "" {
		return input.DiscoveryToken
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h HTTPHandler) nearbyZones(w http.ResponseWriter, r *http.Request) {
	if !h.onboardingRequest(w, r) {
		return
	}
	var input struct {
		DiscoveryToken string  `json:"discovery_token"`
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		RadiusKM       float64 `json:"radius_km"`
	}
	if !readAuthJSON(w, r, &input) {
		return
	}
	if input.RadiusKM == 0 {
		input.RadiusKM = maxNearbyRadiusKM
	}
	zones, principal, err := h.Service.Nearby(r.Context(), h.onboardingCredentials(r, struct {
		DiscoveryToken string `json:"discovery_token"`
	}{input.DiscoveryToken}), input.DiscoveryToken, input.Latitude, input.Longitude, input.RadiusKM)
	if errors.Is(err, ErrInvalidLocation) {
		authError(w, http.StatusBadRequest, "Send a valid location within the search radius.")
		return
	}
	if errors.Is(err, ErrUnauthenticated) {
		authError(w, http.StatusUnauthorized, "This link has expired. Start again from your channel.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Nearby zone discovery is unavailable.")
		return
	}
	writeAuthJSON(w, http.StatusOK, struct {
		Zones           []Zone `json:"zones"`
		ApplicationUser string `json:"application_user_id"`
		Audience        string `json:"audience"`
	}{Zones: zones, ApplicationUser: principal.ApplicationUserID, Audience: principal.Audience})
}

func (h HTTPHandler) joinZone(w http.ResponseWriter, r *http.Request) {
	if !h.onboardingRequest(w, r) {
		return
	}
	var input struct {
		DiscoveryToken string  `json:"discovery_token"`
		ZoneID         string  `json:"zone_id"`
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
	}
	if !readAuthJSON(w, r, &input) {
		return
	}
	// A discovery link is enough to inspect nearby public zones, but joining
	// requires an application login session. This keeps channel identity and
	// authenticated membership requests separate.
	request, _, err := h.Service.RequestJoin(r.Context(), h.sessionCookie(r), input.ZoneID, input.Latitude, input.Longitude)
	if errors.Is(err, ErrInvalidZone) {
		authError(w, http.StatusBadRequest, "Choose a valid zone and share your location.")
		return
	}
	if errors.Is(err, ErrJoinTooFar) {
		authError(w, http.StatusForbidden, "You are outside this Zonity's join radius.")
		return
	}
	if errors.Is(err, ErrUnauthenticated) {
		authError(w, http.StatusUnauthorized, "This link has expired. Start again from your channel.")
		return
	}
	if errors.Is(err, ErrJoinExists) {
		writeAuthJSON(w, http.StatusOK, request)
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Could not submit the zone request.")
		return
	}
	writeAuthJSON(w, http.StatusCreated, request)
}

func (h HTTPHandler) requestZoneCreation(w http.ResponseWriter, r *http.Request) {
	if !h.onboardingRequest(w, r) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !readAuthJSON(w, r, &input) {
		return
	}
	request, _, err := h.Service.RequestZoneCreation(r.Context(), h.sessionCookie(r), input.Name)
	if errors.Is(err, ErrInvalidZoneName) {
		authError(w, http.StatusBadRequest, "Enter a zone name between 2 and 100 characters.")
		return
	}
	if errors.Is(err, ErrUnauthenticated) {
		authError(w, http.StatusUnauthorized, "Sign in before requesting a new Zonity.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Could not submit the Zonity request.")
		return
	}
	writeAuthJSON(w, http.StatusCreated, request)
}

func (h HTTPHandler) sessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func validInternalKey(encoded string, expected []byte) bool {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	return err == nil && subtle.ConstantTimeCompare(decoded, expected) == 1
}

// sessionStatus exposes only whether this cookie is currently valid. It does
// not return a phone number, token, user ID, staff grants, or session lifetime.
func (h HTTPHandler) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	if r.URL.RawQuery != "" {
		authError(w, http.StatusBadRequest, "Invalid request.")
		return
	}
	if h.Service == nil {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	cookie, err := r.Cookie(SessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		authError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	if err != nil {
		authError(w, http.StatusBadRequest, "Invalid session cookie.")
		return
	}
	_, err = h.Service.UserID(r.Context(), cookie.Value)
	if errors.Is(err, ErrUnauthenticated) {
		authError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	writeAuthJSON(w, http.StatusOK, struct {
		Authenticated bool `json:"authenticated"`
	}{true})
}

func readAuthJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		authError(w, http.StatusBadRequest, "Invalid sign-in request.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		authError(w, http.StatusBadRequest, "Invalid sign-in request.")
		return false
	}
	return true
}

func authError(w http.ResponseWriter, status int, message string) {
	writeAuthJSON(w, status, struct {
		Message string `json:"message"`
	}{Message: message})
}

func writeAuthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
