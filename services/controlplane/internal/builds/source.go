// Package builds owns build request business logic: submitting one build
// per request as an isolated workflows.Engine run, reconciling its status
// into durable storage, cancellation, and log access. It does not own
// Argo/Buildah specifics; internal/workflows and internal/workflows/argo
// (not implemented in this pass, see that package's doc comment) own that.
package builds

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidateSource checks repoURL and commitSHA against the plan's build input
// invariants: an allowlisted HTTPS Git source at an immutable full commit SHA.
func ValidateSource(repoURL, commitSHA string, allowedHosts []string) error {
	if !commitSHAPattern.MatchString(commitSHA) {
		return fmt.Errorf("builds: commit SHA must be exactly 40 lowercase hexadecimal characters")
	}

	host, err := gitSourceHost(repoURL)
	if err != nil {
		return err
	}
	if len(allowedHosts) == 0 {
		return fmt.Errorf("builds: no allowlisted git source hosts are configured")
	}
	for _, allowed := range allowedHosts {
		if strings.EqualFold(host, allowed) {
			return nil
		}
	}
	return fmt.Errorf("builds: source host %q is not an allowlisted git source", host)
}

func gitSourceHost(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("builds: source repository URL is required")
	}
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("builds: invalid source repository URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("builds: source repository URL must use https")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("builds: source repository URL must include a host")
	}
	return strings.ToLower(u.Hostname()), nil
}

func IsSSHSource(repoURL string) bool {
	return false
}

// ValidateBuilderImage checks digest against the configured builder image
// allowlist. An empty allowlist means unrestricted builder selection for
// isolated tests, but the image reference must still be digest-pinned.
func ValidateBuilderImage(digest string, allowed []string) error {
	if !strings.Contains(strings.TrimSpace(digest), "@sha256:") {
		return fmt.Errorf("builds: builder image must be digest-pinned")
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if a == digest {
			return nil
		}
	}
	return fmt.Errorf("builds: builder image %q is not an approved builder image", digest)
}
