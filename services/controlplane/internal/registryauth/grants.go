package registryauth

import (
	"context"
	"fmt"
	"strings"

	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

// grant is a normalized prefix+actions pair, whether derived from a
// built-in role's implicit defaults or a stored explicit grant.
type grant struct {
	pathPrefix string
	actions    map[string]bool
}

// implicitGrantsForRole returns the built-in role's fixed default registry
// prefixes from ADR 0010. Custom roles receive no implicit registry grants
// in v1; an administrator must assign explicit grants for them, same as for
// the automation role.
func implicitGrantsForRole(roleID, username string) []grant {
	switch roleID {
	case roles.AdministratorRoleID:
		return []grant{{pathPrefix: "", actions: actionSet("pull", "push")}}
	case roles.DeveloperRoleID:
		return []grant{
			{pathPrefix: "", actions: actionSet("pull")},
			{pathPrefix: "users/" + username + "/", actions: actionSet("pull", "push")},
			{pathPrefix: "builds/" + username + "/", actions: actionSet("pull", "push")},
		}
	case roles.ViewerRoleID:
		return []grant{{pathPrefix: "", actions: actionSet("pull")}}
	default:
		return nil
	}
}

func actionSet(actions ...string) map[string]bool {
	m := make(map[string]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}

// RoleLister is the minimal role-listing contract Authorizer depends on
// (satisfied by *roles.Service), kept narrow so this package doesn't need
// the whole roles.Service surface.
type RoleLister interface {
	ListUserRoles(ctx context.Context, userID string) ([]storage.Role, error)
}

// Authorizer combines a principal's built-in-role defaults with explicit
// stored grants to decide which of a requested scope's actions are allowed.
type Authorizer struct {
	grants storage.RegistryGrantStore
	roles  RoleLister
}

// NewAuthorizer wires an Authorizer from its storage dependencies.
func NewAuthorizer(grants storage.RegistryGrantStore, roleLister RoleLister) *Authorizer {
	return &Authorizer{grants: grants, roles: roleLister}
}

// Decision is the outcome for one requested repository scope: the actions
// actually granted, which may be a subset of what was requested. The OCI
// token contract allows returning fewer actions than requested but never
// more.
type Decision struct {
	Name    string
	Granted []string
}

// grantsFor gathers userID's complete grant set: each assigned built-in
// role's implicit defaults (expanded with username for personal prefixes)
// plus every explicit grant stored against the user or any of their roles.
func (a *Authorizer) grantsFor(ctx context.Context, userID, username string) ([]grant, error) {
	userRoles, err := a.roles.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("registryauth: listing roles for user %s: %w", userID, err)
	}

	roleIDs := make([]string, len(userRoles))
	var allGrants []grant
	for i, r := range userRoles {
		roleIDs[i] = r.ID
		allGrants = append(allGrants, implicitGrantsForRole(r.ID, username)...)
	}

	explicit, err := a.grants.ListForSubjects(ctx, userID, roleIDs)
	if err != nil {
		return nil, fmt.Errorf("registryauth: listing explicit registry grants: %w", err)
	}
	for _, g := range explicit {
		allGrants = append(allGrants, grant{pathPrefix: g.PathPrefix, actions: actionSet(g.Actions...)})
	}
	return allGrants, nil
}

// Authorize resolves the actions granted to userID (with the given
// username, used to expand personal-prefix defaults) for each requested
// scope. permissions is the principal's already-scope-intersected effective
// permission set; a base registry.pull/registry.push permission is
// required before any prefix grant is even considered for that action, per
// the plan's "intersects role permission, prefix grant, ... and optional
// API-token scope" requirement.
func (a *Authorizer) Authorize(ctx context.Context, userID, username string, permissions map[string]bool, requests []ScopeRequest) ([]Decision, error) {
	allGrants, err := a.grantsFor(ctx, userID, username)
	if err != nil {
		return nil, err
	}

	decisions := make([]Decision, 0, len(requests))
	for _, req := range requests {
		var granted []string
		for _, action := range req.Actions {
			if !hasBasePermission(permissions, action) {
				continue
			}
			if matchesAnyGrant(allGrants, req.Name, action) {
				granted = append(granted, action)
			}
		}
		decisions = append(decisions, Decision{Name: req.Name, Granted: granted})
	}
	return decisions, nil
}

// CanPull reports whether userID may pull repoName, combining the base
// registry.pull permission with prefix-grant matching the same way
// Authorize does for a single repository and action.
func (a *Authorizer) CanPull(ctx context.Context, userID, username string, permissions map[string]bool, repoName string) (bool, error) {
	if !hasBasePermission(permissions, "pull") {
		return false, nil
	}
	allGrants, err := a.grantsFor(ctx, userID, username)
	if err != nil {
		return false, err
	}
	return matchesAnyGrant(allGrants, repoName, "pull"), nil
}

// FilterPullable narrows repoNames to those userID may pull. Names that
// fail repository-name normalization are skipped rather than erroring, since
// callers pass catalog entries zot itself already considers valid.
func (a *Authorizer) FilterPullable(ctx context.Context, userID, username string, permissions map[string]bool, repoNames []string) ([]string, error) {
	if !hasBasePermission(permissions, "pull") {
		return nil, nil
	}
	allGrants, err := a.grantsFor(ctx, userID, username)
	if err != nil {
		return nil, err
	}

	pullable := make([]string, 0, len(repoNames))
	for _, name := range repoNames {
		normalized, err := NormalizeRepositoryName(name)
		if err != nil {
			continue
		}
		if matchesAnyGrant(allGrants, normalized, "pull") {
			pullable = append(pullable, name)
		}
	}
	return pullable, nil
}

func hasBasePermission(permissions map[string]bool, action string) bool {
	switch action {
	case "pull":
		return permissions[roles.PermRegistryPull]
	case "push":
		return permissions[roles.PermRegistryPush]
	default:
		return false
	}
}

func matchesAnyGrant(grants []grant, repoName, action string) bool {
	for _, g := range grants {
		if g.actions[action] && strings.HasPrefix(repoName, g.pathPrefix) {
			return true
		}
	}
	return false
}
