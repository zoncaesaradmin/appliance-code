package buildergit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUpsertResolveAndHostUniqueness(t *testing.T) {
	svc, err := NewService(NewMemorySecretManager(), "appliance-builds", []string{"github.com", "gitlab.example.com"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), "github-corp", "github.com", "x-access-token", "token-a"); err != nil {
		t.Fatalf("Upsert github: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), "gitlab-internal", "gitlab.example.com", "bot", "token-b"); err != nil {
		t.Fatalf("Upsert gitlab: %v", err)
	}
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Configured {
		t.Fatalf("expected configured status, got %#v", status)
	}
	if len(status.Credentials) != 2 {
		t.Fatalf("credentials = %d, want 2", len(status.Credentials))
	}
	cred, err := svc.Resolve(context.Background(), "https://github.com/org/repo.git")
	if err != nil {
		t.Fatalf("Resolve github: %v", err)
	}
	if cred.Name != "github-corp" || cred.SecretName != "git-access-github-corp" {
		t.Fatalf("unexpected github credential: %#v", cred)
	}
	many, err := svc.ResolveMany(context.Background(), []string{
		"https://github.com/org/a.git",
		"https://gitlab.example.com/team/b.git",
		"https://github.com/org/c.git",
	})
	if err != nil {
		t.Fatalf("ResolveMany: %v", err)
	}
	if len(many) != 2 {
		t.Fatalf("ResolveMany len = %d, want 2", len(many))
	}
	if _, err := svc.Upsert(context.Background(), "other-github", "github.com", "user", "token-c"); !errors.Is(err, ErrHostConflict) {
		t.Fatalf("expected host conflict, got %v", err)
	}
}

func TestMissingHostFailsClosed(t *testing.T) {
	svc, err := NewService(NewMemorySecretManager(), "appliance-builds", []string{"github.com", "gitlab.example.com"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), "github-corp", "github.com", "user", "token"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Configured {
		t.Fatalf("expected not configured while gitlab host missing")
	}
	if len(status.MissingHosts) != 1 || status.MissingHosts[0] != "gitlab.example.com" {
		t.Fatalf("missing hosts = %#v", status.MissingHosts)
	}
	_, err = svc.Resolve(context.Background(), "https://gitlab.example.com/team/repo.git")
	if !errors.Is(err, ErrHostMismatch) {
		t.Fatalf("expected host mismatch, got %v", err)
	}
}

func TestUpsertRequiresName(t *testing.T) {
	svc, err := NewService(NewMemorySecretManager(), "appliance-builds", []string{"github.com"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), "", "github.com", "user", "token"); err == nil {
		t.Fatal("expected empty name to fail")
	}
	if err := ValidateCredentialName("github-com"); err != nil {
		t.Fatalf("ValidateCredentialName: %v", err)
	}
	if err := ValidateCredentialName("Bad_Name"); err == nil {
		t.Fatal("expected invalid name")
	}
	if !strings.HasPrefix(SecretNameFor("github-com"), SecretNamePrefix) {
		t.Fatal("SecretNameFor missing prefix")
	}
}

func TestDeleteCredential(t *testing.T) {
	svc, err := NewService(NewMemorySecretManager(), "appliance-builds", []string{"github.com"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), "github-corp", "github.com", "user", "token"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := svc.Delete(context.Background(), "github-corp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Delete(context.Background(), "github-corp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
