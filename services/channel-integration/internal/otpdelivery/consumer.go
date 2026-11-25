package otpdelivery

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	gen "appliance-code/sdk/golang/gen/messaging"
	"appliance-code/sdk/golang/messaging"
	"appliance-code/services/channel-integration/internal/meta"
)

type Logger interface {
	Infow(string, ...interface{})
	Warnw(string, ...interface{})
	Errorw(string, ...interface{})
}

type Sender interface {
	SendOTPTemplate(context.Context, string, string, string, string) (*meta.SendMessageResponse, error)
}

var (
	challengeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	phonePattern       = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

type deduper struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// The JetStream durable consumer provides redelivery, while this process-local
// cache suppresses a second Meta request after a successful send whose ACK was
// lost. This deployment is intentionally single-replica; durable cross-replica
// idempotency needs a provider-side idempotency key or a shared delivery store.
func newDeduper() *deduper { return &deduper{seen: make(map[string]time.Time)} }

func (d *deduper) delivered(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, expires := range d.seen {
		if !expires.After(now) {
			delete(d.seen, key)
		}
	}
	_, exists := d.seen[id]
	return exists
}

func (d *deduper) mark(id string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = now.Add(10 * time.Minute)
	if len(d.seen) <= 2048 {
		return
	}
	var oldestID string
	var oldest time.Time
	for key, expires := range d.seen {
		if oldestID == "" || expires.Before(oldest) {
			oldestID, oldest = key, expires
		}
	}
	delete(d.seen, oldestID)
}

// Run consumes the durable WhatsApp OTP delivery stream. Events for other
// channels are acknowledged without side effects, allowing independent
// adapters to share the same provider-neutral event subject.
func Run(ctx context.Context, client *messaging.Client, key []byte, templateName, templateLanguage string, sender Sender, logger Logger) error {
	if client == nil || len(key) != 32 || sender == nil || logger == nil {
		return errors.New("invalid OTP delivery consumer configuration")
	}
	delivered := newDeduper()
	return client.Subscribe(ctx, "event.identity.otp.requested", "channel-integration-whatsapp-otp", func(ctx context.Context, message messaging.Message) error {
		event := message.Wire.GetIdentityEvent()
		if event == nil || event.GetType() != gen.IdentityEvent_OTP_REQUESTED || !contains(event.GetDeliveryChannels(), "whatsapp") {
			return nil
		}
		if !challengeIDPattern.MatchString(event.GetChallengeId()) || !phonePattern.MatchString(event.GetPhone()) {
			logger.Warnw("OTP event rejected", "event", "otp_delivery_rejected", "error_kind", "invalid_identity")
			return nil
		}
		expiresAt := event.GetExpiresAt()
		if expiresAt == nil || !expiresAt.IsValid() || !expiresAt.AsTime().After(time.Now()) {
			logger.Warnw("expired OTP event skipped", "event", "otp_delivery_skipped", "error_kind", "expired")
			return nil
		}
		if delivered.delivered(event.GetChallengeId(), time.Now()) {
			logger.Infow("duplicate OTP event skipped", "event", "otp_delivery_skipped", "error_kind", "duplicate")
			return nil
		}
		codeBytes, err := messaging.Open(key, event.GetEncryptedCode())
		if err != nil {
			logger.Errorw("OTP event decryption failed", "event", "otp_delivery_failed", "error_kind", "decrypt_failed")
			return err
		}
		code := string(codeBytes)
		if len(code) != 6 {
			logger.Errorw("OTP event contained an invalid code", "event", "otp_delivery_failed", "error_kind", "invalid_code")
			return errors.New("invalid OTP code")
		}
		response, err := sender.SendOTPTemplate(ctx, event.GetPhone(), templateName, templateLanguage, code)
		if err != nil {
			kind, status := safeDeliveryError(err)
			logger.Errorw("WhatsApp OTP delivery failed", "event", "otp_delivery_failed", "error_kind", kind, "http_status", status)
			// Do not poison the durable consumer with a permanent provider
			// rejection (bad template, credentials, or recipient). Transient
			// transport/provider failures are retried by JetStream.
			if !retryableDeliveryError(err) {
				return nil
			}
			return err
		}
		providerMessages := 0
		if response != nil {
			providerMessages = len(response.Messages)
		}
		logger.Infow("WhatsApp OTP delivered", "event", "otp_delivery_sent", "provider", "meta", "provider_messages", providerMessages)
		delivered.mark(event.GetChallengeId(), time.Now())
		return nil
	})
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func safeDeliveryError(err error) (string, int) {
	var deliveryErr *meta.DeliveryError
	if errors.As(err, &deliveryErr) {
		if deliveryErr == nil {
			return "delivery_failed", 0
		}
		return deliveryErr.Kind, deliveryErr.StatusCode
	}
	return "delivery_failed", 0
}

func retryableDeliveryError(err error) bool {
	var deliveryErr *meta.DeliveryError
	if !errors.As(err, &deliveryErr) || deliveryErr == nil {
		return true
	}
	status := deliveryErr.StatusCode
	return status == 0 || status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}
