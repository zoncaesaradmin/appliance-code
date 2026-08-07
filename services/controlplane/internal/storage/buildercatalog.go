package storage

import (
	"context"
	"time"
)

// BuilderCatalogRecord is the singleton runtime build catalog document.
type BuilderCatalogRecord struct {
	DocumentText string
	ContentType  string
	UpdatedAt    time.Time
}

// BuilderCatalogStore persists the single appliance build catalog.
type BuilderCatalogStore interface {
	GetBuilderCatalog(ctx context.Context) (BuilderCatalogRecord, error)
	PutBuilderCatalog(ctx context.Context, rec BuilderCatalogRecord) error
}
