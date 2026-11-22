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
	if to == "" || body == "" {
		return nil, &DeliveryError{Kind: "invalid_request"}
	}
	payload, err := json.Marshal(SendTextRequest{MessagingProduct: "whatsapp", RecipientType: "individual", To: to, Type: "text", Text: SendText{Body: body}})
	if err != nil {
		return nil, &DeliveryError{Kind: "request_encode_failed"}
	}
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
	return &sent, nil
}
