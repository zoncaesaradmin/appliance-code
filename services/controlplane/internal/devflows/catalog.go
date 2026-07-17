// Package devflows owns the appliance-native developer workflow catalog and
// service layer. It preserves ForgeLine's server-side concepts without making
// the appliance depend on the old ForgeLine runtime.
package devflows

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	ExecutionRepoScript = "repo_script"
	ExecutionMakeTarget = "make_target"

	DefaultRepoScriptPath             = "build.sh"
	managedBuilderGitNamespace        = "appliance-builds"
	managedBuilderGitSecretName       = "builder-git-key"
	managedBuilderGitKnownHostsSecret = "builder-git-known-hosts"
)

var (
	nameRE       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	ociRepoRE    = regexp.MustCompile(`^[a-z0-9]+([._/-][a-z0-9]+)*$`)
	commitShaRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	makeTargetRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

// Catalog is product configuration for developer workflows.
type Catalog struct {
	WorkspaceProvisionerImageDigest string        `json:"workspaceProvisionerImageDigest,omitempty"`
	WorkProfiles                    []WorkProfile `json:"workProfiles"`
	Repos                           []Repo        `json:"repos"`
	BuildTargets                    []BuildTarget `json:"buildTargets"`
}

type WorkProfile struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Repos       []ProfileRepo `json:"repos,omitempty"`
}

type ProfileRepo struct {
	Name             string `json:"name"`
	EnabledByDefault bool   `json:"enabledByDefault,omitempty"`
}

type Repo struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	DefaultRef string `json:"defaultRef,omitempty"`
}

type BuildTarget struct {
	Name               string   `json:"name"`
	Aliases            []string `json:"aliases,omitempty"`
	Description        string   `json:"description,omitempty"`
	Repo               string   `json:"repo"`
	Execution          string   `json:"execution"`
	ScriptPath         string   `json:"scriptPath,omitempty"`
	MakeTarget         string   `json:"makeTarget,omitempty"`
	ContainerfilePath  string   `json:"containerfilePath,omitempty"`
	ImageRepository    string   `json:"imageRepository"`
	ImageTagTemplate   string   `json:"imageTagTemplate,omitempty"`
	BuilderImageDigest string   `json:"builderImageDigest"`
}

type ResolvedTarget struct {
	Target BuildTarget
	Repo   Repo
}

func (c Catalog) Empty() bool {
	return len(c.WorkProfiles) == 0 && len(c.Repos) == 0 && len(c.BuildTargets) == 0 && strings.TrimSpace(c.WorkspaceProvisionerImageDigest) == ""
}

