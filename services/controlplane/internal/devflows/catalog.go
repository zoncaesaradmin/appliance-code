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

	DefaultRepoScriptPath            = "build.sh"
	managedSourceCredentialNamespace = "appliance-builds"
)

var (
	nameRE       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	ociRepoRE    = regexp.MustCompile(`^[a-z0-9]+([._/-][a-z0-9]+)*$`)
	commitShaRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	makeTargetRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
)

// Catalog is product configuration for developer workflows. It contains no
// private key or token material; source credentials are references to
// appliance/Kubernetes secrets materialized only into workflow pods.
type Catalog struct {
	WorkProfiles      []WorkProfile      `json:"workProfiles"`
	Repos             []Repo             `json:"repos"`
	SourceCredentials []SourceCredential `json:"sourceCredentials"`
	BuildTargets      []BuildTarget      `json:"buildTargets"`
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
	Name                string `json:"name"`
	URL                 string `json:"url"`
	DefaultRef          string `json:"defaultRef,omitempty"`
	SourceCredentialRef string `json:"sourceCredentialRef,omitempty"`
}

type SourceCredential struct {
	ID      string `json:"id"`
	GitHost string `json:"gitHost"`
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
	return len(c.WorkProfiles) == 0 && len(c.Repos) == 0 && len(c.SourceCredentials) == 0 && len(c.BuildTargets) == 0
}

func (c Catalog) Validate() error {
	var errs []string
	if len(c.BuildTargets) == 0 {
		errs = append(errs, "build catalog must declare at least one build target")
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

	creds := map[string]SourceCredential{}
	for _, cred := range c.SourceCredentials {
		id := normalizeName(cred.ID)
		if !validName(id) {
			errs = append(errs, fmt.Sprintf("source credential %q has an invalid id", cred.ID))
			continue
		}
		if cred.GitHost == "" {
			errs = append(errs, fmt.Sprintf("source credential %q must declare gitHost", id))
		}
		if _, exists := creds[id]; exists {
			errs = append(errs, fmt.Sprintf("source credential %q is duplicated", id))
		}
		cred.ID = id
		creds[id] = cred
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
		if isSSHSource(repo.URL) && repo.SourceCredentialRef == "" {
			errs = append(errs, fmt.Sprintf("repo %q uses SSH and must declare sourceCredentialRef", name))
		}
		if repo.SourceCredentialRef != "" {
			cred, ok := creds[normalizeName(repo.SourceCredentialRef)]
			if !ok {
				errs = append(errs, fmt.Sprintf("repo %q references unknown source credential %q", name, repo.SourceCredentialRef))
			} else if host != "" && !strings.EqualFold(host, cred.GitHost) {
				errs = append(errs, fmt.Sprintf("repo %q host %q does not match source credential %q host %q", name, host, cred.ID, cred.GitHost))
			}
		}
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
			// ScriptPath defaults at resolve time.
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

func (c Catalog) SourceCredential(id string) (SourceCredential, bool) {
	id = normalizeName(id)
	for _, cred := range c.SourceCredentials {
		if normalizeName(cred.ID) == id {
			cred.ID = id
			return cred, true
		}
	}
	return SourceCredential{}, false
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

func IsCommitSHA(ref string) bool {
	return commitShaRE.MatchString(strings.ToLower(strings.TrimSpace(ref)))
}

func SourceCredentialNamespace() string { return managedSourceCredentialNamespace }

func SourceCredentialSecretName(id string) string {
	return "builder-git-" + sourceCredentialIDName(id) + "-key"
}

func SourceCredentialKnownHostsSecretName(id string) string {
	return "builder-git-" + sourceCredentialIDName(id) + "-known-hosts"
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
	return strings.Contains(strings.TrimSpace(v), "@sha256:")
}

func sourceCredentialIDName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "source"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "source"
	}
	return name
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
