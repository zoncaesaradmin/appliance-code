package identity

import (
	"context"
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
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
