package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type DeliveryError struct {
	Kind       string
	StatusCode int
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return "graph api delivery failed"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("graph api returned status %d", e.StatusCode)
	}
	return "graph api delivery failed"
}

type Client struct {
	httpClient                                           *http.Client
	accessToken, phoneNumberID, graphAPIVersion, baseURL string
}

func NewClient(accessToken, phoneNumberID, graphAPIVersion string) *Client {
	return &Client{httpClient: &http.Client{Timeout: 15 * time.Second}, accessToken: accessToken, phoneNumberID: phoneNumberID, graphAPIVersion: graphAPIVersion, baseURL: "https://graph.facebook.com"}
}
func (c *Client) SendText(ctx context.Context, to, body string) (*SendMessageResponse, error) {
	recipient, err := normalizeRecipient(to)
	if err != nil || body == "" {
		return nil, &DeliveryError{Kind: "invalid_request"}
	}
	payload, err := json.Marshal(SendTextRequest{MessagingProduct: "whatsapp", RecipientType: "individual", To: recipient, Type: "text", Text: SendText{Body: body}})
	if err != nil {
		return nil, &DeliveryError{Kind: "request_encode_failed"}
	}
	return c.send(ctx, payload)
}

// SendOTPTemplate sends a provider-approved authentication template. The
// template name and language are operator configuration; the adapter does not
// invent provider-specific template identifiers.
func (c *Client) SendOTPTemplate(ctx context.Context, to, templateName, language, code string) (*SendMessageResponse, error) {
	recipient, err := normalizeRecipient(to)
	if err != nil || templateName == "" || language == "" || len(code) != 6 || !digitsOnly(code) {
		return nil, &DeliveryError{Kind: "invalid_request"}
	}
	payload, err := json.Marshal(SendTemplateRequest{
		MessagingProduct: "whatsapp",
		To:               recipient,
		Type:             "template",
		Template: OTPTemplate{
			Name:     templateName,
			Language: TemplateLanguage{Code: language},
			Components: []TemplateComponent{{
				Type:       "body",
				Parameters: []TemplateParameter{{Type: "text", Text: code}},
			}},
		},
	})
	if err != nil {
		return nil, &DeliveryError{Kind: "request_encode_failed"}
	}
	return c.send(ctx, payload)
}

func digitsOnly(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// normalizeRecipient converts the identity service's canonical E.164 value
// into the digits-only recipient form accepted by the WhatsApp Cloud API.
// Web requests use a leading '+' while Meta webhook sender IDs do not.
func normalizeRecipient(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	if len(value) < 8 || len(value) > 15 || value[0] == '0' {
		return "", fmt.Errorf("invalid WhatsApp recipient")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("invalid WhatsApp recipient")
		}
	}
	return value, nil
}

func (c *Client) send(ctx context.Context, payload []byte) (*SendMessageResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/%s/messages", strings.TrimRight(c.baseURL, "/"), c.graphAPIVersion, c.phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, &DeliveryError{Kind: "request_create_failed"}
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &DeliveryError{Kind: "request_failed"}
	}
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, &DeliveryError{Kind: "response_read_failed"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &DeliveryError{Kind: "provider_rejected", StatusCode: response.StatusCode}
	}
	var sent SendMessageResponse
	if err := json.Unmarshal(bodyBytes, &sent); err != nil {
		return nil, &DeliveryError{Kind: "response_decode_failed"}
	}
	if len(sent.Messages) == 0 {
		return nil, &DeliveryError{Kind: "response_missing_message", StatusCode: response.StatusCode}
	}
	return &sent, nil
}
