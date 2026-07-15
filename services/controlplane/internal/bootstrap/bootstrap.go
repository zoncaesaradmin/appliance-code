// Package bootstrap implements the one-time day-0 initialization the plan
// requires: create the first administrator and mark bootstrap consumed
// atomically. It succeeds only when no user exists yet, and it is not a
// normal administrative workflow once the control plane is healthy.
package bootstrap

import (
	"context"
	"errors"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/users"
)

// ErrAlreadyInitialized is returned when Init is called against a database
// that already has at least one user.
var ErrAlreadyInitialized = errors.New("bootstrap: appliance is already initialized")

// Result reports the created administrator's identity.
type Result struct {
	AdminUserID string
	Username    string
}

// Initialized reports whether at least one user already exists, meaning the
// appliance has completed first-admin setup.
func Initialized(ctx context.Context, userStore storage.UserStore) (bool, error) {
	existing, err := userStore.List(ctx)
	if err != nil {
		return false, err
	}
	return len(existing) > 0, nil
}

// Init creates the first administrator user and its administrator role
// assignment in one transaction, refusing to run if any user already
// exists. It never overwrites or reinitializes an already-initialized
// appliance.
func Init(ctx context.Context, db storage.DB, userStore storage.UserStore, roleStore storage.RoleStore, usersSvc *users.Service, adminUsername, adminPassword, displayName string) (Result, error) {
	var result Result
	err := db.WithTx(ctx, func(ctx context.Context) error {
		existing, err := userStore.List(ctx)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return ErrAlreadyInitialized
		}

		user, err := usersSvc.Create(ctx, audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "bootstrap"}, adminUsername, displayName, adminPassword)
		if err != nil {
			return err
		}

		if err := roleStore.AssignUserRole(ctx, user.ID, roles.AdministratorRoleID); err != nil {
			return err
		}

		result = Result{AdminUserID: user.ID, Username: user.Username}
		return nil
	})
	return result, err
}
