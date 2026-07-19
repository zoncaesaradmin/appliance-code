package buildergit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	DefaultNamespace  = "appliance-builds"
	DefaultSecretName = "builder-git-access"
	dataKeyHost       = "host"
	dataKeyUsername   = "username"
	dataKeyToken      = "token"
)

var (
	ErrNotConfigured = errors.New("buildergit: builder Git access is not configured")
	ErrHostMismatch  = errors.New("buildergit: configured Git access host does not match the requested repository host")
)

type SecretManager interface {
	Get(ctx context.Context, namespace, name string) (Secret, bool, error)
	Upsert(ctx context.Context, namespace, name string, secret Secret) error
}

type Secret struct {
	ResourceVersion string
	Data            map[string]string
}

type Status struct {
	Configured    bool
	Host          string
	Username      string
	RequiredHosts []string
}

type Credential struct {
	Host       string
	Username   string
	SecretName string
}

type Service struct {
	manager       SecretManager
	namespace     string
	secretName    string
	requiredHosts []string
}

func NewService(manager SecretManager, namespace, secretName string, requiredHosts []string) (*Service, error) {
	if manager == nil {
		return nil, errors.New("buildergit: secret manager is required")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = DefaultNamespace
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		secretName = DefaultSecretName
	}
	return &Service{
		manager:       manager,
		namespace:     namespace,
		secretName:    secretName,
		requiredHosts: normalizeHosts(requiredHosts),
	}, nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	secret, found, err := s.manager.Get(ctx, s.namespace, s.secretName)
	if err != nil {
		return Status{}, err
	}
	status := Status{RequiredHosts: append([]string(nil), s.requiredHosts...)}
	if !found {
		return status, nil
	}
	host := strings.TrimSpace(secret.Data[dataKeyHost])
	username := strings.TrimSpace(secret.Data[dataKeyUsername])
	token := strings.TrimSpace(secret.Data[dataKeyToken])
	if host == "" || username == "" || token == "" {
		return status, nil
	}
	status.Configured = true
	status.Host = host
	status.Username = username
	return status, nil
}

func (s *Service) Configure(ctx context.Context, host, username, token string) (Status, error) {
	host = normalizeHost(host)
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
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
	existing, _, err := s.manager.Get(ctx, s.namespace, s.secretName)
	if err != nil {
		return Status{}, err
	}
	existing.Data = map[string]string{
		dataKeyHost:     host,
		dataKeyUsername: username,
		dataKeyToken:    token,
	}
	if err := s.manager.Upsert(ctx, s.namespace, s.secretName, existing); err != nil {
		return Status{}, err
	}
	return Status{Configured: true, Host: host, Username: username, RequiredHosts: append([]string(nil), s.requiredHosts...)}, nil
}

func (s *Service) Resolve(ctx context.Context, repoURL string) (Credential, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Credential{}, err
	}
	if !status.Configured {
		return Credential{}, ErrNotConfigured
	}
	repoHost, err := RepoHost(repoURL)
	if err != nil {
		return Credential{}, err
	}
	if !strings.EqualFold(status.Host, repoHost) {
		return Credential{}, fmt.Errorf("%w: configured %q, requested %q", ErrHostMismatch, status.Host, repoHost)
	}
	return Credential{Host: status.Host, Username: status.Username, SecretName: s.secretName}, nil
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
	secret Secret
	found  bool
}

func NewMemorySecretManager() *MemorySecretManager {
	return &MemorySecretManager{}
}

func (m *MemorySecretManager) Get(_ context.Context, _, _ string) (Secret, bool, error) {
	if !m.found {
		return Secret{}, false, nil
	}
	return Secret{ResourceVersion: m.secret.ResourceVersion, Data: cloneMap(m.secret.Data)}, true, nil
}

func (m *MemorySecretManager) Upsert(_ context.Context, _, _ string, secret Secret) error {
	m.secret = Secret{ResourceVersion: "memory", Data: cloneMap(secret.Data)}
	m.found = true
	return nil
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
