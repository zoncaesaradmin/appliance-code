package licensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

var (
	ErrUnresolved      = errors.New("licensing: license state is unresolved")
	ErrInvalidDocument = errors.New("licensing: invalid license document")
	ErrNotEntitled     = errors.New("licensing: capability not entitled")
	ErrAlreadyResolved = errors.New("licensing: license state is already resolved")
)

// Status is the API-facing licensing status.
type Status struct {
	State                string         `json:"state"`
	Resolved             bool           `json:"resolved"`
	ProfileActivationOK  bool           `json:"profileActivationAvailable"`
	EntitledCapabilities []string       `json:"entitledCapabilities"`
	AcceptedAt           *time.Time     `json:"acceptedAt,omitempty"`
	Summary              map[string]any `json:"summary,omitempty"`
}

// Document is the offline license document format (v1).
type Document struct {
	Version      int      `json:"version"`
	Issuer       string   `json:"issuer"`
	CustomerID   string   `json:"customerId,omitempty"`
	ApplianceID  string   `json:"applianceId,omitempty"`
	ValidFrom    string   `json:"validFrom,omitempty"`
	ValidTo      string   `json:"validTo,omitempty"`
	Capabilities []string `json:"capabilities"`
	Signature    string   `json:"signature"`
}

// Service owns licensing state transitions and entitlement checks.
type Service struct {
	store storage.LicensingStore
	db    storage.DB
	audit *audit.Recorder
	now   func() time.Time
}

func NewService(db storage.DB, store storage.LicensingStore, recorder *audit.Recorder) *Service {
	return &Service{db: db, store: store, audit: recorder, now: time.Now}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	rec, err := s.store.GetLicensing(ctx)
	if err != nil {
		return Status{}, err
	}
	caps, err := s.entitledCapabilities(rec)
	if err != nil {
		return Status{}, err
	}
	var summary map[string]any
	if rec.LicenseSummaryJSON != "" && rec.LicenseSummaryJSON != "{}" {
		_ = json.Unmarshal([]byte(rec.LicenseSummaryJSON), &summary)
	}
	return Status{
		State:                rec.State,
		Resolved:             rec.State != storage.LicensingUnresolved,
		ProfileActivationOK:  rec.State != storage.LicensingUnresolved,
		EntitledCapabilities: caps,
		AcceptedAt:           rec.AcceptedAt,
		Summary:              summary,
	}, nil
}

func (s *Service) Entitlements(ctx context.Context) ([]string, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	return st.EntitledCapabilities, nil
}

func (s *Service) IsResolved(ctx context.Context) (bool, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	return st.Resolved, nil
}

func (s *Service) IsCapabilityEntitled(ctx context.Context, capability string) (bool, error) {
	caps, err := s.Entitlements(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range caps {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) AcceptBaseEntitlement(ctx context.Context, actor audit.Actor) (Status, error) {
	var out Status
	err := s.db.WithTx(ctx, func(txCtx context.Context) error {
		rec, err := s.store.GetLicensing(txCtx)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		summary, _ := json.Marshal(map[string]any{
			"mode":         "base_free",
			"capabilities": baseFreeCapabilities(),
		})
		rec.State = storage.LicensingBaseFree
		rec.LicenseDocument = ""
		rec.LicenseSummaryJSON = string(summary)
		rec.AcceptedAt = &now
		rec.AcceptedByUserID = actor.UserID
		rec.UpdatedAt = now
		if err := s.store.PutLicensing(txCtx, rec); err != nil {
			return err
		}
		if s.audit != nil {
			if err := s.audit.Record(txCtx, actor, audit.Event{
				Action:     "licensing.base_entitlement.accept",
				TargetType: "licensing",
				TargetID:   "appliance",
				Outcome:    storage.AuditOutcomeSuccess,
			}); err != nil {
				return err
			}
		}
		out, err = s.Status(txCtx)
		return err
	})
	return out, err
}

func (s *Service) ImportLicense(ctx context.Context, actor audit.Actor, raw string) (Status, error) {
	doc, err := ParseAndValidateDocument(raw)
	if err != nil {
		return Status{}, err
	}
	var out Status
	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		rec, err := s.store.GetLicensing(txCtx)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		summary, _ := json.Marshal(map[string]any{
			"mode":         "licensed",
			"issuer":       doc.Issuer,
			"customerId":   doc.CustomerID,
			"applianceId":  doc.ApplianceID,
			"validFrom":    doc.ValidFrom,
			"validTo":      doc.ValidTo,
			"capabilities": doc.Capabilities,
		})
		rec.State = storage.LicensingLicensed
		rec.LicenseDocument = strings.TrimSpace(raw)
		rec.LicenseSummaryJSON = string(summary)
		rec.AcceptedAt = &now
		rec.AcceptedByUserID = actor.UserID
		rec.UpdatedAt = now
		if err := s.store.PutLicensing(txCtx, rec); err != nil {
			return err
		}
		if s.audit != nil {
			if err := s.audit.Record(txCtx, actor, audit.Event{
				Action:     "licensing.license.import",
				TargetType: "licensing",
				TargetID:   "appliance",
				Outcome:    storage.AuditOutcomeSuccess,
				Details: map[string]any{
					"issuer":     doc.Issuer,
					"customerId": doc.CustomerID,
				},
			}); err != nil {
				return err
			}
		}
		out, err = s.Status(txCtx)
		return err
	})
	return out, err
}

