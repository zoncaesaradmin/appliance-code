package httpapi

import (
	"net/http"
	"strings"
	"time"
)

const videoPlaybackCookieName = "appliance_video_playback"

const videoPlaybackCookiePath = "/api/v1/video/stream/"

func setVideoPlaybackCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     videoPlaybackCookieName,
		Value:    token,
		Path:     videoPlaybackCookiePath,
		Expires:  expiresAt.UTC(),
		MaxAge:   maxCookieAge(expiresAt),
		HttpOnly: true,
		Secure:   requestUsesTLS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearVideoPlaybackCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     videoPlaybackCookieName,
		Value:    "",
		Path:     videoPlaybackCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesTLS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func requestUsesTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func maxCookieAge(expiresAt time.Time) int {
	seconds := int(time.Until(expiresAt).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
