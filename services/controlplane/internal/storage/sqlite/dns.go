package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// DNSRecordStore is the SQLite-backed storage.DNSRecordStore.
type DNSRecordStore struct {
	db *DB
}

// NewDNSRecordStore returns a DNSRecordStore backed by db.
func NewDNSRecordStore(db *DB) *DNSRecordStore {
	return &DNSRecordStore{db: db}
}

func (s *DNSRecordStore) Upsert(ctx context.Context, rec storage.DNSRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	var lease any
	if rec.LeaseExpiresAt != nil {
		lease = rec.LeaseExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO dns_records (name, ipv4, ttl, source, owner, created_at, updated_at, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			ipv4 = excluded.ipv4,
			ttl = excluded.ttl,
			source = excluded.source,
			owner = excluded.owner,
			updated_at = excluded.updated_at,
			lease_expires_at = excluded.lease_expires_at`,
		rec.Name, rec.IPv4, rec.TTL, string(rec.Source), rec.Owner,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
		rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
		lease,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upserting dns record %s: %w", rec.Name, err)
	}
	return nil
}

const selectDNSRecordColumns = `name, ipv4, ttl, source, owner, created_at, updated_at, lease_expires_at`

func scanDNSRecord(row interface{ Scan(dest ...any) error }) (storage.DNSRecord, error) {
	var (
		rec            storage.DNSRecord
		source         string
		createdAt      string
		updatedAt      string
		leaseExpiresAt sql.NullString
	)
	if err := row.Scan(&rec.Name, &rec.IPv4, &rec.TTL, &source, &rec.Owner, &createdAt, &updatedAt, &leaseExpiresAt); err != nil {
		return storage.DNSRecord{}, err
	}
	rec.Source = storage.DNSRecordSource(source)
	var err error
	if rec.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.DNSRecord{}, err
	}
	if rec.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.DNSRecord{}, err
	}
	if leaseExpiresAt.Valid && leaseExpiresAt.String != "" {
		t, err := time.Parse(time.RFC3339Nano, leaseExpiresAt.String)
		if err != nil {
			return storage.DNSRecord{}, err
		}
		rec.LeaseExpiresAt = &t
	}
	return rec, nil
}

func (s *DNSRecordStore) Get(ctx context.Context, name string) (storage.DNSRecord, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectDNSRecordColumns+` FROM dns_records WHERE name = ?`, name)
	rec, err := scanDNSRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.DNSRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.DNSRecord{}, fmt.Errorf("sqlite: getting dns record %s: %w", name, err)
	}
	return rec, nil
}

func (s *DNSRecordStore) List(ctx context.Context) ([]storage.DNSRecord, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectDNSRecordColumns+` FROM dns_records ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing dns records: %w", err)
	}
	defer rows.Close()
	var out []storage.DNSRecord
	for rows.Next() {
		rec, err := scanDNSRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *DNSRecordStore) Delete(ctx context.Context, name string) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM dns_records WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("sqlite: deleting dns record %s: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: deleting dns record %s: %w", name, err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *DNSRecordStore) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.q(ctx).ExecContext(ctx, `
		DELETE FROM dns_records
		WHERE lease_expires_at IS NOT NULL AND lease_expires_at <= ?`,
		now.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("sqlite: deleting expired dns records: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlite: deleting expired dns records: %w", err)
	}
	return int(n), nil
}
