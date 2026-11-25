package meta

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendOTPTemplateUsesAuthenticationTemplatePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v21.0/123/messages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		var payload SendTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Type != "template" || payload.Template.Name != "login_code" || payload.Template.Language.Code != "en_US" {
			t.Fatalf("unexpected template: %#v", payload)
		}
		if len(payload.Template.Components) != 1 || len(payload.Template.Components[0].Parameters) != 1 || payload.Template.Components[0].Parameters[0].Text != "123456" {
			t.Fatalf("unexpected code parameter: %#v", payload.Template.Components)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.test"}]}`))
	}))
	defer server.Close()

	client := NewClient("secret", "123", "v21.0")
	client.baseURL = server.URL
	response, err := client.SendOTPTemplate(context.Background(), "+919876543210", "login_code", "en_US", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || len(response.Messages) != 1 || response.Messages[0].ID != "wamid.test" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestSendOTPTemplateRejectsEmptyProviderSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer server.Close()

	client := NewClient("secret", "123", "v21.0")
	client.baseURL = server.URL
	_, err := client.SendOTPTemplate(context.Background(), "+919876543210", "login_code", "en_US", "123456")
	var deliveryErr *DeliveryError
	if !errors.As(err, &deliveryErr) || deliveryErr.Kind != "response_missing_message" {
		t.Fatalf("error = %v, want response_missing_message", err)
	}
}

func TestSendOTPTemplateRejectsMalformedCode(t *testing.T) {
	client := NewClient("secret", "123", "v21.0")
	if _, err := client.SendOTPTemplate(context.Background(), "919876543210", "login_code", "en_US", "12345"); err == nil {
		t.Fatal("expected malformed code error")
	}
	if _, err := client.SendOTPTemplate(context.Background(), "919876543210", "login_code", "en_US", "12a456"); err == nil {
		t.Fatal("expected non-numeric code error")
	}
}

func TestSendTextNormalizesE164Recipient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload SendTextRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.To != "919876543210" {
			t.Fatalf("recipient = %q, want digits-only E.164", payload.To)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.text"}]}`))
	}))
	defer server.Close()

	client := NewClient("secret", "123", "v21.0")
	client.baseURL = server.URL
	if _, err := client.SendText(context.Background(), "+919876543210", "hello"); err != nil {
		t.Fatal(err)
	}
}
