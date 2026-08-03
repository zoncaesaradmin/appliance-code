package authn

import (
	"errors"
	"fmt"
	"strings"
)

// AuthDomainLocal is the only authentication domain supported in V1: the
// appliance-local user store (not a DNS zone or security realm name).
const AuthDomainLocal = "local"

// SupportedAuthDomains lists every authentication domain the control plane
// accepts today. New IdP domains are added here and to NormalizeAuthDomain.
var SupportedAuthDomains = []string{AuthDomainLocal}

// ErrUnsupportedAuthDomain is returned when login requests a domain other
// than one listed in SupportedAuthDomains.
var ErrUnsupportedAuthDomain = errors.New("authn: unsupported authentication domain")

// NormalizeAuthDomain trims and lowercases domain. Omitted or empty values
// (including whitespace-only) default to AuthDomainLocal so API clients may
// leave the field unset. Non-supported domains are rejected.
func NormalizeAuthDomain(domain string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(domain))
	if normalized == "" {
		normalized = AuthDomainLocal
	}
	for _, supported := range SupportedAuthDomains {
		if normalized == supported {
			return normalized, nil
		}
	}
	return "", fmt.Errorf("%w: %q (supported: %s)", ErrUnsupportedAuthDomain, normalized, strings.Join(SupportedAuthDomains, ", "))
}
