package buildergit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultNamespace = "appliance-builds"
	SecretNamePrefix = "git-access-"

	LabelGitAccess     = "appliance.zon/git-access"
	LabelGitAccessName = "appliance.zon/git-access-name"
	LabelGitAccessHost = "appliance.zon/git-access-host"

	dataKeyHost     = "host"
	dataKeyUsername = "username"
	dataKeyToken    = "token"
)

var (
	ErrNotConfigured = errors.New("buildergit: builder Git access is not configured")
	ErrHostMismatch  = errors.New("buildergit: configured Git access host does not match the requested repository host")
	ErrNotFound      = errors.New("buildergit: credential not found")
	ErrHostConflict  = errors.New("buildergit: another credential already covers this Git host")

	credentialNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type SecretManager interface {
	Get(ctx context.Context, namespace, name string) (Secret, bool, error)
	Upsert(ctx context.Context, namespace, name string, secret Secret) error
	Delete(ctx context.Context, namespace, name string) error
	List(ctx context.Context, namespace, labelSelector string) ([]Secret, error)
}

type Secret struct {
	Name            string
	ResourceVersion string
	Labels          map[string]string
	Data            map[string]string
}

type CredentialInfo struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Username   string `json:"username"`
	SecretName string `json:"-"`
}

type Status struct {
	Configured    bool
	RequiredHosts []string
	CoveredHosts  []string
	MissingHosts  []string
	Credentials   []CredentialInfo
}

type Credential struct {
	Name       string
	Host       string
	Username   string
	SecretName string
}

type Service struct {
	manager       SecretManager
	namespace     string
	requiredHosts []string
}

