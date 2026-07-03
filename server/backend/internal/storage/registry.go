package storage

import (
	"context"
	"time"
)

// RegistryGrantSubjectType distinguishes a user-scoped from a
// role-scoped registry grant.
type RegistryGrantSubjectType string

const (
	RegistryGrantSubjectUser RegistryGrantSubjectType = "user"
	RegistryGrantSubjectRole RegistryGrantSubjectType = "role"
)

// RegistryGrant is an explicit, administrator-assigned repository-prefix
// grant, on top of the built-in roles' implicit prefixes computed at
// authorization time.
type RegistryGrant struct {
	ID          string
	SubjectType RegistryGrantSubjectType
	SubjectID   string
	PathPrefix  string
	Actions     []string // subset of "pull", "push"
	CreatedAt   time.Time
}

// RegistryGrantStore persists RegistryGrant records.
type RegistryGrantStore interface {
	Create(ctx context.Context, g RegistryGrant) error
	Get(ctx context.Context, id string) (RegistryGrant, error)
	List(ctx context.Context) ([]RegistryGrant, error)
	ListForSubjects(ctx context.Context, userID string, roleIDs []string) ([]RegistryGrant, error)
	Delete(ctx context.Context, id string) error
}
