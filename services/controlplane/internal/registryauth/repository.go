// Package registryauth owns OCI Distribution token-service concerns: scope
// parsing, repository-name normalization, repository-prefix grant
// evaluation, and registry access-token signing. It knows nothing about
// zot's API or storage format; internal/zotadapter owns that boundary.
package registryauth

import (
	"fmt"
	"strings"
)

// NormalizeRepositoryName validates and lowercases a requested repository
// path per ADR 0010: lowercase slash-separated segments, and it rejects
// empty, traversal, or non-canonical segments.
func NormalizeRepositoryName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return "", fmt.Errorf("registryauth: repository name must not be empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("registryauth: repository name must not have leading or trailing slashes")
	}

	segments := strings.Split(name, "/")
	for _, seg := range segments {
		if err := validateSegment(seg); err != nil {
			return "", err
		}
	}
	return name, nil
}

func validateSegment(seg string) error {
	if seg == "" {
		return fmt.Errorf("registryauth: repository name must not contain empty segments")
	}
	if seg == "." || seg == ".." {
		return fmt.Errorf("registryauth: repository name must not contain traversal segments")
	}
	for _, r := range seg {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		isPunct := r == '-' || r == '_' || r == '.'
		if !isLower && !isDigit && !isPunct {
			return fmt.Errorf("registryauth: repository segment %q contains an invalid character", seg)
		}
	}
	return nil
}

// NormalizePathPrefix validates and lowercases a registry-grant path
// prefix. Unlike a repository name, a prefix may be empty (meaning "every
// repository") or end in a single trailing slash (meaning "this path and
// everything under it").
func NormalizePathPrefix(raw string) (string, error) {
	prefix := strings.ToLower(strings.TrimSpace(raw))
	if prefix == "" {
		return "", nil
	}
	if strings.HasPrefix(prefix, "/") {
		return "", fmt.Errorf("registryauth: path prefix must not have a leading slash")
	}

	trimmed := strings.TrimSuffix(prefix, "/")
	for _, seg := range strings.Split(trimmed, "/") {
		if err := validateSegment(seg); err != nil {
			return "", err
		}
	}
	return prefix, nil
}
