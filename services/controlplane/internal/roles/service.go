package roles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

// ErrBuiltInRole is returned for any attempt to modify a built-in role's
// identity (name, built-in flag) or delete it. Built-in role IDs and names
// are permanent; only Seed may adjust their effective permission set across
// versions.
var ErrBuiltInRole = errors.New("roles: built-in roles cannot be modified or deleted this way")

// ErrUnknownPermission is returned when a role references a permission name
// outside the published catalog.
var ErrUnknownPermission = errors.New("roles: unknown permission")

// ErrLastAdministrator is returned when a role assignment change would
// leave the appliance without an enabled effective administrator. This
// invariant is enforced here via the fixed built-in administrator role:
// v1's custom roles cannot themselves confer the administrator identity, so
// only administrator-role (un)assignment can trigger it.
var ErrLastAdministrator = errors.New("roles: cannot remove the last enabled administrator")

// Service implements role and role-assignment business logic above
// storage.RoleStore.
type Service struct {
	db    storage.DB
	roles storage.RoleStore
	users storage.UserStore
	audit *audit.Recorder
}

// NewService wires a Service from its storage dependencies.
func NewService(db storage.DB, roles storage.RoleStore, users storage.UserStore, recorder *audit.Recorder) *Service {
	return &Service{db: db, roles: roles, users: users, audit: recorder}
}

func (s *Service) ListPermissions(ctx context.Context) ([]storage.Permission, error) {
	return s.roles.ListPermissions(ctx)
}

func (s *Service) List(ctx context.Context) ([]storage.Role, error) {
	return s.roles.List(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (storage.Role, error) {
	return s.roles.Get(ctx, id)
}

func (s *Service) validatePermissions(ctx context.Context, permissionNames []string) error {
	catalog, err := s.roles.ListPermissions(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(catalog))
	for _, p := range catalog {
		known[p.Name] = true
	}
	for _, name := range permissionNames {
		if !known[name] {
			return fmt.Errorf("%w: %s", ErrUnknownPermission, name)
		}
	}
	return nil
}

// Create defines a new custom role from a subset of the published
// permission catalog.
func (s *Service) Create(ctx context.Context, actor audit.Actor, name string, permissionNames []string) (storage.Role, error) {
	if err := s.validatePermissions(ctx, permissionNames); err != nil {
		return storage.Role{}, err
	}

	role := storage.Role{ID: uuid.Must(uuid.NewV7()).String(), Name: name, BuiltIn: false}
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roles.UpsertRole(ctx, role); err != nil {
			return err
		}
		if err := s.roles.SetRolePermissions(ctx, role.ID, permissionNames); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "roles.create", TargetType: "role", TargetID: role.ID, Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"name": name},
		})
	})
	if err != nil {
		return storage.Role{}, err
	}
	return role, nil
}

// Update replaces a custom role's permission set.
func (s *Service) Update(ctx context.Context, actor audit.Actor, id string, permissionNames []string) error {
	role, err := s.roles.Get(ctx, id)
	if err != nil {
		return err
	}
	if role.BuiltIn {
		return ErrBuiltInRole
	}
	if err := s.validatePermissions(ctx, permissionNames); err != nil {
		return err
	}

	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roles.SetRolePermissions(ctx, id, permissionNames); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "roles.update", TargetType: "role", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// Delete removes a custom role and its assignments.
func (s *Service) Delete(ctx context.Context, actor audit.Actor, id string) error {
	role, err := s.roles.Get(ctx, id)
	if err != nil {
		return err
	}
	if role.BuiltIn {
		return ErrBuiltInRole
	}

	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roles.Delete(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "roles.delete", TargetType: "role", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// SetUserRoles replaces userID's complete role assignment set, refusing any
// change that would leave the appliance without an enabled effective
// administrator.
func (s *Service) SetUserRoles(ctx context.Context, actor audit.Actor, userID string, roleIDs []string) error {
	hadAdmin := false
	willHaveAdmin := false
	for _, id := range roleIDs {
		if id == AdministratorRoleID {
			willHaveAdmin = true
		}
	}
	current, err := s.roles.ListUserRoles(ctx, userID)
	if err != nil {
		return err
	}
	for _, r := range current {
		if r.ID == AdministratorRoleID {
			hadAdmin = true
		}
	}

	if hadAdmin && !willHaveAdmin {
		count, err := s.users.CountEnabledAdministrators(ctx, AdministratorRoleID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdministrator
		}
	}

	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.roles.SetUserRoles(ctx, userID, roleIDs); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.roles.update", TargetType: "user", TargetID: userID, Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"roleIds": roleIDs},
		})
	})
}

func (s *Service) ListUserRoles(ctx context.Context, userID string) ([]storage.Role, error) {
	return s.roles.ListUserRoles(ctx, userID)
}