func (c Catalog) Validate() error {
	var errs []string
	if len(c.BuildTargets) == 0 {
		errs = append(errs, "build catalog must declare at least one build target")
	}
	if digest := strings.TrimSpace(c.WorkspaceProvisionerImageDigest); digest != "" && !validBuilderImageDigest(digest) {
		errs = append(errs, "workspace provisioner image digest must be digest-pinned")
	}
	profiles := map[string]struct{}{}
	for _, p := range c.WorkProfiles {
		name := normalizeName(p.Name)
		if !validName(name) {
			errs = append(errs, fmt.Sprintf("workspace profile %q has an invalid name", p.Name))
			continue
		}
		if _, exists := profiles[name]; exists {
			errs = append(errs, fmt.Sprintf("workspace profile %q is duplicated", name))
		}
		profiles[name] = struct{}{}
		seenProfileRepos := map[string]struct{}{}
		for _, profileRepo := range p.Repos {
			repoName := normalizeName(profileRepo.Name)
			if !validName(repoName) {
				errs = append(errs, fmt.Sprintf("workspace profile %q repo %q has an invalid name", name, profileRepo.Name))
				continue
			}
			if _, exists := seenProfileRepos[repoName]; exists {
				errs = append(errs, fmt.Sprintf("workspace profile %q repo %q is duplicated", name, repoName))
				continue
			}
			seenProfileRepos[repoName] = struct{}{}
		}
	}

	repos := map[string]Repo{}
	for _, repo := range c.Repos {
		name := normalizeName(repo.Name)
		if !validName(name) {
			errs = append(errs, fmt.Sprintf("repo %q has an invalid name", repo.Name))
			continue
		}
		host, err := sourceHost(repo.URL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("repo %q has invalid url: %v", name, err))
		}
		_ = host
		if _, exists := repos[name]; exists {
			errs = append(errs, fmt.Sprintf("repo %q is duplicated", name))
		}
		repo.Name = name
		repos[name] = repo
	}

	seenTargetNames := map[string]string{}
	for _, target := range c.BuildTargets {
		name := normalizeName(target.Name)
		if !validName(name) {
			errs = append(errs, fmt.Sprintf("build target %q has an invalid name", target.Name))
			continue
		}
		if prev, exists := seenTargetNames[name]; exists {
			errs = append(errs, fmt.Sprintf("build target name/alias %q is duplicated by %q and %q", name, prev, name))
		}
		seenTargetNames[name] = name
		for _, alias := range target.Aliases {
			alias = normalizeName(alias)
			if !validName(alias) {
				errs = append(errs, fmt.Sprintf("build target %q has invalid alias %q", name, alias))
				continue
			}
			if prev, exists := seenTargetNames[alias]; exists {
				errs = append(errs, fmt.Sprintf("build target alias %q is duplicated by %q and %q", alias, prev, name))
			}
			seenTargetNames[alias] = name
		}
		if _, ok := repos[normalizeName(target.Repo)]; !ok {
			errs = append(errs, fmt.Sprintf("build target %q references unknown repo %q", name, target.Repo))
		}
		switch target.Execution {
		case ExecutionRepoScript:
			if target.ScriptPath != "" && !validRepoRelativePath(target.ScriptPath) {
				errs = append(errs, fmt.Sprintf("build target %q has invalid scriptPath %q", name, target.ScriptPath))
			}
		case ExecutionMakeTarget:
			if strings.TrimSpace(target.MakeTarget) == "" {
				errs = append(errs, fmt.Sprintf("build target %q make_target execution requires makeTarget", name))
			} else if !validMakeTarget(target.MakeTarget) {
				errs = append(errs, fmt.Sprintf("build target %q has invalid makeTarget %q", name, target.MakeTarget))
			}
		default:
			errs = append(errs, fmt.Sprintf("build target %q has unsupported execution %q", name, target.Execution))
		}
		if target.ContainerfilePath != "" && !validRepoRelativePath(target.ContainerfilePath) {
			errs = append(errs, fmt.Sprintf("build target %q has invalid containerfilePath %q", name, target.ContainerfilePath))
		}
		if !ociRepoRE.MatchString(target.ImageRepository) {
			errs = append(errs, fmt.Sprintf("build target %q has invalid imageRepository %q", name, target.ImageRepository))
		}
		if strings.TrimSpace(target.BuilderImageDigest) == "" {
			errs = append(errs, fmt.Sprintf("build target %q requires builderImageDigest", name))
		} else if !validBuilderImageDigest(target.BuilderImageDigest) {
			errs = append(errs, fmt.Sprintf("build target %q builderImageDigest must be digest-pinned", name))
		}
	}

	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("devflows catalog: %s", strings.Join(errs, "; "))
	}
	for _, p := range c.WorkProfiles {
		profileName := normalizeName(p.Name)
		if len(p.Repos) == 0 {
			return fmt.Errorf("devflows catalog: workspace profile %q must declare at least one repo", profileName)
		}
		for _, profileRepo := range p.Repos {
			repoName := normalizeName(profileRepo.Name)
			if _, ok := repos[repoName]; !ok {
				return fmt.Errorf("devflows catalog: workspace profile %q references unknown repo %q", profileName, profileRepo.Name)
			}
		}
	}
	return nil
}

