package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// LicensingStore is the SQLite-backed storage.LicensingStore.
type LicensingStore struct {
	db *DB
}

// NewLicensingStore returns a LicensingStore backed by db.
func NewLicensingStore(db *DB) *LicensingStore {
	return &LicensingStore{db: db}
}

func (s *LicensingStore) GetLicensing(ctx context.Context) (storage.LicensingRecord, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT state, COALESCE(license_document, ''), license_summary_json,
		       accepted_at, COALESCE(accepted_by_user_id, ''), updated_at
		FROM licensing_state WHERE id = 1`)
	var (
		rec        storage.LicensingRecord
		acceptedAt sql.NullString
		updatedAt  string
	)
	if err := row.Scan(&rec.State, &rec.LicenseDocument, &rec.LicenseSummaryJSON, &acceptedAt, &rec.AcceptedByUserID, &updatedAt); err != nil {
		return storage.LicensingRecord{}, fmt.Errorf("sqlite: getting licensing state: %w", err)
	}
	var err error
	if rec.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		if rec.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt); err != nil {
			return storage.LicensingRecord{}, fmt.Errorf("sqlite: parsing licensing updated_at: %w", err)
		}
	}
	if acceptedAt.Valid && acceptedAt.String != "" {
		t, err := time.Parse(time.RFC3339Nano, acceptedAt.String)
		if err != nil {
			t, err = time.Parse(time.RFC3339, acceptedAt.String)
			if err != nil {
				return storage.LicensingRecord{}, fmt.Errorf("sqlite: parsing licensing accepted_at: %w", err)
			}
		}
		rec.AcceptedAt = &t
	}
	return rec, nil
}

func (s *LicensingStore) PutLicensing(ctx context.Context, rec storage.LicensingRecord) error {
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	if rec.LicenseSummaryJSON == "" {
		rec.LicenseSummaryJSON = "{}"
	}
	var accepted any
	if rec.AcceptedAt != nil {
		accepted = rec.AcceptedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE licensing_state
		SET state = ?, license_document = ?, license_summary_json = ?,
		    accepted_at = ?, accepted_by_user_id = ?, updated_at = ?
		WHERE id = 1`,
		rec.State, nullIfEmpty(rec.LicenseDocument), rec.LicenseSummaryJSON,
		accepted, nullIfEmpty(rec.AcceptedByUserID),
		rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: putting licensing state: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *LicensingStore) ListCustomProfiles(ctx context.Context) ([]storage.CustomApplianceProfile, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `
		SELECT id, display_name, description, capabilities_json, created_at, updated_at, COALESCE(created_by_user_id, '')
		FROM appliance_custom_profiles ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing custom profiles: %w", err)
	}
	defer rows.Close()
	var out []storage.CustomApplianceProfile
	for rows.Next() {
		p, err := scanCustomProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *LicensingStore) GetCustomProfile(ctx context.Context, id string) (storage.CustomApplianceProfile, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT id, display_name, description, capabilities_json, created_at, updated_at, COALESCE(created_by_user_id, '')
		FROM appliance_custom_profiles WHERE id = ?`, id)
	p, err := scanCustomProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.CustomApplianceProfile{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.CustomApplianceProfile{}, fmt.Errorf("sqlite: getting custom profile %s: %w", id, err)
	}
	return p, nil
}

func scanCustomProfile(row interface{ Scan(dest ...any) error }) (storage.CustomApplianceProfile, error) {
	var (
		p         storage.CustomApplianceProfile
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&p.ID, &p.DisplayName, &p.Description, &p.CapabilitiesJSON, &createdAt, &updatedAt, &p.CreatedByUserID); err != nil {
		return storage.CustomApplianceProfile{}, err
	}
	var err error
	if p.CreatedAt, err = parseTimeFlexible(createdAt); err != nil {
		return storage.CustomApplianceProfile{}, err
	}
	if p.UpdatedAt, err = parseTimeFlexible(updatedAt); err != nil {
		return storage.CustomApplianceProfile{}, err
	}
	return p, nil
}

func parseTimeFlexible(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, value)
}

func (s *LicensingStore) UpsertCustomProfile(ctx context.Context, profile storage.CustomApplianceProfile) error {
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now().UTC()
	}
	if profile.UpdatedAt.IsZero() {
		profile.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO appliance_custom_profiles
			(id, display_name, description, capabilities_json, created_at, updated_at, created_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name = excluded.display_name,
			description = excluded.description,
			capabilities_json = excluded.capabilities_json,
			updated_at = excluded.updated_at`,
		profile.ID, profile.DisplayName, profile.Description, profile.CapabilitiesJSON,
		profile.CreatedAt.UTC().Format(time.RFC3339Nano),
		profile.UpdatedAt.UTC().Format(time.RFC3339Nano),
		nullIfEmpty(profile.CreatedByUserID),
	)
	if err != nil {
		return fmt.Errorf("sqlite: upserting custom profile %s: %w", profile.ID, err)
	}
	return nil
}

func (s *LicensingStore) DeleteCustomProfile(ctx context.Context, id string) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM appliance_custom_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting custom profile %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *LicensingStore) GetActivation(ctx context.Context) (storage.ProfileActivationRecord, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT COALESCE(desired_profile_id, ''), status, message, updated_at, COALESCE(updated_by_user_id, '')
		FROM appliance_profile_activation WHERE id = 1`)
	var (
		rec       storage.ProfileActivationRecord
		updatedAt string
	)
	if err := row.Scan(&rec.DesiredProfileID, &rec.Status, &rec.Message, &updatedAt, &rec.UpdatedByUserID); err != nil {
		return storage.ProfileActivationRecord{}, fmt.Errorf("sqlite: getting profile activation: %w", err)
	}
	var err error
	if rec.UpdatedAt, err = parseTimeFlexible(updatedAt); err != nil {
		return storage.ProfileActivationRecord{}, err
	}
	return rec, nil
}

func (s *LicensingStore) PutActivation(ctx context.Context, rec storage.ProfileActivationRecord) error {
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE appliance_profile_activation
		SET desired_profile_id = ?, status = ?, message = ?, updated_at = ?, updated_by_user_id = ?
		WHERE id = 1`,
		nullIfEmpty(rec.DesiredProfileID), rec.Status, rec.Message,
		rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
		nullIfEmpty(rec.UpdatedByUserID),
	)
	if err != nil {
		return fmt.Errorf("sqlite: putting profile activation: %w", err)
	}
	return nil
}

func (s *LicensingStore) AcknowledgeNotification(ctx context.Context, ack storage.NotificationAck) error {
	if ack.AcknowledgedAt.IsZero() {
		ack.AcknowledgedAt = time.Now().UTC()
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO notification_acknowledgements (notification_id, user_id, acknowledged_at)
		VALUES (?, ?, ?)
		ON CONFLICT(notification_id) DO UPDATE SET
			user_id = excluded.user_id,
			acknowledged_at = excluded.acknowledged_at`,
		ack.NotificationID, ack.UserID, ack.AcknowledgedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: acknowledging notification %s: %w", ack.NotificationID, err)
	}
	return nil
}

func (s *LicensingStore) IsNotificationAcknowledged(ctx context.Context, notificationID, userID string) (bool, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT 1 FROM notification_acknowledgements WHERE notification_id = ? AND user_id = ?`,
		notificationID, userID)
	var one int
	err := row.Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sqlite: checking notification ack: %w", err)
	}
	return true, nil
}
