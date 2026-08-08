// Package auditops owns audit export jobs, checkpoints, and retention prune.
package auditops

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
)

const (
	OperationKindAuditExport storage.OperationKind = "audit.export"
	exportSchemaVersion                            = 1
)

var ErrNotReady = errors.New("auditops: export not ready")

type Service struct {
	store      storage.AuditStore
	operations storage.OperationsStore
	dataDir    string
	retention  time.Duration
	logger     logging.Logger
	now        func() time.Time
}

func NewService(store storage.AuditStore, operations storage.OperationsStore, dataDir string, retentionDays int, logger logging.Logger) (*Service, error) {
	if store == nil {
		return nil, errors.New("auditops: audit store is required")
	}
	if operations == nil {
		return nil, errors.New("auditops: operations store is required")
	}
	if logger == nil {
		return nil, errors.New("auditops: logger is required")
	}
	if retentionDays < 90 {
		retentionDays = 90
	}
	if retentionDays > 3650 {
		retentionDays = 3650
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "audit-exports"), 0o750); err != nil {
		return nil, fmt.Errorf("auditops: creating export directory: %w", err)
	}
	return &Service{
		store:      store,
		operations: operations,
		dataDir:    dataDir,
		retention:  time.Duration(retentionDays) * 24 * time.Hour,
		logger:     logger,
		now:        time.Now,
	}, nil
}

func (s *Service) StartExport(ctx context.Context, ownerID string) (storage.Operation, error) {
	op := storage.Operation{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Kind:      OperationKindAuditExport,
		OwnerID:   ownerID,
		Status:    storage.OperationStatusPending,
		CreatedAt: s.now().UTC(),
		UpdatedAt: s.now().UTC(),
	}
	if err := s.operations.Create(ctx, op); err != nil {
		return storage.Operation{}, err
	}
	go s.runExport(op.ID)
	return op, nil
}

func (s *Service) GetExport(ctx context.Context, id string) (storage.Operation, error) {
	op, err := s.operations.Get(ctx, id)
	if err != nil {
		return storage.Operation{}, err
	}
	if op.Kind != OperationKindAuditExport {
		return storage.Operation{}, storage.ErrNotFound
	}
	return op, nil
}

func (s *Service) ExportContentPath(ctx context.Context, id string) (string, error) {
	op, err := s.GetExport(ctx, id)
	if err != nil {
		return "", err
	}
	if op.Status != storage.OperationStatusSucceeded {
		return "", ErrNotReady
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(op.ResultBody, &result); err != nil || result.Path == "" {
		return "", fmt.Errorf("auditops: invalid export result")
	}
	return result.Path, nil
}

func (s *Service) runExport(id string) {
	ctx := context.Background()
	_ = s.operations.UpdateStatus(ctx, id, storage.OperationStatusRunning, nil, nil)
	path := filepath.Join(s.dataDir, "audit-exports", id+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		s.failExport(ctx, id, err)
		return
	}
	defer file.Close()

	lastSeq, lastHash, err := s.store.LatestSequence(ctx)
	if err != nil {
		s.failExport(ctx, id, err)
		return
	}
	header := map[string]any{
		"schemaVersion": exportSchemaVersion,
		"exportedAt":    s.now().UTC().Format(time.RFC3339Nano),
		"lastSequence":  lastSeq,
		"chainHash":     hex.EncodeToString(lastHash),
	}
	if err := writeJSONLine(file, header); err != nil {
		s.failExport(ctx, id, err)
		return
	}

	var since int64
	var count int
	for {
		batch, err := s.store.ExportEvents(ctx, since, 500)
		if err != nil {
			s.failExport(ctx, id, err)
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, event := range batch {
			row := map[string]any{
				"id": event.ID, "sequence": event.Sequence, "occurredAt": event.OccurredAt.UTC().Format(time.RFC3339Nano),
				"actorUserId": event.ActorUserID, "actorType": string(event.ActorType), "authMethod": event.AuthMethod,
				"credentialId": event.CredentialID, "action": event.Action, "targetType": event.TargetType,
				"targetId": event.TargetID, "outcome": string(event.Outcome), "reasonCode": event.ReasonCode,
				"requestId": event.RequestID, "sourceAddr": event.SourceAddr, "severity": string(event.Severity),
			}
			if len(event.Details) > 0 {
				var details any
				if json.Unmarshal(event.Details, &details) == nil {
					row["details"] = details
				}
			}
			if err := writeJSONLine(file, row); err != nil {
				s.failExport(ctx, id, err)
				return
			}
			since = event.Sequence
			count++
		}
	}
	if err := file.Sync(); err != nil {
		s.failExport(ctx, id, err)
		return
	}
	result, _ := json.Marshal(map[string]any{
		"path":          path,
		"eventCount":    count,
		"lastSequence":  lastSeq,
		"chainHash":     hex.EncodeToString(lastHash),
		"schemaVersion": exportSchemaVersion,
	})
	_ = s.operations.UpdateStatus(ctx, id, storage.OperationStatusSucceeded, result, nil)
}

func (s *Service) failExport(ctx context.Context, id string, err error) {
	s.logger.Errorw("audit export failed", "exportId", id, "error", err)
	problem, _ := json.Marshal(map[string]any{"title": "Audit export failed", "detail": err.Error()})
	_ = s.operations.UpdateStatus(ctx, id, storage.OperationStatusFailed, nil, problem)
}

func writeJSONLine(file *os.File, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// RunCheckpoint records the current chain tip into audit_checkpoints.
func (s *Service) RunCheckpoint(ctx context.Context) error {
	seq, hash, err := s.store.LatestSequence(ctx)
	if err != nil {
		return err
	}
	if seq == 0 {
		return nil
	}
	if latest, err := s.store.LatestCheckpoint(ctx); err == nil && latest.LastSequence == seq {
		return nil
	} else if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	return s.store.CreateCheckpoint(ctx, storage.AuditCheckpoint{
		ID:           uuid.Must(uuid.NewV7()).String(),
		CreatedAt:    s.now().UTC(),
		LastSequence: seq,
		ChainHash:    hash,
	})
}

// RunRetention prunes events older than the configured retention window only
// after a covering checkpoint exists, and never shortens retention.
func (s *Service) RunRetention(ctx context.Context) error {
	cp, err := s.store.LatestCheckpoint(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := s.now().UTC().Add(-s.retention)
	deleted, err := s.store.DeleteOlderThan(ctx, cutoff, cp.LastSequence)
	if err != nil {
		return err
	}
	if deleted > 0 {
		s.logger.Infow("pruned audit events", "deleted", deleted, "cutoff", cutoff, "maxSequence", cp.LastSequence)
	}
	return nil
}

// StartMaintenance runs checkpoint + retention on an interval until ctx ends.
func (s *Service) StartMaintenance(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.runMaintenanceOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runMaintenanceOnce(ctx)
			}
		}
	}()
}

func (s *Service) runMaintenanceOnce(ctx context.Context) {
	if err := s.RunCheckpoint(ctx); err != nil {
		s.logger.Warnw("audit checkpoint failed", "error", err)
	}
	if err := s.RunRetention(ctx); err != nil {
		s.logger.Warnw("audit retention prune failed", "error", err)
	}
}
