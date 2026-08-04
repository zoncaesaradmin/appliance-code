package notifications

import (
	"context"
	"time"

	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/storage"
)

const UnresolvedLicenseNotificationID = "licensing-unresolved"

// Notification is a system or user-visible alert.
type Notification struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Severity  string    `json:"severity"`
	ActionURL string    `json:"actionUrl,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Service synthesizes notification alerts from appliance setup state.
type Service struct {
	licensing *licensing.Service
	store     storage.LicensingStore
	now       func() time.Time
}

func NewService(licensingSvc *licensing.Service, store storage.LicensingStore) *Service {
	return &Service{licensing: licensingSvc, store: store, now: time.Now}
}

func (s *Service) List(ctx context.Context, userID string) ([]Notification, error) {
	st, err := s.licensing.Status(ctx)
	if err != nil {
		return nil, err
	}
	var out []Notification
	if !st.Resolved {
		out = append(out, Notification{
			ID:        UnresolvedLicenseNotificationID,
			Kind:      "licensing",
			Title:     "Licensing is not configured",
			Body:      "Configure licensing to unlock entitled capabilities, or continue with the base entitlement.",
			Severity:  "warning",
			ActionURL: "/admin/licensing",
			CreatedAt: s.now().UTC(),
		})
	}
	return out, nil
}

func (s *Service) Acknowledge(ctx context.Context, userID, notificationID string) error {
	// Unresolved licensing stays visible until licensing is resolved.
	if notificationID == UnresolvedLicenseNotificationID {
		return nil
	}
	return s.store.AcknowledgeNotification(ctx, storage.NotificationAck{
		NotificationID: notificationID,
		UserID:         userID,
		AcknowledgedAt: s.now().UTC(),
	})
}
