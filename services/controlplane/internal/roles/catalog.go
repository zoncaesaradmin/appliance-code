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

	PermWorkProfilesRead     = "work_profiles.read"
	PermWorkspacesCreate     = "workspaces.create"
	PermWorkspacesReadSelf   = "workspaces.read.self"
	PermWorkspacesReadAny    = "workspaces.read.any"
	PermWorkspacesDeleteSelf = "workspaces.delete.self"
	PermWorkspacesDeleteAny  = "workspaces.delete.any"
	PermBuildTargetsRead     = "build_targets.read"
	PermJobsReadSelf         = "jobs.read.self"
	PermJobsReadAny          = "jobs.read.any"
	PermJobsCancelSelf       = "jobs.cancel.self"
	PermJobsCancelAny        = "jobs.cancel.any"

	PermArtifactsRead       = "artifacts.read"
	PermArtifactsWrite      = "artifacts.write"
	PermArtifactsDeleteSelf = "artifacts.delete.self"
	PermArtifactsDeleteAny  = "artifacts.delete.any"

	PermOperationsReadSelf = "operations.read.self"
	PermOperationsReadAny  = "operations.read.any"

	PermArtifactsDelete      = "artifacts.delete"
	PermArtifactsGrantsRead  = "artifacts.grants.read"
	PermArtifactsGrantsWrite = "artifacts.grants.write"

	PermHostRead  = "host.read"
	PermHostWrite = "host.write"

	PermDNSRecordsRead     = "dns.records.read"
	PermDNSRecordsWrite    = "dns.records.write"
	PermDNSRecordsRegister = "dns.records.register"

	// PermDNSPublish lets any appliance (base capability) publish its
	// name/IP to a remote DNS appliance via POST /api/v1/dns/publish.
	PermDNSPublish = "dns.publish"

	PermMCPInvoke = "mcp.invoke"

	PermSystemRead    = "system.read"
	PermSystemOperate = "system.operate"
	PermAuditRead     = "audit.read"
	PermAuditExport   = "audit.export"

	PermLicensingRead            = "licensing.read"
	PermLicensingManage          = "licensing.manage"
	PermMetadataRead             = "metadata.read"
	PermMetadataManage           = "metadata.manage"
	PermProfilesRead             = "profiles.read"
	PermProfilesActivate         = "profiles.activate"
	PermNotificationsRead        = "notifications.read"
	PermNotificationsAcknowledge = "notifications.acknowledge"
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

	{Name: PermWorkProfilesRead, Description: "Read developer workflow profiles"},
	{Name: PermWorkspacesCreate, Description: "Create own developer workspaces"},
	{Name: PermWorkspacesReadSelf, Description: "Read own developer workspaces"},
	{Name: PermWorkspacesReadAny, Description: "Read any developer workspace"},
	{Name: PermWorkspacesDeleteSelf, Description: "Delete own developer workspaces"},
	{Name: PermWorkspacesDeleteAny, Description: "Delete any developer workspace"},
	{Name: PermBuildTargetsRead, Description: "Read configured build targets"},
	{Name: PermJobsReadSelf, Description: "Read own developer workflow jobs"},
	{Name: PermJobsReadAny, Description: "Read any developer workflow job"},
	{Name: PermJobsCancelSelf, Description: "Cancel own developer workflow jobs"},
	{Name: PermJobsCancelAny, Description: "Cancel any developer workflow job"},

	{Name: PermArtifactsRead, Description: "Read artifact metadata, OCI artifact content, and appliance-managed file content"},
	{Name: PermArtifactsWrite, Description: "Push OCI artifacts and upload appliance-managed file artifacts"},
	{Name: PermArtifactsDeleteSelf, Description: "Delete artifacts produced by own builds"},
	{Name: PermArtifactsDeleteAny, Description: "Delete any artifact"},

	{Name: PermOperationsReadSelf, Description: "Read own durable operations"},
	{Name: PermOperationsReadAny, Description: "Read any durable operation"},

	{Name: PermArtifactsDelete, Description: "Delete artifact repository content"},
	{Name: PermArtifactsGrantsRead, Description: "Read artifact repository-prefix grants"},
	{Name: PermArtifactsGrantsWrite, Description: "Manage artifact repository-prefix grants"},

	{Name: PermHostRead, Description: "Read host health, stats, identity, and wifi-ap status information"},
	{Name: PermHostWrite, Description: "Change host-managed capabilities such as the management wifi access point"},

	{Name: PermDNSRecordsRead, Description: "Read LAN DNS A records"},
	{Name: PermDNSRecordsWrite, Description: "Create, update, or delete any LAN DNS A record"},
	{Name: PermDNSRecordsRegister, Description: "Register or renew owned LAN DNS A records"},
	{Name: PermDNSPublish, Description: "Publish this appliance's DNS name and IP to a remote DNS appliance"},

	{Name: PermMCPInvoke, Description: "Invoke MCP tools"},

	{Name: PermSystemRead, Description: "Read system status and version"},
	{Name: PermSystemOperate, Description: "Perform system operations"},
	{Name: PermAuditRead, Description: "Read audit events"},
	{Name: PermAuditExport, Description: "Export audit events"},

	{Name: PermLicensingRead, Description: "Read appliance licensing status and entitlements"},
	{Name: PermLicensingManage, Description: "Import licenses and accept base entitlements"},
	{Name: PermMetadataRead, Description: "Read active appliance metadata bundle status"},
	{Name: PermMetadataManage, Description: "Validate, install, or roll back appliance metadata bundles"},
	{Name: PermProfilesRead, Description: "Read appliance profile catalog from the active metadata bundle"},
	{Name: PermProfilesActivate, Description: "Validate and activate appliance profiles"},
	{Name: PermNotificationsRead, Description: "Read system notifications"},
	{Name: PermNotificationsAcknowledge, Description: "Acknowledge system notifications"},
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
			PermWorkProfilesRead, PermWorkspacesCreate, PermWorkspacesReadSelf, PermWorkspacesDeleteSelf,
			PermBuildTargetsRead, PermBuildsCreate, PermBuildsReadSelf, PermBuildsCancelSelf,
			PermJobsReadSelf, PermJobsCancelSelf,
			PermArtifactsRead, PermArtifactsWrite, PermArtifactsDeleteSelf,
			PermHostRead, PermOperationsReadSelf,
			PermMCPInvoke,
		},
	},
	{
		ID:   ViewerRoleID,
		Name: Viewer,
		Permissions: []string{
			PermTokensReadSelf, PermTokensCreateSelf, PermTokensRevokeSelf,
			PermWorkProfilesRead, PermWorkspacesReadAny, PermBuildTargetsRead,
			PermBuildsReadAny, PermJobsReadAny, PermArtifactsRead, PermHostRead,
			PermOperationsReadSelf,
			PermDNSRecordsRead,
			PermNotificationsRead,
			PermLicensingRead,
		},
	},
	{
		ID:   AutomationRoleID,
		Name: Automation,
		Permissions: []string{
			PermWorkProfilesRead, PermWorkspacesCreate, PermWorkspacesReadSelf,
			PermBuildTargetsRead, PermBuildsCreate, PermBuildsReadSelf, PermBuildsCancelSelf,
			PermJobsReadSelf, PermJobsCancelSelf,
			PermArtifactsRead, PermArtifactsWrite, PermHostRead,
			PermOperationsReadSelf,
			PermDNSRecordsRegister,
			PermDNSPublish,
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
