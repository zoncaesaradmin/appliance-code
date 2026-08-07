package builds

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/buildergit"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/tokens"
	"appliance-code/services/controlplane/internal/workflows"
)

const (
	registrySecretPrefix = "build-registry-"
	registryDataUsername = "username"
	registryDataToken    = "token"
	// Appliance LAN registries use appliance-issued TLS that is not in the
	// builder image trust store; buildah/make need tls-verify=false for push.
	defaultRegistryTLSVerify = "false"
	pushTokenTTLBuffer       = 15 * time.Minute
)

// TokenIssuer mints and revokes build-scoped API tokens used as registry
// login passwords (OCI token realm basic-auth).
type TokenIssuer interface {
	Create(ctx context.Context, actor audit.Actor, ownerUserID, name string, ttl time.Duration, scopes []string) (raw string, token storage.APIToken, err error)
	Revoke(ctx context.Context, actor audit.Actor, id string) error
}

// UserLookup resolves the build owner's username for registry login.
type UserLookup interface {
	Get(ctx context.Context, id string) (storage.User, error)
}

// RegistryPushConfig wires build-pod registry push credentials.
type RegistryPushConfig struct {
	Host      string
	TLSVerify string
	Namespace string
	Secrets   buildergit.SecretManager
	Tokens    TokenIssuer
	Users     UserLookup
}

// ConfigureRegistryPush enables minting a build-scoped API token and
// Kubernetes Secret so workflow pods can push to the appliance artifact
// server via DEV_REGISTRY* / SERVICE_IMAGE_*.
func (s *Service) ConfigureRegistryPush(cfg RegistryPushConfig) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("builds: registry push host is required")
	}
	if cfg.Secrets == nil {
		return fmt.Errorf("builds: registry push secret manager is required")
	}
	if cfg.Tokens == nil {
		return fmt.Errorf("builds: registry push token issuer is required")
	}
	if cfg.Users == nil {
		return fmt.Errorf("builds: registry push user lookup is required")
	}
	namespace := strings.TrimSpace(cfg.Namespace)
	if namespace == "" {
		namespace = buildergit.DefaultNamespace
	}
	tlsVerify := strings.TrimSpace(cfg.TLSVerify)
	if tlsVerify == "" {
		tlsVerify = defaultRegistryTLSVerify
	}
	s.registryPush = &RegistryPushConfig{
		Host:      host,
		TLSVerify: tlsVerify,
		Namespace: namespace,
		Secrets:   cfg.Secrets,
		Tokens:    cfg.Tokens,
		Users:     cfg.Users,
	}
	return nil
}

// RegistryHostFromOrigin extracts the host[:port] from a canonical origin URL.
func RegistryHostFromOrigin(origin string) (string, error) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", fmt.Errorf("builds: canonical origin is empty")
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("builds: canonical origin %q is not an absolute URL with a host", origin)
	}
	return u.Host, nil
}

func (s *Service) provisionRegistryPush(ctx context.Context, actor audit.Actor, build *storage.Build) error {
	if s.registryPush == nil {
		return nil
	}
	cfg := s.registryPush
	user, err := cfg.Users.Get(ctx, build.OwnerID)
	if err != nil {
		return fmt.Errorf("builds: looking up owner for registry push: %w", err)
	}
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return fmt.Errorf("builds: owner username is empty")
	}

	ttl := time.Until(build.DeadlineAt) + pushTokenTTLBuffer
	if ttl < tokens.MinLifetime {
		ttl = tokens.MinLifetime
	}
	if ttl > tokens.MaxLifetime {
		ttl = tokens.MaxLifetime
	}

	raw, token, err := cfg.Tokens.Create(ctx, actor, build.OwnerID, "bld-"+build.ID, ttl, []string{
		roles.PermArtifactsRead,
		roles.PermArtifactsWrite,
	})
	if err != nil {
		return fmt.Errorf("builds: creating registry push token: %w", err)
	}
	build.PushTokenID = token.ID
	s.SetSensitiveLogValues(append(s.sensitiveLogValues, raw)...)

	secretName := registrySecretPrefix + build.ID
	err = cfg.Secrets.Upsert(ctx, cfg.Namespace, secretName, buildergit.Secret{
		Name: secretName,
		Labels: map[string]string{
			"app.kubernetes.io/part-of":        "appliance",
			"appliance.zon/build-id":           build.ID,
			"appliance.zon/registry-push":      "true",
			"appliance.zon/registry-push-host": cfg.Host,
		},
		Data: map[string]string{
			registryDataUsername: username,
			registryDataToken:    raw,
		},
	})
	if err != nil {
		_ = cfg.Tokens.Revoke(ctx, actor, token.ID)
		return fmt.Errorf("builds: storing registry push secret: %w", err)
	}
	build.RegistrySecretName = secretName
	return nil
}

func (s *Service) applyRegistryPushSpec(spec *workflows.Spec, build storage.Build) {
	if s.registryPush == nil || strings.TrimSpace(build.RegistrySecretName) == "" {
		return
	}
	spec.RegistryHost = s.registryPush.Host
	spec.RegistryTLSVerify = s.registryPush.TLSVerify
	spec.RegistryCredentialSecret = build.RegistrySecretName
}

func (s *Service) cleanupRegistryPush(ctx context.Context, build storage.Build) {
	if s.registryPush == nil {
		return
	}
	cfg := s.registryPush
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "builds.cleanup"}
	if id := strings.TrimSpace(build.PushTokenID); id != "" {
		_ = cfg.Tokens.Revoke(ctx, actor, id)
	}
	if name := strings.TrimSpace(build.RegistrySecretName); name != "" {
		_ = cfg.Secrets.Delete(ctx, cfg.Namespace, name)
	}
}