func (c Catalog) ResolveTarget(repoName, name string) (ResolvedTarget, error) {
	repoName = normalizeName(repoName)
	lookup := normalizeName(name)
	for _, target := range c.BuildTargets {
		if normalizeName(target.Repo) != repoName {
			continue
		}
		if normalizeName(target.Name) != lookup && !containsName(target.Aliases, lookup) {
			continue
		}
		for _, repo := range c.Repos {
			if normalizeName(repo.Name) == normalizeName(target.Repo) {
				if target.ScriptPath == "" && target.Execution == ExecutionRepoScript {
					target.ScriptPath = DefaultRepoScriptPath
				}
				if target.ContainerfilePath == "" {
					target.ContainerfilePath = "Containerfile"
				}
				return ResolvedTarget{Target: target, Repo: repo}, nil
			}
		}
	}
	return ResolvedTarget{}, fmt.Errorf("devflows: unknown build target %q", name)
}

func (c Catalog) WorkProfile(name string) (WorkProfile, bool) {
	name = normalizeName(name)
	for _, p := range c.WorkProfiles {
		if normalizeName(p.Name) == name {
			p.Name = name
			for i := range p.Repos {
				p.Repos[i].Name = normalizeName(p.Repos[i].Name)
			}
			return p, true
		}
	}
	return WorkProfile{}, false
}

