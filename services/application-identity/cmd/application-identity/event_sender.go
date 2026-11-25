package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	gen "appliance-code/sdk/golang/gen/messaging"
	"appliance-code/sdk/golang/messaging"
	"appliance-code/sdk/golang/servicehealth"
	"appliance-code/services/application-identity/internal/identity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const otpRequestedSubject = "event.identity.otp.requested"

type otpEventSender struct {
	client   *messaging.Client
	key      []byte
	channels []string
}

func (s *otpEventSender) Send(ctx context.Context, phone, code string) error {
	return s.SendOTP(ctx, identity.OTPDeliveryRequest{Phone: phone, Code: code, Factor: "otp", DeliveryChannels: s.channels, Purpose: "login", OriginChannel: "web", Audience: identity.CustomerAudience, TenantID: identity.DefaultTenantID})
}

func (s *otpEventSender) SendOTP(ctx context.Context, request identity.OTPDeliveryRequest) error {
	if s == nil || s.client == nil || len(s.key) != 32 {
		return errors.New("identity OTP event sender is unavailable")
	}
	if request.ChallengeID == "" || request.Phone == "" || len(request.Code) != 6 || request.ExpiresAt.IsZero() || !request.ExpiresAt.After(time.Now()) {
		return errors.New("invalid identity OTP delivery request")
	}
	for _, digit := range request.Code {
		if digit < '0' || digit > '9' {
			return errors.New("invalid identity OTP delivery request")
		}
	}
	channels := request.DeliveryChannels
	if len(channels) == 0 {
		channels = s.channels
	}
	channels = selectConfiguredChannels(channels, s.channels)
	if len(channels) == 0 {
		return errors.New("no configured OTP delivery channel")
	}
	encryptedCode, err := messaging.Seal(s.key, []byte(request.Code))
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, otpRequestedSubject, "application-identity", &gen.IdentityEvent{
		Type:             gen.IdentityEvent_OTP_REQUESTED,
		ChallengeId:      request.ChallengeID,
		Phone:            request.Phone,
		Factor:           request.Factor,
		DeliveryChannels: append([]string(nil), channels...),
		Purpose:          request.Purpose,
		OriginChannel:    request.OriginChannel,
		Audience:         request.Audience,
		TenantId:         request.TenantID,
		EncryptedCode:    encryptedCode,
		ExpiresAt:        timestamppb.New(request.ExpiresAt),
	})
}

func selectConfiguredChannels(requested, configured []string) []string {
	selected := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(configured))
	for _, want := range requested {
		for _, available := range configured {
			if strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(available)) {
				key := strings.ToLower(strings.TrimSpace(available))
				if _, exists := seen[key]; !exists {
					selected = append(selected, available)
					seen[key] = struct{}{}
				}
				break
			}
		}
	}
	return selected
}

func connectMessaging(ctx context.Context, url, name string) (*messaging.Client, error) {
	var client *messaging.Client
	policy := servicehealth.RetryPolicy{InitialDelay: time.Second, MaxDelay: 30 * time.Second, AttemptTimeout: 5 * time.Second}
	err := policy.Wait(ctx, func(ctx context.Context) error {
		candidate, err := messaging.Connect(messaging.Options{URL: url, Name: name, JetStream: true, Timeout: 5 * time.Second})
		if err != nil {
			return err
		}
		if err := candidate.EnsureStream(ctx, messaging.StreamSpec{Name: "identity", Subjects: []string{"event.identity.>"}, MaxAge: 24 * time.Hour}); err != nil {
			candidate.Close()
			return err
		}
		client = candidate
		return nil
	}, func(_ error, retryIn time.Duration) {
		// Do not include broker connection details in the service log; the
		// deployment already exposes the configured endpoint through the pod
		// environment and the retry event is sufficient for diagnosis.
		fmt.Fprintf(os.Stderr, "{\"level\":\"warn\",\"service\":\"identity\",\"event\":\"messaging_unavailable\",\"retry_in\":%q}\n", retryIn.String())
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}
