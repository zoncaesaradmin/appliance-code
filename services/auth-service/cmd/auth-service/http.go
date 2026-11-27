package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const sessionCookieName = "__Host-app_session"

type authHandler struct {
	store            *authStore
	sender           sender
	otpKey           []byte
	origin           string
	ready            *atomic.Bool
	inbox            *localInbox
	deliveryChannels []string
}

func (h authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/health/live":
		w.WriteHeader(http.StatusOK)
		return
	case "/health/ready":
		if h.ready != nil && h.ready.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		}
		return
	case "/api/v1/auth/capabilities":
		writeJSON(w, http.StatusOK, map[string]any{"service": "auth-service", "authentication": map[string]any{"methods": []string{"otp"}, "deliveryChannels": h.deliveryChannels}})
		return
	case "/api/shared/v1/auth/principal":
		h.principal(w, r)
		return
	case "/api/shared/v1/auth/session":
		h.session(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/shared/v1/auth/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	if h.origin == "" || r.Header.Get("Origin") != h.origin {
		authError(w, http.StatusForbidden, "Request origin is not allowed.")
		return
	}
	if r.URL.Path == "/api/shared/v1/auth/logout" {
		if err := h.store.revokeSession(r.Context(), cookieValue(r), time.Now()); err != nil {
			authError(w, 503, "Could not sign out. Try again.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		writeJSON(w, 200, struct{}{})
		return
	}
	client := r.RemoteAddr
	if host, _, err := net.SplitHostPort(client); err == nil {
		client = host
	}
	if r.URL.Path == "/api/shared/v1/auth/otp/challenges" {
		var in struct {
			Phone            string   `json:"phone"`
			DeliveryChannels []string `json:"delivery_channels,omitempty"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		id, err := h.begin(r.Context(), in.Phone, client, in.DeliveryChannels)
		if errors.Is(err, errInvalidPhone) {
			authError(w, 400, "Enter a valid mobile number with country code.")
			return
		}
		if errors.Is(err, errRateLimited) {
			authError(w, 429, "Too many attempts. Try again later.")
			return
		}
		if err != nil {
			authError(w, 503, "Could not send a code. Try again.")
			return
		}
		writeJSON(w, 201, map[string]string{"challenge_id": id})
		return
	}
	if r.URL.Path != "/api/shared/v1/auth/otp/verify" {
		http.NotFound(w, r)
		return
	}
	var in struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	token, err := h.verify(r.Context(), in.ChallengeID, in.Code)
	if errors.Is(err, errInvalidChallenge) {
		authError(w, 401, "The code is invalid or expired.")
		return
	}
	if err != nil {
		authError(w, 503, "Could not verify the code. Try again.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
	writeJSON(w, 200, struct{}{})
}

func (h authHandler) principal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		authError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	const prefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, prefix) {
		authError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	if token == "" || h.store.validSession(r.Context(), token, time.Now()) != nil {
		authError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var userID, tenantID, channelID, audience, authMethod string
	err := h.store.db.QueryRowContext(r.Context(), `SELECT s.user_id, COALESCE(s.tenant_id, 'zonited-solutions'), COALESCE(s.channel_id, 'customer-web'), COALESCE(s.audience, 'customer'), COALESCE(s.auth_method, 'mobile_otp') FROM identity_sessions s WHERE s.token_hash=$1 AND s.expires_at>$2 AND s.revoked_at IS NULL`, sessionTokenHash(token), time.Now()).Scan(&userID, &tenantID, &channelID, &audience, &authMethod)
	if errors.Is(err, sql.ErrNoRows) {
		authError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	if err != nil {
		authError(w, http.StatusServiceUnavailable, "Sign-in is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application_user_id": userID, "tenant_id": tenantID, "channel_id": channelID, "audience": audience, "auth_method": authMethod, "roles": []string{}, "permissions": []string{}, "zone_scopes": []string{}})
}
func (h authHandler) begin(ctx context.Context, phone, client string, channels []string) (string, error) {
	if !phonePattern.MatchString(phone) {
		return "", errInvalidPhone
	}
	id, err := newChallengeID()
	if err != nil {
		return "", err
	}
	code, err := newCode()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(5 * time.Minute)
	if err := h.store.createChallenge(ctx, id, phone, createChallenge(h.otpKey, id, code), expires, client); err != nil {
		return "", err
	}
	if err := h.sender.SendOTP(ctx, otpRequest{ChallengeID: id, Phone: phone, Code: code, Factor: "otp", DeliveryChannels: channels, Purpose: "login", OriginChannel: "web", Audience: "customer", TenantID: "zonited-solutions", ExpiresAt: expires}); err != nil {
		_ = h.store.revokeChallenge(ctx, id)
		return "", errDelivery
	}
	return id, nil
}
func (h authHandler) verify(ctx context.Context, id, code string) (string, error) {
	if !challengePattern.MatchString(id) || !codePattern.MatchString(code) {
		return "", errInvalidChallenge
	}
	return h.store.verify(ctx, id, createChallenge(h.otpKey, id, code), time.Now(), time.Now().Add(30*24*time.Hour))
}
func (h authHandler) session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		authError(w, 405, "Method not allowed.")
		return
	}
	if cookieValue(r) == "" || h.store.validSession(r.Context(), cookieValue(r), time.Now()) != nil {
		authError(w, 401, "Sign in first.")
		return
	}
	writeJSON(w, 200, map[string]bool{"authenticated": true})
}
func cookieValue(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		authError(w, 415, "Send JSON content.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		authError(w, 400, "Invalid sign-in request.")
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		authError(w, 400, "Invalid sign-in request.")
		return false
	}
	return true
}
func authError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
