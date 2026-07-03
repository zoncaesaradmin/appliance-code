package registryauth

import (
	"fmt"
	"strings"
)

// ScopeRequest is one parsed OCI Distribution token-scope entry, e.g.
// "repository:library/nginx:pull,push".
type ScopeRequest struct {
	Type    string
	Name    string
	Actions []string
}

// ParseScopes parses the repeated "scope" query parameters an OCI
// Distribution client sends when requesting a registry token. Only the
// "repository" scope type is supported in v1.
func ParseScopes(raw []string) ([]ScopeRequest, error) {
	scopes := make([]ScopeRequest, 0, len(raw))
	for _, s := range raw {
		parts := strings.SplitN(s, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("registryauth: malformed scope %q", s)
		}
		typ, name, actionsRaw := parts[0], parts[1], parts[2]
		if typ != "repository" {
			return nil, fmt.Errorf("registryauth: unsupported scope type %q", typ)
		}

		normalizedName, err := NormalizeRepositoryName(name)
		if err != nil {
			return nil, err
		}

		rawActions := strings.Split(actionsRaw, ",")
		actions := make([]string, 0, len(rawActions))
		for _, a := range rawActions {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if a != "pull" && a != "push" {
				return nil, fmt.Errorf("registryauth: unsupported action %q in scope %q", a, s)
			}
			actions = append(actions, a)
		}
		if len(actions) == 0 {
			return nil, fmt.Errorf("registryauth: scope %q names no actions", s)
		}

		scopes = append(scopes, ScopeRequest{Type: typ, Name: normalizedName, Actions: actions})
	}
	return scopes, nil
}
