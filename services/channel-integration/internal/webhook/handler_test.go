package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"appliance-code/services/channel-integration/internal/identityclient"
	"appliance-code/services/channel-integration/internal/meta"
)

type senderStub struct {
	response *meta.SendMessageResponse
	err      error
	body     string
}

type resolverStub struct {
	principal identityclient.Principal
	err       error
}

func (s resolverStub) Resolve(context.Context, string, string) (identityclient.Principal, error) {
	return s.principal, s.err
}

func (s *senderStub) SendText(_ context.Context, _, body string) (*meta.SendMessageResponse, error) {
	s.body = body
	return s.response, s.err
}

type capturedLog struct {
	message string
	fields  []interface{}
}

type captureLogger struct {
	entries []capturedLog
}

func (l *captureLogger) Infow(message string, fields ...interface{}) {
	l.entries = append(l.entries, capturedLog{message: message, fields: fields})
}

func (l *captureLogger) Warnw(message string, fields ...interface{}) {
	l.entries = append(l.entries, capturedLog{message: message, fields: fields})
}

func (l *captureLogger) Errorw(message string, fields ...interface{}) {
	l.entries = append(l.entries, capturedLog{message: message, fields: fields})
}

func (l *captureLogger) hasEvent(event string) bool {
	for _, entry := range l.entries {
		for i := 0; i+1 < len(entry.fields); i += 2 {
			if entry.fields[i] == "event" && entry.fields[i+1] == event {
				return true
			}
		}
	}
	return false
}

func (l *captureLogger) hasField(key string, want interface{}) bool {
	for _, entry := range l.entries {
		for i := 0; i+1 < len(entry.fields); i += 2 {
			if entry.fields[i] == key && entry.fields[i+1] == want {
				return true
			}
		}
	}
	return false
}

func TestHandleVerifyLogsSafeRejectionDetails(t *testing.T) {
	logger := &captureLogger{}
	handler := NewHandler("expected-token", "app-secret", &senderStub{}, logger)
	request := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode=subscribe&hub.verify_token=wrong-token&hub.challenge=challenge", nil)
	recorder := httptest.NewRecorder()

	handler.handleVerify(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if !logger.hasEvent("webhook_verification_rejected") {
		t.Fatalf("missing rejection log: %#v", logger.entries)
	}
	if !logger.hasField("verify_token_present", true) || !logger.hasField("verify_token_matches", false) {
		t.Fatalf("missing safe verification details: %#v", logger.entries)
	}
	if strings.Contains(fmt.Sprint(logger.entries), "wrong-token") {
		t.Fatal("verification token leaked into log fields")
	}
}

func TestHandleEventLogsReferenceLifecycle(t *testing.T) {
	logger := &captureLogger{}
	handler := NewHandler("expected-token", "app-secret", &senderStub{response: &meta.SendMessageResponse{}}, logger)
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"messages":[{"from":"919999999999","type":"text","text":{"body":"private message"}}]}}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set("X-Hub-Signature-256", signature("app-secret", body))
	recorder := httptest.NewRecorder()

	handler.handleEvent(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, event := range []string{"webhook_received", "webhook_reply_sent", "webhook_processed"} {
		if !logger.hasEvent(event) {
			t.Fatalf("missing %s log: %#v", event, logger.entries)
		}
	}
	if !logger.hasField("kind", "user_message") || !logger.hasField("text_messages", 1) || !logger.hasField("replies_sent", 1) || !logger.hasField("replies_failed", 0) {
		t.Fatalf("missing lifecycle counts: %#v", logger.entries)
	}
	if strings.Contains(fmt.Sprint(logger.entries), "919999999999") || strings.Contains(fmt.Sprint(logger.entries), "private message") {
		t.Fatal("personal data appeared in logs")
	}
}

func TestHandleEventResolvesGenericPrincipalWithoutLoggingSender(t *testing.T) {
	logger := &captureLogger{}
	handler := NewHandler("expected-token", "app-secret", &senderStub{response: &meta.SendMessageResponse{}}, logger)
	handler.SetPrincipalResolver(resolverStub{principal: identityclient.Principal{ChannelID: "customer-whatsapp", Audience: "customer", AuthMethod: "channel_proof"}})
	body := []byte(`{"entry":[{"changes":[{"value":{"messages":[{"from":"919999999999","type":"text","text":{"body":"private message"}}]}}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set("X-Hub-Signature-256", signature("app-secret", body))
	recorder := httptest.NewRecorder()

	handler.handleEvent(recorder, request)

	if recorder.Code != http.StatusOK || !logger.hasEvent("webhook_principal_resolved") || strings.Contains(fmt.Sprint(logger.entries), "919999999999") {
		t.Fatalf("principal resolution lifecycle failed: status=%d logs=%#v", recorder.Code, logger.entries)
	}
}

func TestHandleEventSendsDiscoveryLinkForResolvedCustomer(t *testing.T) {
	logger := &captureLogger{}
	sender := &senderStub{response: &meta.SendMessageResponse{}}
	handler := NewHandler("expected-token", "app-secret", sender, logger)
	handler.SetPrincipalResolver(resolverStub{principal: identityclient.Principal{ChannelID: "customer-whatsapp", Audience: "customer", AuthMethod: "channel_proof", DiscoveryURL: "https://example.test/app/discover?token=opaque"}})
	body := []byte(`{"entry":[{"changes":[{"value":{"messages":[{"from":"919999999999","type":"text","text":{"body":"hello"}}]}}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set("X-Hub-Signature-256", signature("app-secret", body))
	recorder := httptest.NewRecorder()
	handler.handleEvent(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(sender.body, "/app/discover?token=opaque") {
		t.Fatalf("discovery link was not sent: status=%d body=%q", recorder.Code, sender.body)
	}
}

func TestHandleEventLogsOtherPayload(t *testing.T) {
	logger := &captureLogger{}
	handler := NewHandler("expected-token", "app-secret", &senderStub{}, logger)
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set("X-Hub-Signature-256", signature("app-secret", body))
	recorder := httptest.NewRecorder()

	handler.handleEvent(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !logger.hasField("kind", "other") || !logger.hasField("text_messages", 0) || !logger.hasField("replies_skipped", 0) {
		t.Fatalf("missing other-event summary: %#v", logger.entries)
	}
}

func TestHandleEventLogsReplyFailure(t *testing.T) {
	logger := &captureLogger{}
	sender := &senderStub{err: &meta.DeliveryError{Kind: "provider_rejected", StatusCode: http.StatusUnauthorized}}
	handler := NewHandler("expected-token", "app-secret", sender, logger)
	body := []byte(`{"entry":[{"changes":[{"value":{"messages":[{"from":"919999999999","type":"text","text":{"body":"hello"}}]}}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	request.Header.Set("X-Hub-Signature-256", signature("app-secret", body))
	recorder := httptest.NewRecorder()

	handler.handleEvent(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !logger.hasEvent("webhook_reply_failed") || !logger.hasField("error_kind", "provider_rejected") || !logger.hasField("http_status", http.StatusUnauthorized) || !logger.hasField("replies_failed", 1) {
		t.Fatalf("missing reply failure details: %#v", logger.entries)
	}
}

func signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
