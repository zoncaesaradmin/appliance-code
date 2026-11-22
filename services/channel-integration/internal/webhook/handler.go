package webhook

import (
	"appliance-code/services/channel-integration/internal/meta"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const maxWebhookBytes = 1 << 20

type Logger interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
	Errorw(string, ...interface{})
}
type MessageSender interface {
	SendText(context.Context, string, string) (*meta.SendMessageResponse, error)
}
type Handler struct {
	verifyToken, appSecret string
	sender                 MessageSender
	logger                 Logger
	now                    func() time.Time
}

func NewHandler(verifyToken, appSecret string, sender MessageSender, logger Logger) *Handler {
	if logger == nil {
		panic("channel integration logger is required")
	}
	return &Handler{verifyToken: verifyToken, appSecret: appSecret, sender: sender, logger: logger, now: time.Now}
}
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /webhook", h.handleVerify)
	mux.HandleFunc("POST /webhook", h.handleEvent)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
}
func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
func (h *Handler) handleVerify(w http.ResponseWriter, r *http.Request) {
	valid := r.URL.Query().Get("hub.mode") == "subscribe" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("hub.verify_token")), []byte(h.verifyToken)) == 1
	challenge := r.URL.Query().Get("hub.challenge")
	if !valid || challenge == "" {
		h.logger.Warnw("webhook verification rejected", "event", "webhook_verification_rejected")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.logger.Infow("webhook verification accepted", "event", "webhook_verification_accepted")
	_, _ = w.Write([]byte(challenge))
}
func (h *Handler) handleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "bad request", http.StatusBadRequest)
		}
		return
	}
	if err := VerifySignature(h.appSecret, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		h.logger.Warnw("webhook signature rejected", "event", "webhook_signature_rejected")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var payload meta.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, message := range change.Value.Messages {
				if message.Type != "text" || message.Text == nil || message.From == "" || h.sender == nil {
					continue
				}
				reply := "Thanks for your message. Current server time is " + h.now().UTC().Format(time.RFC3339) + "."
				if _, err := h.sender.SendText(r.Context(), message.From, reply); err != nil {
					h.logger.Errorw("webhook reply failed", "event", "webhook_reply_failed", "provider", "meta")
				}
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}
