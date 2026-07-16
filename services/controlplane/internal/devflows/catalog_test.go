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
	if resolved.Repo.URL != "git@git.internal.example.com:team/app.git" {
		t.Errorf("repo url = %q", resolved.Repo.URL)
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

func TestCatalogTargetsForProfile(t *testing.T) {
	catalog := testCatalog()
	targets, err := catalog.TargetsForProfile("builder")
	if err != nil {
		t.Fatalf("TargetsForProfile: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "default" {
		t.Fatalf("TargetsForProfile returned %+v, want one default target", targets)
	}
}

func TestCatalogResolveTargetForProfile(t *testing.T) {
	catalog := testCatalog()
	resolved, err := catalog.ResolveTargetForProfile("builder", "app")
	if err != nil {
		t.Fatalf("ResolveTargetForProfile: %v", err)
	}
	if resolved.Target.Name != "default" {
		t.Fatalf("resolved target name = %q, want default", resolved.Target.Name)
	}
	if resolved.Repo.Name != "app" {
		t.Fatalf("resolved repo name = %q, want app", resolved.Repo.Name)
	}
}

func TestCatalogRejectsMissingBuildTargets(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets = nil
	if err := catalog.Validate(); err == nil {
		t.Fatal("Validate should reject catalogs with no build targets")
	}
}

func TestBuilderGitSecretsUseManagedNames(t *testing.T) {
	if got := BuilderGitNamespace(); got != "appliance-builds" {
		t.Fatalf("BuilderGitNamespace() = %q, want appliance-builds", got)
	}
	if got := BuilderGitSecretName(); got != "builder-git-key" {
		t.Fatalf("BuilderGitSecretName() = %q", got)
	}
	if got := BuilderGitKnownHostsSecretName(); got != "builder-git-known-hosts" {
		t.Fatalf("BuilderGitKnownHostsSecretName() = %q", got)
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
		WorkProfiles: []WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []Repo{{Name: "app", URL: "git@git.internal.example.com:team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: ExecutionRepoScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}", BuilderImageDigest: "buildah@sha256:approved"}},
	}
}
