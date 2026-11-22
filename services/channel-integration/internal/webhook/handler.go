package webhook

import (
	"appliance-code/services/channel-integration/internal/meta"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
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
	query := r.URL.Query()
	mode := query.Get("hub.mode")
	verifyToken := query.Get("hub.verify_token")
	challenge := query.Get("hub.challenge")
	modeValid := mode == "subscribe"
	tokenMatches := subtle.ConstantTimeCompare([]byte(verifyToken), []byte(h.verifyToken)) == 1
	challengePresent := challenge != ""
	valid := modeValid && tokenMatches && challengePresent
	if !valid || challenge == "" {
		h.logger.Warnw("webhook verification rejected",
			"event", "webhook_verification_rejected",
			"method", r.Method,
			"path", r.URL.Path,
			"mode", mode,
			"mode_valid", modeValid,
			"verify_token_present", verifyToken != "",
			"verify_token_matches", tokenMatches,
			"challenge_present", challengePresent,
		)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.logger.Infow("webhook verification accepted",
		"event", "webhook_verification_accepted",
		"method", r.Method,
		"path", r.URL.Path,
		"mode", mode,
		"challenge_present", challengePresent,
	)
	_, _ = w.Write([]byte(challenge))
}
func (h *Handler) handleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := readBoundedBody(w, r)
	if err != nil {
		h.logger.Warnw("webhook request rejected", "event", "webhook_request_rejected", "reason", "body_read_failed")
		return
	}
	if err := VerifySignature(h.appSecret, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		h.logger.Warnw("webhook signature rejected", "event", "webhook_signature_rejected", "signature_present", strings.TrimSpace(r.Header.Get("X-Hub-Signature-256")) != "")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var payload meta.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Warnw("webhook payload rejected", "event", "webhook_payload_rejected", "reason", "invalid_json")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.logger.Infow("webhook received", "event", "webhook_received", "kind", classify(payload))
	summary := h.replyToTextMessages(r.Context(), payload)
	h.logger.Infow("webhook processed", "event", "webhook_processed", "kind", classify(payload), "text_messages", summary.textMessages, "other_messages", summary.otherMessages, "reply_attempts", summary.replyAttempts, "replies_sent", summary.repliesSent, "replies_failed", summary.repliesFailed, "replies_skipped", summary.repliesSkipped)
	w.WriteHeader(http.StatusOK)
}

func readBoundedBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return nil, err
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, err
	}
	return body, nil
}

type replySummary struct {
	textMessages   int
	otherMessages  int
	replyAttempts  int
	repliesSent    int
	repliesFailed  int
	repliesSkipped int
}

func (h *Handler) replyToTextMessages(ctx context.Context, payload meta.WebhookPayload) replySummary {
	var summary replySummary
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, message := range change.Value.Messages {
				if message.Type != "text" || message.Text == nil || message.From == "" {
					summary.otherMessages++
					continue
				}
				summary.textMessages++
				if h.sender == nil {
					summary.repliesSkipped++
					continue
				}
				summary.replyAttempts++
				reply := "Thanks for your message. Current server time is " + h.now().UTC().Format(time.RFC3339) + "."
				response, err := h.sender.SendText(ctx, message.From, reply)
				if err != nil {
					summary.repliesFailed++
					errorKind, statusCode := safeDeliveryError(err)
					h.logger.Errorw("webhook reply failed", "event", "webhook_reply_failed", "provider", "meta", "error_kind", errorKind, "http_status", statusCode)
					continue
				}
				summary.repliesSent++
				providerMessages := 0
				if response != nil {
					providerMessages = len(response.Messages)
				}
				h.logger.Infow("webhook reply sent", "event", "webhook_reply_sent", "provider", "meta", "provider_messages", providerMessages)
			}
		}
	}
	if summary.repliesSkipped > 0 {
		h.logger.Warnw("webhook reply skipped", "event", "webhook_reply_skipped", "reason", "sender_unavailable", "replies_skipped", summary.repliesSkipped)
	}
	return summary
}

func classify(payload meta.WebhookPayload) string {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if len(change.Value.Messages) > 0 {
				return "user_message"
			}
		}
	}
	return "other"
}

func safeDeliveryError(err error) (string, int) {
	if err == nil {
		return "", 0
	}
	var deliveryErr *meta.DeliveryError
	if errors.As(err, &deliveryErr) {
		return deliveryErr.Kind, deliveryErr.StatusCode
	}
	return "unknown", 0
}
