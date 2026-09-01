package storage

import (
	"context"
	"time"
)

// FocusContent is the appliance-wide resource presented first on Home.
type FocusContent struct {
	ResourceType string    `json:"resourceType"`
	ResourcePath string    `json:"resourcePath"`
	Title        string    `json:"title"`
	Message      string    `json:"message,omitempty"`
	PublishedAt  time.Time `json:"publishedAt"`
	PublishedBy  string    `json:"publishedBy"`
}

// FocusContentStore persists the single current-focus selection.
type FocusContentStore interface {
	GetFocusContent(ctx context.Context) (FocusContent, error)
	PutFocusContent(ctx context.Context, content FocusContent) error
	ClearFocusContent(ctx context.Context) error
}
