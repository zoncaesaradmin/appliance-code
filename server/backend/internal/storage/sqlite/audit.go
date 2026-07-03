package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"appliance-code/server/backend/internal/storage"
)

// AuditStore is the SQLite-backed storage.AuditStore. Events are chained by
// hashing each record together with the previous record's hash, so any
// retroactive edit or deletion breaks the chain from that point forward.
// This is tamper-evident, not tamper-proof against host root access, per the
// plan's stated audit guarantee.
type AuditStore struct {
	db *DB
}

// NewAuditStore returns an AuditStore backed by db.
func NewAuditStore(db *DB) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) Append(ctx context.Context, event storage.AuditEvent) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		var lastSeq int64
		var lastHash []byte
		row := s.db.q(ctx).QueryRowContext(ctx, `SELECT sequence, hash FROM audit_events ORDER BY sequence DESC LIMIT 1`)
		switch err := row.Scan(&lastSeq, &lastHash); {
		case errors.Is(err, sql.ErrNoRows):
			lastSeq, lastHash = 0, nil
		case err != nil:
			return fmt.Errorf("sqlite: reading last audit sequence: %w", err)
		}

		nextSeq := lastSeq + 1
		hash := chainHash(nextSeq, event, lastHash)

		_, err := s.db.q(ctx).ExecContext(ctx, `
			INSERT INTO audit_events (
				id, sequence, occurred_at, actor_user_id, actor_type, auth_method, credential_id,
				action, target_type, target_id, outcome, reason_code, request_id, source_addr,
				severity, details, prev_hash, hash
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID, nextSeq, event.OccurredAt.Format(time.RFC3339Nano),
			nullableString(event.ActorUserID), string(event.ActorType), nullableString(event.AuthMethod), nullableString(event.CredentialID),
			event.Action, nullableString(event.TargetType), nullableString(event.TargetID), string(event.Outcome), nullableString(event.ReasonCode),
			nullableString(event.RequestID), nullableString(event.SourceAddr), string(event.Severity), event.Details, lastHash, hash,
		)
		if err != nil {
			return fmt.Errorf("sqlite: appending audit event: %w", err)
		}
		return nil
	})
}

// chainHash computes the hash for one audit record given its sequence
// number, canonical field values, and the previous record's hash.
func chainHash(sequence int64, e storage.AuditEvent, prevHash []byte) []byte {
	var b strings.Builder
	b.WriteString(strconv.FormatInt(sequence, 10))
	b.WriteByte(0x1f)
	b.WriteString(e.OccurredAt.UTC().Format(time.RFC3339Nano))
	b.WriteByte(0x1f)
	b.WriteString(e.ActorUserID)
	b.WriteByte(0x1f)
	b.WriteString(string(e.ActorType))
	b.WriteByte(0x1f)
	b.WriteString(e.AuthMethod)
	b.WriteByte(0x1f)
	b.WriteString(e.CredentialID)
	b.WriteByte(0x1f)
	b.WriteString(e.Action)
	b.WriteByte(0x1f)
	b.WriteString(e.TargetType)
	b.WriteByte(0x1f)
	b.WriteString(e.TargetID)
	b.WriteByte(0x1f)
	b.WriteString(string(e.Outcome))
	b.WriteByte(0x1f)
	b.WriteString(e.ReasonCode)
	b.WriteByte(0x1f)
	b.WriteString(e.RequestID)
	b.WriteByte(0x1f)
	b.WriteString(e.SourceAddr)
	b.WriteByte(0x1f)
	b.WriteString(string(e.Severity))
	b.WriteByte(0x1f)
	b.Write(e.Details)
	b.WriteByte(0x1f)
	b.Write(prevHash)

	sum := sha256.Sum256([]byte(b.String()))
	return sum[:]
}

func (s *AuditStore) List(ctx context.Context, filter storage.AuditFilter) ([]storage.AuditEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `SELECT id, sequence, occurred_at, actor_user_id, actor_type, auth_method, credential_id,
		action, target_type, target_id, outcome, reason_code, request_id, source_addr, severity, details
		FROM audit_events WHERE 1 = 1`
	var args []any
	if filter.ActorUserID != "" {
		query += ` AND actor_user_id = ?`
		args = append(args, filter.ActorUserID)
	}
	if filter.Action != "" {
		query += ` AND action = ?`
		args = append(args, filter.Action)
	}
	if !filter.Since.IsZero() {
		query += ` AND occurred_at >= ?`
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing audit events: %w", err)
	}
	defer rows.Close()

	var events []storage.AuditEvent
	for rows.Next() {
		e, _, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func scanAuditEvent(row interface{ Scan(dest ...any) error }) (storage.AuditEvent, int64, error) {
	var (
		e                                                                                              storage.AuditEvent
		sequence                                                                                       int64
		occurredAt                                                                                     string
		actorUserID, authMethod, credentialID, targetType, targetID, reasonCode, requestID, sourceAddr sql.NullString
	)
	if err := row.Scan(
		&e.ID, &sequence, &occurredAt, &actorUserID, &e.ActorType, &authMethod, &credentialID,
		&e.Action, &targetType, &targetID, &e.Outcome, &reasonCode, &requestID, &sourceAddr,
		&e.Severity, &e.Details,
	); err != nil {
		return storage.AuditEvent{}, 0, err
	}

	var err error
	if e.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt); err != nil {
		return storage.AuditEvent{}, 0, err
	}
	e.ActorUserID = actorUserID.String
	e.AuthMethod = authMethod.String
	e.CredentialID = credentialID.String
	e.TargetType = targetType.String
	e.TargetID = targetID.String
	e.ReasonCode = reasonCode.String
	e.RequestID = requestID.String
	e.SourceAddr = sourceAddr.String
	return e, sequence, nil
}

// VerifyChain recomputes the hash chain over every stored audit event in
// sequence order and fails on the first mismatch, proving no record has
// been altered or removed since it was appended.
func (s *AuditStore) VerifyChain(ctx context.Context) error {
	rows, err := s.db.q(ctx).QueryContext(ctx, `
		SELECT id, sequence, occurred_at, actor_user_id, actor_type, auth_method, credential_id,
			action, target_type, target_id, outcome, reason_code, request_id, source_addr, severity, details,
			prev_hash, hash
		FROM audit_events ORDER BY sequence ASC`)
	if err != nil {
		return fmt.Errorf("sqlite: reading audit events for chain verification: %w", err)
	}
	defer rows.Close()

	var expectedPrevHash []byte
	for rows.Next() {
		var (
			e                                                                                              storage.AuditEvent
			sequence                                                                                       int64
			occurredAt                                                                                     string
			actorUserID, authMethod, credentialID, targetType, targetID, reasonCode, requestID, sourceAddr sql.NullString
			prevHash, hash                                                                                 []byte
		)
		if err := rows.Scan(
			&e.ID, &sequence, &occurredAt, &actorUserID, &e.ActorType, &authMethod, &credentialID,
			&e.Action, &targetType, &targetID, &e.Outcome, &reasonCode, &requestID, &sourceAddr,
			&e.Severity, &e.Details, &prevHash, &hash,
		); err != nil {
			return err
		}
		if e.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt); err != nil {
			return err
		}
		e.ActorUserID, e.AuthMethod, e.CredentialID = actorUserID.String, authMethod.String, credentialID.String
		e.TargetType, e.TargetID, e.ReasonCode = targetType.String, targetID.String, reasonCode.String
		e.RequestID, e.SourceAddr = requestID.String, sourceAddr.String

		if string(prevHash) != string(expectedPrevHash) {
			return fmt.Errorf("sqlite: audit chain broken at sequence %d: unexpected prev_hash", sequence)
		}
		want := chainHash(sequence, e, prevHash)
		if string(want) != string(hash) {
			return fmt.Errorf("sqlite: audit chain broken at sequence %d: hash mismatch", sequence)
		}
		expectedPrevHash = hash
	}
	return rows.Err()
}