func (s *Service) entitledCapabilities(rec storage.LicensingRecord) ([]string, error) {
	switch rec.State {
	case storage.LicensingUnresolved:
		return nil, nil
	case storage.LicensingBaseFree:
		return baseFreeCapabilities(), nil
	case storage.LicensingLicensed:
		var summary struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal([]byte(rec.LicenseSummaryJSON), &summary); err != nil {
			return nil, fmt.Errorf("licensing: parse summary: %w", err)
		}
		return normalizeCapabilities(summary.Capabilities), nil
	default:
		return nil, fmt.Errorf("licensing: unknown state %q", rec.State)
	}
}

func baseFreeCapabilities() []string {
	return []string{
		string(appliance.CapabilityBase),
		string(appliance.CapabilityHost),
		string(appliance.CapabilityWorkflows),
	}
}

func ParseAndValidateDocument(raw string) (Document, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Document{}, fmt.Errorf("%w: empty document", ErrInvalidDocument)
	}
	var doc Document
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if doc.Version != 1 {
		return Document{}, fmt.Errorf("%w: unsupported version %d", ErrInvalidDocument, doc.Version)
	}
	if strings.TrimSpace(doc.Issuer) == "" {
		return Document{}, fmt.Errorf("%w: issuer is required", ErrInvalidDocument)
	}
	if strings.TrimSpace(doc.Signature) == "" {
		return Document{}, fmt.Errorf("%w: signature is required", ErrInvalidDocument)
	}
	// V1 offline trust: accept explicit offline signatures. Full PKI trust-root
	// packaging remains an open product decision; fail closed on empty/malformed.
	sig := strings.TrimSpace(doc.Signature)
	if sig != "offline-dev" && !strings.HasPrefix(sig, "offline:") {
		return Document{}, fmt.Errorf("%w: untrusted signature", ErrInvalidDocument)
	}
	if len(doc.Capabilities) == 0 {
		return Document{}, fmt.Errorf("%w: capabilities are required", ErrInvalidDocument)
	}
	caps := normalizeCapabilities(doc.Capabilities)
	hasBase := false
	for _, c := range caps {
		if !appliance.IsKnownCapability(appliance.Capability(c)) {
			return Document{}, fmt.Errorf("%w: unknown capability %q", ErrInvalidDocument, c)
		}
		if c == string(appliance.CapabilityBase) {
			hasBase = true
		}
	}
	if !hasBase {
		return Document{}, fmt.Errorf("%w: base capability is required", ErrInvalidDocument)
	}
	if err := validateValidityWindow(doc.ValidFrom, doc.ValidTo, time.Now().UTC()); err != nil {
		return Document{}, err
	}
	doc.Capabilities = caps
	return doc, nil
}

func validateValidityWindow(from, to string, now time.Time) error {
	if strings.TrimSpace(from) != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return fmt.Errorf("%w: invalid validFrom", ErrInvalidDocument)
		}
		if now.Before(t) {
			return fmt.Errorf("%w: license not yet valid", ErrInvalidDocument)
		}
	}
	if strings.TrimSpace(to) != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return fmt.Errorf("%w: invalid validTo", ErrInvalidDocument)
		}
		if now.After(t) {
			return fmt.Errorf("%w: license expired", ErrInvalidDocument)
		}
	}
	return nil
}

func normalizeCapabilities(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
