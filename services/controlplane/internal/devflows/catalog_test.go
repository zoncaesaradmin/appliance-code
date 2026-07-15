package devflows

import "testing"

func TestCatalogValidatesAndResolvesAlias(t *testing.T) {
	catalog := testCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := catalog.ResolveTarget("app", "app")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if resolved.Target.ScriptPath != DefaultRepoScriptPath {
		t.Errorf("ScriptPath = %q, want default", resolved.Target.ScriptPath)
	}
	if resolved.Repo.SourceCredentialRef != "git-main" {
		t.Errorf("credential ref = %q", resolved.Repo.SourceCredentialRef)
	}
}

func TestCatalogRejectsDuplicateAlias(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets = append(catalog.BuildTargets, BuildTarget{Name: "other", Aliases: []string{"app"}, Repo: "app", Execution: ExecutionRepoScript, ImageRepository: "users/alice/other", BuilderImageDigest: "buildah@sha256:approved"})
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject duplicate alias")
	}
}

func TestCatalogRejectsUnknownProfileRepoMembership(t *testing.T) {
	catalog := testCatalog()
	catalog.WorkProfiles[0].Repos = append(catalog.WorkProfiles[0].Repos, ProfileRepo{Name: "missing"})
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject workspace profile repo membership that references an unknown repo")
	}
}

func TestCatalogRejectsMissingBuildTargets(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets = nil
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject catalogs with no build targets")
	}
}

func TestSourceCredentialSecretsUseManagedNames(t *testing.T) {
	if got := SourceCredentialNamespace(); got != "appliance-builds" {
		t.Fatalf("SourceCredentialNamespace() = %q, want appliance-builds", got)
	}
	if got := SourceCredentialSecretName("git.main_repo"); got != "builder-git-git-main-repo-key" {
		t.Fatalf("SourceCredentialSecretName() = %q", got)
	}
	if got := SourceCredentialKnownHostsSecretName("git.main_repo"); got != "builder-git-git-main-repo-known-hosts" {
		t.Fatalf("SourceCredentialKnownHostsSecretName() = %q", got)
	}
}

func TestCatalogRejectsSSHRepoWithoutCredentialRef(t *testing.T) {
	catalog := testCatalog()
	catalog.Repos[0].SourceCredentialRef = ""
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject SSH repos without sourceCredentialRef")
	}
}

func TestCatalogRejectsCredentialHostMismatch(t *testing.T) {
	catalog := testCatalog()
	catalog.SourceCredentials[0].GitHost = "other.example.com"
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject credential host mismatch")
	}
}

func TestCatalogRejectsUnsafeExecutionPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Catalog)
	}{
		{
			name: "absolute script path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].ScriptPath = "/tmp/build.sh"
			},
		},
		{
			name: "traversal script path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].ScriptPath = "../build.sh"
			},
		},
		{
			name: "dot segment script path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].ScriptPath = "./build.sh"
			},
		},
		{
			name: "traversal containerfile path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].ContainerfilePath = "deploy/../../Containerfile"
			},
		},
		{
			name: "dot segment containerfile path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].ContainerfilePath = "deploy/./Containerfile"
			},
		},
		{
			name: "unsafe make target",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].Execution = ExecutionMakeTarget
				c.BuildTargets[0].MakeTarget = "image && whoami"
			},
		},
		{
			name: "tag-only builder image",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].BuilderImageDigest = "buildah:latest"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := testCatalog()
			tc.mutate(&catalog)
			if err := catalog.Validate(); err == nil {
				t.Fatal("Validate should reject unsafe execution input")
			}
		})
	}
}

func testCatalog() Catalog {
	return Catalog{
		WorkProfiles:      []WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		SourceCredentials: []SourceCredential{{ID: "git-main", GitHost: "git.internal.example.com"}},
		Repos:             []Repo{{Name: "app", URL: "git@git.internal.example.com:team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567", SourceCredentialRef: "git-main"}},
		BuildTargets:      []BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: ExecutionRepoScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}", BuilderImageDigest: "buildah@sha256:approved"}},
	}
}
