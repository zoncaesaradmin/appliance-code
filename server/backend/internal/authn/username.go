package authn

import (
	"fmt"
	"regexp"
	"strings"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// NormalizeUsername lowercases and validates a candidate username against
// the ADR 0010 login-identifier policy: canonical lowercase ASCII, 3 to 64
// characters, matching [a-z][a-z0-9._-]*. Usernames are immutable once
// created because they anchor personal registry namespaces; a separate
// mutable Unicode display name carries any presentation preference.
func NormalizeUsername(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if len(normalized) < 3 || len(normalized) > 64 {
		return "", fmt.Errorf("username must be 3 to 64 characters")
	}
	if !usernamePattern.MatchString(normalized) {
		return "", fmt.Errorf("username must start with a letter and contain only lowercase letters, digits, '.', '_', or '-'")
	}
	return normalized, nil
}
