package builds

import "testing"

func TestValidateSourceRejectsWhenNoAllowedHostsConfigured(t *testing.T) {
	err := ValidateSource("https://git.example.internal/team/repo.git", "0123456789abcdef0123456789abcdef01234567", nil)
	if err == nil {
		t.Fatal("ValidateSource should fail closed when no git hosts are configured")
	}
}

func TestValidateSourceAcceptsAllowedHost(t *testing.T) {
	err := ValidateSource(
		"https://git.example.internal/team/repo.git",
		"0123456789abcdef0123456789abcdef01234567",
		[]string{"git.example.internal"},
	)
	if err != nil {
		t.Fatalf("ValidateSource returned error: %v", err)
	}
}

func TestValidateBuilderImageAllowsAllWhenNoPolicyConfigured(t *testing.T) {
	if err := ValidateBuilderImage("buildah@sha256:anything", nil); err != nil {
		t.Fatalf("ValidateBuilderImage returned error: %v", err)
	}
}

func TestValidateBuilderImageRejectsUnapprovedDigestWhenPolicyConfigured(t *testing.T) {
	err := ValidateBuilderImage("buildah@sha256:other", []string{"buildah@sha256:approved"})
	if err == nil {
		t.Fatal("ValidateBuilderImage should reject digests outside the configured allowlist")
	}
}
