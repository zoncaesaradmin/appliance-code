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
	ExecutionMake   = "make"
	ExecutionScript = "script"

	DefaultScriptArg = "build.sh"

	legacyExecutionMake   = "make_target"
	legacyExecutionScript = "repo_script"
)

var (
	nameRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	ociRepoRE     = regexp.MustCompile(`^[a-z0-9]+([._/-][a-z0-9]+)*$`)
	commitShaRE   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	makeTargetRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	imageDigestRE = regexp.MustCompile(`^.+@sha256:[0-9a-f]{64}$`)
)

// Catalog is product configuration for developer workflows.
type Catalog struct {
	WorkProfiles []WorkProfile `json:"workProfiles"`
	Repos        []Repo        `json:"repos"`
	BuildTargets []BuildTarget `json:"buildTargets"`
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
	Name         string        `json:"name"`
	URL          string        `json:"url"`
	DefaultRef   string        `json:"defaultRef,omitempty"`
	BuildTargets []BuildTarget `json:"buildTargets,omitempty"`
}

type BuildTarget struct {
	Name               string   `json:"name"`
	Aliases            []string `json:"aliases,omitempty"`
	Description        string   `json:"description,omitempty"`
	Repo               string   `json:"repo,omitempty"`
	Execution          string   `json:"execution"`
	Args               []string `json:"args,omitempty"`
	ContainerfilePath  string   `json:"containerfilePath,omitempty"`
	ImageRepository    string   `json:"imageRepository"`
	ImageTagTemplate   string   `json:"imageTagTemplate,omitempty"`
	BuilderImageDigest string   `json:"builderImageDigest"`

	// Legacy input-only fields. Accepted on unmarshal for older catalogs and
	// cleared by Normalize into Args.
	ScriptPath string `json:"scriptPath,omitempty"`
	MakeTarget string `json:"makeTarget,omitempty"`
}

type ResolvedTarget struct {
	Target BuildTarget
	Repo   Repo
}

func (c Catalog) Empty() bool {
	if len(c.WorkProfiles) > 0 || len(c.BuildTargets) > 0 {
		return false
	}
	for _, repo := range c.Repos {
		if len(repo.BuildTargets) > 0 {
			return false
		}
	}
	return len(c.Repos) == 0
}

// Normalize lifts repos[].buildTargets into the top-level buildTargets list
// and fills each target's repo from its parent when omitted. Both nested and
// top-level authoring forms are accepted; runtime code always uses the flat list.
// It also normalizes execution to make|script and folds legacy makeTarget /
// scriptPath fields into args.
func (c *Catalog) Normalize() {
	if c == nil {
		return
	}
	var lifted []BuildTarget
	for i, repo := range c.Repos {
		repoName := normalizeName(repo.Name)
		for _, target := range repo.BuildTargets {
			if strings.TrimSpace(target.Repo) == "" {
				target.Repo = repoName
			}
			lifted = append(lifted, target)
		}
		c.Repos[i].BuildTargets = nil
	}
	if len(lifted) > 0 {
		c.BuildTargets = append(c.BuildTargets, lifted...)
	}
	for i := range c.BuildTargets {
		normalizeTargetExecution(&c.BuildTargets[i])
	}
}

func normalizeTargetExecution(t *BuildTarget) {
	if t == nil {
		return
	}
	execution := strings.TrimSpace(t.Execution)
	args := compactArgs(t.Args)
	switch execution {
	case legacyExecutionMake, ExecutionMake:
		t.Execution = ExecutionMake
		if len(args) == 0 {
			if v := strings.TrimSpace(t.MakeTarget); v != "" {
				args = []string{v}
			}
		}
	case legacyExecutionScript, ExecutionScript:
		t.Execution = ExecutionScript
		if len(args) == 0 {
			if v := strings.TrimSpace(t.ScriptPath); v != "" {
				args = []string{v}
			} else {
				args = []string{DefaultScriptArg}
			}
		}
	default:
		// Leave unknown execution for Validate to reject.
		t.Execution = execution
	}
	t.Args = args
	t.MakeTarget = ""
	t.ScriptPath = ""
}

func compactArgs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func (t BuildTarget) PrimaryArg() string {
	if len(t.Args) == 0 {
		return ""
	}
	return t.Args[0]
}

func (c Catalog) Validate() error {
	normalized := c.copyForNormalize()
	normalized.Normalize()
	return normalized.validateNormalized()
}

func (c Catalog) copyForNormalize() Catalog {
	out := c
	if c.WorkProfiles != nil {
		out.WorkProfiles = append([]WorkProfile(nil), c.WorkProfiles...)
		for i := range out.WorkProfiles {
			if c.WorkProfiles[i].Repos != nil {
				out.WorkProfiles[i].Repos = append([]ProfileRepo(nil), c.WorkProfiles[i].Repos...)
			}
		}
	}
	if c.Repos != nil {
		out.Repos = make([]Repo, len(c.Repos))
		for i := range c.Repos {
			out.Repos[i] = c.Repos[i]
			if c.Repos[i].BuildTargets != nil {
				out.Repos[i].BuildTargets = append([]BuildTarget(nil), c.Repos[i].BuildTargets...)
			}
		}
	}
	if c.BuildTargets != nil {
		out.BuildTargets = append([]BuildTarget(nil), c.BuildTargets...)
	}
	return out
}

func (c Catalog) validateNormalized() error {
	var errs []string
	if len(c.WorkProfiles) == 0 {
		errs = append(errs, "build catalog must declare at least one workspace profile")
	}
	if len(c.Repos) == 0 {
		errs = append(errs, "build catalog must declare at least one repo")
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
		case ExecutionScript:
			if len(target.Args) != 1 {
				errs = append(errs, fmt.Sprintf("build target %q script execution requires exactly one args entry", name))
			} else if !validRepoRelativePath(target.Args[0]) {
				errs = append(errs, fmt.Sprintf("build target %q has invalid script args entry %q", name, target.Args[0]))
			}
		case ExecutionMake:
			if len(target.Args) != 1 {
				errs = append(errs, fmt.Sprintf("build target %q make execution requires exactly one args entry", name))
			} else if !validMakeTarget(target.Args[0]) {
				errs = append(errs, fmt.Sprintf("build target %q has invalid make args entry %q", name, target.Args[0]))
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
				normalizeTargetExecution(&target)
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
		normalizeTargetExecution(&target)
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
		normalizeTargetExecution(&target)
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
		normalizeTargetExecution(&target)
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
	v = strings.TrimSpace(v)
	if !imageDigestRE.MatchString(v) {
		return false
	}
	_, digest, _ := strings.Cut(v, "@sha256:")
	return digest != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}

func containsName(values []string, name string) bool {
	for _, v := range values {
		if normalizeName(v) == name {
			return true
		}
	}
	return false
}

func sourceHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("source url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("unsupported source scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("source host is required")
	}
	return strings.ToLower(u.Hostname()), nil
}
