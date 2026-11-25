package otpdelivery

import (
	"errors"
	"net/http"
	"testing"

	"appliance-code/services/channel-integration/internal/meta"
)

func TestRetryableDeliveryErrorClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "transport", want: true},
		{name: "rate limited", status: http.StatusTooManyRequests, want: true},
		{name: "server", status: http.StatusBadGateway, want: true},
		{name: "bad template", status: http.StatusBadRequest, want: false},
		{name: "unauthorized", status: http.StatusUnauthorized, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.name != "transport" {
				err = &meta.DeliveryError{Kind: "provider_rejected", StatusCode: tc.status}
			} else {
				err = errors.New("network unavailable")
			}
			if got := retryableDeliveryError(err); got != tc.want {
				t.Fatalf("retryableDeliveryError() = %v, want %v", got, tc.want)
			}
		})
	}
}
