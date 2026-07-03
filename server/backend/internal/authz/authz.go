// Package authz resolves a principal's effective permissions and evaluates
// RBAC decisions. It is transport- and product-neutral: REST, MCP, and
// registry-token issuance all call the same Check to guarantee identical
// allow/deny behavior for the same principal and permission, per the plan's
// "consistent RBAC everywhere" requirement.
package authz

import (
	"context"
	"errors"
	"fmt"

	"appliance-code/server/backend/internal/storage"
)

// ErrForbidden is returned when a principal lacks a required permission.
var ErrForbidden = errors.New("authz: forbidden")

// Service evaluates RBAC decisions against role/permission assignments.
type Service struct {
	roles storage.RoleStore
}

// NewService returns a Service backed by roles.
func NewService(roles storage.RoleStore) *Service {
	return &Service{roles: roles}
}

// EffectivePermissions returns the union of permissions granted by every
// role assigned to userID.
func (s *Service) EffectivePermissions(ctx context.Context, userID string) (map[string]bool, error) {
	userRoles, err := s.roles.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("authz: listing roles for user %s: %w", userID, err)
	}

	perms := make(map[string]bool)
	for _, role := range userRoles {
		names, err := s.roles.ListRolePermissions(ctx, role.ID)
		if err != nil {
			return nil, fmt.Errorf("authz: listing permissions for role %s: %w", role.ID, err)
		}
		for _, name := range names {
			perms[name] = true
		}
	}
	return perms, nil
}

// Check resolves userID's effective permissions and returns ErrForbidden if
// permission is not among them. Callers holding an API token must first
// intersect the result of EffectivePermissions with IntersectScopes before
// calling HasPermission directly; Check always evaluates the user's full
// role-derived permissions.
func (s *Service) Check(ctx context.Context, userID, permission string) error {
	perms, err := s.EffectivePermissions(ctx, userID)
	if err != nil {
		return err
	}
	if !perms[permission] {
		return ErrForbidden
	}
	return nil
}

// IntersectScopes narrows effective to the subset also present in scopes.
// A nil scopes slice means "no reduction" (the caller inherits every
// effective permission), matching the plan's API-token scope semantics:
// tokens may only ever reduce, never expand, their owner's permissions.
func IntersectScopes(effective map[string]bool, scopes []string) map[string]bool {
	if scopes == nil {
		return effective
	}
	narrowed := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		if effective[s] {
			narrowed[s] = true
		}
	}
	return narrowed
}

// HasPermission reports whether permission is present in perms, without
// returning ErrForbidden, for callers that need to branch on multiple
// candidate permissions (e.g. the ".self" vs ".any" ownership pattern).
func HasPermission(perms map[string]bool, permission string) bool {
	return perms[permission]
}
