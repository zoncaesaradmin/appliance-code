package storage

import (
	"context"
	"time"
)

// MetadataBundleRecord persists active/previous metadata bundle metadata.
type MetadataBundleRecord struct {
	Slot            string // "active" or "previous"
	MetadataVersion string
	SoftwareVersion string
	Digest          string
	DirectoryName   string
	DirectoryPath   string
	Signature       string
	InstalledAt     time.Time
	InstalledBy     string
}

// MetadataBundleStore persists active and previous metadata bundles.
type MetadataBundleStore interface {
	GetMetadataBundle(ctx context.Context, slot string) (MetadataBundleRecord, error)
	PutMetadataBundle(ctx context.Context, rec MetadataBundleRecord) error
	ClearMetadataBundle(ctx context.Context, slot string) error
}
