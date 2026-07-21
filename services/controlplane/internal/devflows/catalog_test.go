package devflows

import "testing"

func TestCatalogNormalizesNestedBuildTargets(t *testing.T) {
	catalog := Catalog{
		WorkProfiles: []WorkProfile{{Name: "builder", Repos: []ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos: []Repo{{
			Name:       "app",
			URL:        "https://git.internal.example.com/team/app.git",
			DefaultRef: "main",
			BuildTargets: []BuildTarget{
				{Name: "app", Execution: ExecutionMake, Args: []string{"build"}, ImageRepository: "users/alice/app"},
				{Name: "app-api", Execution: ExecutionMake, Args: []string{"api"}, ImageRepository: "users/alice/app-api"},
			},
		}},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	catalog.Normalize()
	if len(catalog.BuildTargets) != 2 {
		t.Fatalf("BuildTargets = %+v, want 2 after normalize", catalog.BuildTargets)
	}
	if catalog.Repos[0].BuildTargets != nil {
		t.Fatalf("nested BuildTargets should be cleared after normalize: %+v", catalog.Repos[0].BuildTargets)
	}
	if catalog.BuildTargets[0].Repo != "app" || catalog.BuildTargets[1].Repo != "app" {
		t.Fatalf("lifted targets missing repo: %+v", catalog.BuildTargets)
	}
}

func TestCatalogValidatesAndResolvesAlias(t *testing.T) {
	catalog := testCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := catalog.ResolveTarget("app", "app")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if len(resolved.Target.Args) != 1 || resolved.Target.Args[0] != DefaultScriptArg {
		t.Errorf("Args = %#v, want [%q]", resolved.Target.Args, DefaultScriptArg)
	}
	if resolved.Repo.URL != "https://git.internal.example.com/team/app.git" {
		t.Errorf("repo url = %q", resolved.Repo.URL)
	}
}

func TestCatalogRejectsDuplicateAlias(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets = append(catalog.BuildTargets, BuildTarget{Name: "other", Aliases: []string{"app"}, Repo: "app", Execution: ExecutionScript, ImageRepository: "users/alice/other"})
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

func TestCatalogAllowsWorkspaceOnlyCatalog(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets = nil
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate should accept workspace-only catalogs: %v", err)
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
				c.BuildTargets[0].Execution = ExecutionScript
				c.BuildTargets[0].Args = []string{"/tmp/build.sh"}
			},
		},
		{
			name: "traversal script path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].Execution = ExecutionScript
				c.BuildTargets[0].Args = []string{"../build.sh"}
			},
		},
		{
			name: "dot segment script path",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].Execution = ExecutionScript
				c.BuildTargets[0].Args = []string{"./build.sh"}
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
				c.BuildTargets[0].Execution = ExecutionMake
				c.BuildTargets[0].Args = []string{"image && whoami"}
			},
		},
		{
			name: "unknown builder image name",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].BuilderImageDigest = "custom-builder"
			},
		},
		{
			name: "tag-only builder image",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].BuilderImageDigest = "buildah:latest"
			},
		},
		{
			name: "placeholder builder image digest",
			mutate: func(c *Catalog) {
				c.BuildTargets[0].BuilderImageDigest = "registry.local/buildah@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
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

func TestCatalogAcceptsAutomationDevBuilderImage(t *testing.T) {
	catalog := testCatalog()
	catalog.BuildTargets[0].BuilderImageDigest = DefaultBuilderImageRef
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCatalogNormalizesEmptyBuilderImageToAutomationDev(t *testing.T) {
	catalog := testCatalog()
	catalog.Normalize()
	if catalog.BuildTargets[0].BuilderImageDigest != DefaultBuilderImageRef {
		t.Fatalf("BuilderImageDigest = %q, want %q", catalog.BuildTargets[0].BuilderImageDigest, DefaultBuilderImageRef)
	}
}

func TestResolveBuilderImage(t *testing.T) {
	appliance := "registry.local/automation-dev@sha256:5ccdfda08e940614d030e377b75f048a55e3f61cbb0234294ad333f27afe222c"
	got, err := ResolveBuilderImage(DefaultBuilderImageRef, appliance)
	if err != nil {
		t.Fatalf("ResolveBuilderImage: %v", err)
	}
	if got != appliance {
		t.Fatalf("got %q, want appliance digest", got)
	}
	override := "registry.local/other@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err = ResolveBuilderImage(override, appliance)
	if err != nil {
		t.Fatalf("ResolveBuilderImage override: %v", err)
	}
	if got != override {
		t.Fatalf("override got %q, want %q", got, override)
	}
}

func testCatalog() Catalog {
	return Catalog{
		WorkProfiles: []WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []Repo{{Name: "app", URL: "https://git.internal.example.com/team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: ExecutionScript, Args: []string{"build.sh"}, ImageRepository: "users/alice/app", ImageTagTemplate: "{workspace}-{target}"}},
	}
}
