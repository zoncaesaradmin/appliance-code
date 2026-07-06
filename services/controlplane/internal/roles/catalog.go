// Package roles owns the published permission catalog, built-in roles, and
// the authorization service that resolves a user's effective permissions.
// RBAC policy evaluation itself lives in internal/authz; this package owns
// the catalog and role/user assignment business logic above
// storage.RoleStore.
package roles

import (
	"context"
	"fmt"

	"appliance-code/services/controlplane/internal/storage"
)

// Built-in role IDs are fixed and stable, per the plan's requirement that
// built-in role names and IDs never change even as their effective
// permissions are versioned.
const (
	AdministratorRoleID = "role-administrator"
	DeveloperRoleID     = "role-developer"
	ViewerRoleID        = "role-viewer"
	AutomationRoleID    = "role-automation"
)

const (
	Administrator = "administrator"
	Developer     = "developer"
	Viewer        = "viewer"
	Automation    = "automation"
)

// Permission name constants matching the published catalog in ADR 0010.
const (
	PermUsersRead    = "users.read"
	PermUsersCreate  = "users.create"
	PermUsersUpdate  = "users.update"
	PermUsersDisable = "users.disable"

	PermRolesRead   = "roles.read"
	PermRolesCreate = "roles.create"
	PermRolesUpdate = "roles.update"
	PermRolesDelete = "roles.delete"

	PermTokensReadSelf   = "tokens.read.self"
	PermTokensCreateSelf = "tokens.create.self"
	PermTokensCreateAny  = "tokens.create.any"
	PermTokensRevokeSelf = "tokens.revoke.self"
	PermTokensRevokeAny  = "tokens.revoke.any"

	PermBuildsCreate     = "builds.create"
	PermBuildsReadSelf   = "builds.read.self"
	PermBuildsReadAny    = "builds.read.any"
	PermBuildsCancelSelf = "builds.cancel.self"
	PermBuildsCancelAny  = "builds.cancel.any"

	PermArtifactsRead       = "artifacts.read"
	PermArtifactsDeleteSelf = "artifacts.delete.self"
	PermArtifactsDeleteAny  = "artifacts.delete.any"

	PermOperationsReadSelf = "operations.read.self"
	PermOperationsReadAny  = "operations.read.any"

	PermRegistryPull        = "registry.pull"
	PermRegistryPush        = "registry.push"
	PermRegistryDelete      = "registry.delete"
	PermRegistryGrantsRead  = "registry.grants.read"
	PermRegistryGrantsWrite = "registry.grants.write"

	PermMCPInvoke = "mcp.invoke"

	PermSystemRead    = "system.read"
	PermSystemOperate = "system.operate"
	PermAuditRead     = "audit.read"
	PermAuditExport   = "audit.export"
)

