package storage

import (
	"context"
	"time"
)

// LicensingState values for the singleton licensing_state row.
const (
	LicensingUnresolved = "unresolved"
	LicensingBaseFree   = "base_free"
	LicensingLicensed   = "licensed"
)

// LicensingRecord is the persisted appliance licensing configuration.
type LicensingRecord struct {
	State              string
	LicenseDocument    string
	LicenseSummaryJSON string
	AcceptedAt         *time.Time
	AcceptedByUserID   string
	UpdatedAt          time.Time
}

// CustomApplianceProfile is an administrator-defined appliance profile.
type CustomApplianceProfile struct {
	ID               string
	DisplayName      string
	Description      string
	CapabilitiesJSON string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CreatedByUserID  string
}

// ProfileActivationStatus values.
const (
	ProfileActivationActive         = "active"
	ProfileActivationPendingRestart = "pending_restart"
	ProfileActivationFailed         = "failed"
)

// ProfileActivationRecord tracks desired/pending profile activation.
type ProfileActivationRecord struct {
	DesiredProfileID string
	Status           string
	Message          string
	UpdatedAt        time.Time
	UpdatedByUserID  string
}

// NotificationAck records that a user acknowledged a notification.
type NotificationAck struct {
	NotificationID string
	UserID         string
	AcknowledgedAt time.Time
}

// LicensingStore persists licensing, custom profiles, and activation state.
type LicensingStore interface {
	GetLicensing(ctx context.Context) (LicensingRecord, error)
	PutLicensing(ctx context.Context, rec LicensingRecord) error

	ListCustomProfiles(ctx context.Context) ([]CustomApplianceProfile, error)
	GetCustomProfile(ctx context.Context, id string) (CustomApplianceProfile, error)
	UpsertCustomProfile(ctx context.Context, profile CustomApplianceProfile) error
	DeleteCustomProfile(ctx context.Context, id string) error

	GetActivation(ctx context.Context) (ProfileActivationRecord, error)
	PutActivation(ctx context.Context, rec ProfileActivationRecord) error

	AcknowledgeNotification(ctx context.Context, ack NotificationAck) error
	IsNotificationAcknowledged(ctx context.Context, notificationID, userID string) (bool, error)
}