func (c Catalog) RepoHosts() ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, repo := range c.Repos {
		host, err := sourceHost(repo.URL)
		if err != nil {
			return nil, fmt.Errorf("repo %q has invalid url: %w", repo.Name, err)
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	sort.Strings(out)
	return out, nil
}

func (c Catalog) BuilderImageDigests() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, target := range c.BuildTargets {
		ref := strings.TrimSpace(target.BuilderImageDigest)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func (c Catalog) SensitiveLogValues() []string {
	return nil
}

func (c Catalog) Repo(name string) (Repo, bool) {
	name = normalizeName(name)
	for _, r := range c.Repos {
		if normalizeName(r.Name) == name {
			r.Name = name
			return r, true
		}
	}
	return Repo{}, false
}

func (c Catalog) ReposForProfile(workProfile string) ([]Repo, error) {
	profile, ok := c.WorkProfile(workProfile)
	if !ok {
		return nil, fmt.Errorf("devflows: unknown workspace profile %q", workProfile)
	}
	out := make([]Repo, 0, len(profile.Repos))
	for _, profileRepo := range profile.Repos {
		repo, ok := c.Repo(profileRepo.Name)
		if !ok {
			return nil, fmt.Errorf("devflows: unknown repo %q for workspace profile %q", profileRepo.Name, workProfile)
		}
		out = append(out, repo)
	}
	return out, nil
}

func (c Catalog) WorkspaceProvisionerImageDigestForProfile(workProfile string) (string, error) {
	if digest := strings.TrimSpace(c.WorkspaceProvisionerImageDigest); digest != "" {
		return digest, nil
	}
	targets, err := c.TargetsForProfile(workProfile)
	if err != nil {
		return "", err
	}
	seen := map[string]struct{}{}
	var digests []string
	for _, target := range targets {
		digest := strings.TrimSpace(target.BuilderImageDigest)
		if digest == "" {
			continue
		}
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		digests = append(digests, digest)
	}
	switch len(digests) {
	case 0:
		return "", fmt.Errorf("devflows: workspace profile %q does not resolve any builder image digest for workspace provisioning", workProfile)
	case 1:
		return digests[0], nil
	default:
		sort.Strings(digests)
		return "", fmt.Errorf("devflows: workspace profile %q resolves multiple builder image digests; set workspaceProvisionerImageDigest explicitly", workProfile)
	}
}

func (c Catalog) ProfileAllowsRepo(workProfile, repoName string) bool {
	profile, ok := c.WorkProfile(workProfile)
	if !ok {
		return false
	}
	repoName = normalizeName(repoName)
	for _, repo := range profile.Repos {
		if normalizeName(repo.Name) == repoName {
			return true
		}
	}
	return false
}

func (c Catalog) TargetsForRepo(repoName string) []BuildTarget {
	repoName = normalizeName(repoName)
	var out []BuildTarget
	for _, target := range c.BuildTargets {
		if normalizeName(target.Repo) != repoName {
			continue
		}
		if target.ScriptPath == "" && target.Execution == ExecutionRepoScript {
			target.ScriptPath = DefaultRepoScriptPath
		}
		if target.ContainerfilePath == "" {
			target.ContainerfilePath = "Containerfile"
		}
		out = append(out, target)
	}
	return out
}

func (c Catalog) TargetsForProfile(workProfile string) ([]BuildTarget, error) {
	profile, ok := c.WorkProfile(workProfile)
	if !ok {
		return nil, fmt.Errorf("devflows: unknown workspace profile %q", workProfile)
	}
	allowed := map[string]struct{}{}
	for _, repo := range profile.Repos {
		allowed[normalizeName(repo.Name)] = struct{}{}
	}
	var out []BuildTarget
	for _, target := range c.BuildTargets {
		if _, ok := allowed[normalizeName(target.Repo)]; !ok {
			continue
		}
		if target.ScriptPath == "" && target.Execution == ExecutionRepoScript {
			target.ScriptPath = DefaultRepoScriptPath
		}
		if target.ContainerfilePath == "" {
			target.ContainerfilePath = "Containerfile"
		}
		out = append(out, target)
	}
	return out, nil
}

func (c Catalog) ResolveTargetForProfile(workProfile, name string) (ResolvedTarget, error) {
	profile, ok := c.WorkProfile(workProfile)
	if !ok {
		return ResolvedTarget{}, fmt.Errorf("devflows: unknown workspace profile %q", workProfile)
	}
	allowed := map[string]struct{}{}
	for _, repo := range profile.Repos {
		allowed[normalizeName(repo.Name)] = struct{}{}
	}
	lookup := normalizeName(name)
	for _, target := range c.BuildTargets {
		repoName := normalizeName(target.Repo)
		if _, ok := allowed[repoName]; !ok {
			continue
		}
		if normalizeName(target.Name) != lookup && !containsName(target.Aliases, lookup) {
			continue
		}
		repo, ok := c.Repo(repoName)
		if !ok {
			break
		}
		if target.ScriptPath == "" && target.Execution == ExecutionRepoScript {
			target.ScriptPath = DefaultRepoScriptPath
		}
		if target.ContainerfilePath == "" {
			target.ContainerfilePath = "Containerfile"
		}
		return ResolvedTarget{Target: target, Repo: repo}, nil
	}
	return ResolvedTarget{}, fmt.Errorf("devflows: unknown build target %q for workspace profile %q", name, workProfile)
}

func IsCommitSHA(ref string) bool {
	return commitShaRE.MatchString(strings.ToLower(strings.TrimSpace(ref)))
}

func BuilderGitNamespace() string { return managedBuilderGitNamespace }

func BuilderGitSecretName() string { return managedBuilderGitSecretName }

func BuilderGitKnownHostsSecretName() string { return managedBuilderGitKnownHostsSecret }

func normalizeName(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func validName(v string) bool { return nameRE.MatchString(v) }

func validRepoRelativePath(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "/") || strings.Contains(v, "\\") {
		return false
	}
	clean := path.Clean(v)
	for _, part := range strings.Split(v, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validMakeTarget(v string) bool {
	return makeTargetRE.MatchString(strings.TrimSpace(v))
}

func validBuilderImageDigest(v string) bool {
	return strings.Contains(strings.TrimSpace(v), "@sha256:")
}

func containsName(values []string, name string) bool {
	for _, v := range values {
		if normalizeName(v) == name {
			return true
		}
	}
	return false
}

func isSSHSource(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "git@") || strings.HasPrefix(raw, "ssh://")
}

func sourceHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("source url is required")
	}
	if strings.HasPrefix(raw, "git@") {
		rest := strings.TrimPrefix(raw, "git@")
		host, _, ok := strings.Cut(rest, ":")
		if !ok || host == "" {
			return "", fmt.Errorf("invalid ssh git url")
		}
		return strings.ToLower(host), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https", "ssh":
	default:
		return "", fmt.Errorf("unsupported source scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("source host is required")
	}
	return strings.ToLower(u.Hostname()), nil
}