// AllPermissions is the complete published v1 permission catalog.
var AllPermissions = []storage.Permission{
	{Name: PermUsersRead, Description: "Read user accounts"},
	{Name: PermUsersCreate, Description: "Create user accounts"},
	{Name: PermUsersUpdate, Description: "Update user profile attributes"},
	{Name: PermUsersDisable, Description: "Disable or enable user accounts"},

	{Name: PermRolesRead, Description: "Read roles and the permission catalog"},
	{Name: PermRolesCreate, Description: "Create custom roles"},
	{Name: PermRolesUpdate, Description: "Update custom roles"},
	{Name: PermRolesDelete, Description: "Delete custom roles"},

	{Name: PermTokensReadSelf, Description: "Read own API tokens"},
	{Name: PermTokensCreateSelf, Description: "Create own API tokens"},
	{Name: PermTokensCreateAny, Description: "Create API tokens for any user"},
	{Name: PermTokensRevokeSelf, Description: "Revoke own API tokens"},
	{Name: PermTokensRevokeAny, Description: "Revoke any user's API tokens"},

	{Name: PermBuildsCreate, Description: "Submit builds"},
	{Name: PermBuildsReadSelf, Description: "Read own builds"},
	{Name: PermBuildsReadAny, Description: "Read any build"},
	{Name: PermBuildsCancelSelf, Description: "Cancel own builds"},
	{Name: PermBuildsCancelAny, Description: "Cancel any build"},

	{Name: PermArtifactsRead, Description: "Read artifact metadata"},
	{Name: PermArtifactsDeleteSelf, Description: "Delete artifacts produced by own builds"},
	{Name: PermArtifactsDeleteAny, Description: "Delete any artifact"},

	{Name: PermOperationsReadSelf, Description: "Read own durable operations"},
	{Name: PermOperationsReadAny, Description: "Read any durable operation"},

	{Name: PermRegistryPull, Description: "Pull OCI images and artifacts"},
	{Name: PermRegistryPush, Description: "Push OCI images and artifacts"},
	{Name: PermRegistryDelete, Description: "Delete OCI repository content"},
	{Name: PermRegistryGrantsRead, Description: "Read registry repository-prefix grants"},
	{Name: PermRegistryGrantsWrite, Description: "Manage registry repository-prefix grants"},

	{Name: PermMCPInvoke, Description: "Invoke MCP tools"},

	{Name: PermSystemRead, Description: "Read system status and version"},
	{Name: PermSystemOperate, Description: "Perform system operations"},
	{Name: PermAuditRead, Description: "Read audit events"},
	{Name: PermAuditExport, Description: "Export audit events"},
}

func allPermissionNames() []string {
	names := make([]string, len(AllPermissions))
	for i, p := range AllPermissions {
		names[i] = p.Name
	}
	return names
}

// BuiltInRole is a fixed role definition seeded on every startup.
type BuiltInRole struct {
	ID          string
	Name        string
	Permissions []string
}

// BuiltInRoles is the accepted v1 built-in role set from ADR 0010. Only the
// listed Permissions may change across versions; ID and Name are permanent.
var BuiltInRoles = []BuiltInRole{
	{
		ID:          AdministratorRoleID,
		Name:        Administrator,
		Permissions: allPermissionNames(),
	},
	{
		ID:   DeveloperRoleID,
		Name: Developer,
		Permissions: []string{
			PermTokensReadSelf, PermTokensCreateSelf, PermTokensRevokeSelf,
			PermBuildsCreate, PermBuildsReadSelf, PermBuildsCancelSelf,
			PermArtifactsRead, PermArtifactsDeleteSelf,
			PermOperationsReadSelf,
			PermRegistryPull, PermRegistryPush,
			PermMCPInvoke,
		},
	},
	{
		ID:   ViewerRoleID,
		Name: Viewer,
		Permissions: []string{
			PermTokensReadSelf, PermTokensCreateSelf, PermTokensRevokeSelf,
			PermBuildsReadAny, PermArtifactsRead,
			PermOperationsReadSelf,
			PermRegistryPull,
		},
	},
	{
		ID:   AutomationRoleID,
		Name: Automation,
		Permissions: []string{
			PermBuildsCreate, PermBuildsReadSelf, PermBuildsCancelSelf,
			PermArtifactsRead,
			PermOperationsReadSelf,
		},
	},
}

// Seed idempotently upserts the permission catalog and built-in roles. It is
// safe to call on every startup; only role ID/name/built-in flag and each
// built-in role's permission set are enforced, so an administrator's custom
// roles and user-role assignments are never touched.
func Seed(ctx context.Context, store storage.RoleStore) error {
	for _, p := range AllPermissions {
		if err := store.UpsertPermission(ctx, p); err != nil {
			return fmt.Errorf("roles: seeding permission %s: %w", p.Name, err)
		}
	}

	for _, br := range BuiltInRoles {
		if err := store.UpsertRole(ctx, storage.Role{ID: br.ID, Name: br.Name, BuiltIn: true}); err != nil {
			return fmt.Errorf("roles: seeding role %s: %w", br.Name, err)
		}
		if err := store.SetRolePermissions(ctx, br.ID, br.Permissions); err != nil {
			return fmt.Errorf("roles: seeding permissions for role %s: %w", br.Name, err)
		}
	}
	return nil
}
