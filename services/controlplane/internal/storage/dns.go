package storage

import (
	"context"
	"time"
)

// DNSRecordSource identifies who last wrote a LAN DNS A record.
type DNSRecordSource string

const (
	DNSRecordSourceBootstrap DNSRecordSource = "bootstrap"
	DNSRecordSourceAdmin     DNSRecordSource = "admin"
	DNSRecordSourcePeer      DNSRecordSource = "peer"
)

// DNSRecord is one left-hand label under the appliance LAN zone.
type DNSRecord struct {
	Name           string
	IPv4           string
	TTL            int
	Source         DNSRecordSource
	Owner          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LeaseExpiresAt *time.Time
}

// DNSRecordStore persists LAN DNS A records for the dns capability.
type DNSRecordStore interface {
	Upsert(ctx context.Context, rec DNSRecord) error
	Get(ctx context.Context, name string) (DNSRecord, error)
	List(ctx context.Context) ([]DNSRecord, error)
	Delete(ctx context.Context, name string) error
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}
