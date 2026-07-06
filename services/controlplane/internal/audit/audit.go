// Package audit builds and appends the versioned audit event schema the
// plan requires: event ID, time, actor, authentication method and
// credential ID, action, target, outcome, reason code, and request/source
// correlation, with automatic redaction of any sensitive detail field.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
)

// Actor identifies who performed an audited action.
type Actor struct {
	UserID       string
	Type         storage.AuditActorType
	AuthMethod   string
	CredentialID string
	RequestID    string
	SourceAddr   string
}

// SystemActor is used for actions the control plane itself performs
// (maintenance jobs, startup seeding) rather than a request-driven actor.
var SystemActor = Actor{Type: storage.AuditActorSystem}

// Event describes one action to record. Details values are shallow-redacted
// for known-sensitive keys before being persisted, but callers must still
// avoid passing raw secrets since redaction only matches on key name.
type Event struct {
	Action     string
	TargetType string
	TargetID   string
	Outcome    storage.AuditOutcome
	ReasonCode string
	Severity   storage.AuditSeverity
	Details    map[string]any
}

// Recorder appends Event records as storage.AuditEvent rows. Call Record
// inside the same storage.DB.WithTx block as the state mutation it
// documents so the plan's "audit write fails the mutation closed"
// requirement holds: internal/storage/sqlite's WithTx detects an
// already-active transaction on ctx and joins it rather than nesting.
type Recorder struct {
	store storage.AuditStore
}

// NewRecorder returns a Recorder appending to store.
func NewRecorder(store storage.AuditStore) *Recorder {
	return &Recorder{store: store}
}

// Record appends one audit event attributed to actor.
func (r *Recorder) Record(ctx context.Context, actor Actor, e Event) error {
	if e.Severity == "" {
		e.Severity = storage.AuditSeverityInfo
	}

	var detailsJSON []byte
	if e.Details != nil {
		redacted := logging.RedactMap(e.Details)
		b, err := json.Marshal(redacted)
		if err != nil {
			return fmt.Errorf("audit: encoding event details: %w", err)
		}
		detailsJSON = b
	}

	event := storage.AuditEvent{
		ID:           uuid.Must(uuid.NewV7()).String(),
		OccurredAt:   time.Now().UTC(),
		ActorUserID:  actor.UserID,
		ActorType:    actor.Type,
		AuthMethod:   actor.AuthMethod,
		CredentialID: actor.CredentialID,
		Action:       e.Action,
		TargetType:   e.TargetType,
		TargetID:     e.TargetID,
		Outcome:      e.Outcome,
		ReasonCode:   e.ReasonCode,
		RequestID:    actor.RequestID,
		SourceAddr:   actor.SourceAddr,
		Severity:     e.Severity,
		Details:      detailsJSON,
	}
	if err := r.store.Append(ctx, event); err != nil {
		return fmt.Errorf("audit: appending event %s: %w", e.Action, err)
	}
	return nil
}