func NewService(manager SecretManager, namespace string, requiredHosts []string) (*Service, error) {
	if manager == nil {
		return nil, errors.New("buildergit: secret manager is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = DefaultNamespace
	}
	return &Service{
		manager:       manager,
		namespace:     namespace,
		requiredHosts: normalizeHosts(requiredHosts),
	}, nil
}

func (s *Service) SetRequiredHosts(hosts []string) {
	s.requiredHosts = normalizeHosts(hosts)
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	credentials, err := s.listCredentials(ctx)
	if err != nil {
		return Status{}, err
	}
	return s.statusFromCredentials(credentials), nil
}

func (s *Service) List(ctx context.Context) (Status, error) {
	return s.Status(ctx)
}

func (s *Service) Upsert(ctx context.Context, name, host, username, token string) (Status, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	host = normalizeHost(host)
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
	if err := ValidateCredentialName(name); err != nil {
		return Status{}, err
	}
	if host == "" {
		return Status{}, fmt.Errorf("buildergit: host is required")
	}
	if username == "" {
		return Status{}, fmt.Errorf("buildergit: username is required")
	}
	if token == "" {
		return Status{}, fmt.Errorf("buildergit: token is required")
	}
	if len(s.requiredHosts) > 0 {
		ok := false
		for _, required := range s.requiredHosts {
			if strings.EqualFold(required, host) {
				ok = true
				break
			}
		}
		if !ok {
			return Status{}, fmt.Errorf("buildergit: host %q is not used by the configured build catalog", host)
		}
	}

	existing, err := s.listCredentials(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, cred := range existing {
		if strings.EqualFold(cred.Host, host) && cred.Name != name {
			return Status{}, fmt.Errorf("%w: host %q is owned by credential %q", ErrHostConflict, host, cred.Name)
		}
	}

	secretName := SecretNameFor(name)
	current, found, err := s.manager.Get(ctx, s.namespace, secretName)
	if err != nil {
		return Status{}, err
	}
	if !found {
		current = Secret{}
	}
	current.Name = secretName
	current.Labels = map[string]string{
		LabelGitAccess:     "true",
		LabelGitAccessName: name,
		LabelGitAccessHost: host,
	}
	current.Data = map[string]string{
		dataKeyHost:     host,
		dataKeyUsername: username,
		dataKeyToken:    token,
	}
	if err := s.manager.Upsert(ctx, s.namespace, secretName, current); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

func (s *Service) Delete(ctx context.Context, name string) (Status, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if err := ValidateCredentialName(name); err != nil {
		return Status{}, err
	}
	secretName := SecretNameFor(name)
	_, found, err := s.manager.Get(ctx, s.namespace, secretName)
	if err != nil {
		return Status{}, err
	}
	if !found {
		return Status{}, ErrNotFound
	}
	if err := s.manager.Delete(ctx, s.namespace, secretName); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

func (s *Service) Resolve(ctx context.Context, repoURL string) (Credential, error) {
	repoHost, err := RepoHost(repoURL)
	if err != nil {
		return Credential{}, err
	}
	credentials, err := s.listCredentials(ctx)
	if err != nil {
		return Credential{}, err
	}
	if len(credentials) == 0 {
		return Credential{}, ErrNotConfigured
	}
	for _, cred := range credentials {
		if strings.EqualFold(cred.Host, repoHost) {
			return Credential{
				Name:       cred.Name,
				Host:       cred.Host,
				Username:   cred.Username,
				SecretName: cred.SecretName,
			}, nil
		}
	}
	return Credential{}, fmt.Errorf("%w: no credential covers host %q", ErrHostMismatch, repoHost)
}

func (s *Service) ResolveMany(ctx context.Context, repoURLs []string) ([]Credential, error) {
	seen := map[string]Credential{}
	order := make([]string, 0)
	for _, repoURL := range repoURLs {
		cred, err := s.Resolve(ctx, repoURL)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[cred.SecretName]; ok {
			continue
		}
		seen[cred.SecretName] = cred
		order = append(order, cred.SecretName)
	}
	out := make([]Credential, 0, len(order))
	for _, secretName := range order {
		out = append(out, seen[secretName])
	}
	return out, nil
}

func (s *Service) listCredentials(ctx context.Context) ([]CredentialInfo, error) {
	listed, err := s.manager.List(ctx, s.namespace, LabelGitAccess+"=true")
	if err != nil {
		return nil, err
	}
	byName := map[string]CredentialInfo{}
	for _, secret := range listed {
		info, ok := credentialFromSecret(secret)
		if !ok {
			continue
		}
		byName[info.Name] = info
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]CredentialInfo, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func (s *Service) statusFromCredentials(credentials []CredentialInfo) Status {
	covered := map[string]struct{}{}
	for _, cred := range credentials {
		covered[cred.Host] = struct{}{}
	}
	coveredHosts := make([]string, 0, len(covered))
	for host := range covered {
		coveredHosts = append(coveredHosts, host)
	}
	sort.Strings(coveredHosts)

	missing := make([]string, 0)
	for _, host := range s.requiredHosts {
		if _, ok := covered[host]; !ok {
			missing = append(missing, host)
		}
	}
	configured := len(s.requiredHosts) > 0 && len(missing) == 0
	if len(s.requiredHosts) == 0 {
		configured = len(credentials) > 0
	}
	return Status{
		Configured:    configured,
		RequiredHosts: append([]string(nil), s.requiredHosts...),
		CoveredHosts:  coveredHosts,
		MissingHosts:  missing,
		Credentials:   credentials,
	}
}

func credentialFromSecret(secret Secret) (CredentialInfo, bool) {
	host := strings.TrimSpace(secret.Data[dataKeyHost])
	username := strings.TrimSpace(secret.Data[dataKeyUsername])
	token := strings.TrimSpace(secret.Data[dataKeyToken])
	if host == "" || username == "" || token == "" {
		return CredentialInfo{}, false
	}
	name := strings.TrimSpace(secret.Labels[LabelGitAccessName])
	if name == "" {
		name = nameFromSecretName(secret.Name)
	}
	if name == "" {
		return CredentialInfo{}, false
	}
	secretName := strings.TrimSpace(secret.Name)
	if secretName == "" {
		secretName = SecretNameFor(name)
	}
	return CredentialInfo{
		Name:       name,
		Host:       normalizeHost(host),
		Username:   username,
		SecretName: secretName,
	}, true
}

func nameFromSecretName(secretName string) string {
	secretName = strings.TrimSpace(secretName)
	if strings.HasPrefix(secretName, SecretNamePrefix) {
		return strings.TrimPrefix(secretName, SecretNamePrefix)
	}
	return ""
}

func SecretNameFor(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	return SecretNamePrefix + name
}

func ValidateCredentialName(name string) error {
	name = strings.TrimSpace(strings.ToLower(name))
	if !credentialNameRE.MatchString(name) {
		return fmt.Errorf("buildergit: credential name %q is invalid", name)
	}
	return nil
}

func RepoHost(repoURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil {
		return "", fmt.Errorf("buildergit: parse repo URL: %w", err)
	}
	host := normalizeHost(parsed.Hostname())
	if parsed.Scheme == "" || host == "" {
		return "", fmt.Errorf("buildergit: repo URL must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("buildergit: repo URL must use HTTPS")
	}
	return host, nil
}

type MemorySecretManager struct {
	secrets map[string]Secret
}

func NewMemorySecretManager() *MemorySecretManager {
	return &MemorySecretManager{secrets: map[string]Secret{}}
}

func (m *MemorySecretManager) Get(_ context.Context, _, name string) (Secret, bool, error) {
	secret, ok := m.secrets[name]
	if !ok {
		return Secret{}, false, nil
	}
	return cloneSecret(secret), true, nil
}

func (m *MemorySecretManager) Upsert(_ context.Context, _, name string, secret Secret) error {
	secret.Name = name
	secret.ResourceVersion = "memory"
	m.secrets[name] = cloneSecret(secret)
	return nil
}

func (m *MemorySecretManager) Delete(_ context.Context, _, name string) error {
	delete(m.secrets, name)
	return nil
}

func (m *MemorySecretManager) List(_ context.Context, _, labelSelector string) ([]Secret, error) {
	key, value, ok := parseLabelSelector(labelSelector)
	out := make([]Secret, 0, len(m.secrets))
	for _, secret := range m.secrets {
		if ok {
			if secret.Labels[key] != value {
				continue
			}
		}
		out = append(out, cloneSecret(secret))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseLabelSelector(selector string) (string, string, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", "", false
	}
	parts := strings.SplitN(selector, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func normalizeHosts(hosts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	return host
}

func cloneSecret(in Secret) Secret {
	return Secret{
		Name:            in.Name,
		ResourceVersion: in.ResourceVersion,
		Labels:          cloneMap(in.Labels),
		Data:            cloneMap(in.Data),
	}
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func EncodeSecretValue(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func decodeSecretValue(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
