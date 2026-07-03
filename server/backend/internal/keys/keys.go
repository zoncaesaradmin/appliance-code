// Package keys loads the purpose-separated secrets ADR 0005 requires:
// independent key material for session signing, API-token digesting, and
// refresh/reset-credential digesting, with no reuse across purposes.
//
// In the released appliance these files are generated and lifecycle-managed
// by the installer and mounted from Kubernetes Secrets. This package's
// LoadOrGenerate additionally creates them on first use so local
// development and direct-host testing work without an installer; production
// deployment should mount pre-generated files at the same paths instead of
// relying on first-run generation.
package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	sessionPrivateFile  = "session_ed25519_private.key"
	registryPrivateFile = "registry_ed25519_private.key"
	apiTokenPepperFile  = "api_token_pepper.key"
	refreshPepperFile   = "refresh_pepper.key"

	pepperLength = 32
)

// Material holds every purpose-separated secret the authn and registryauth
// packages need. The registry signing key rotates independently from the
// session signing key and never signs anything but registry access tokens,
// per ADR 0005.
type Material struct {
	SessionPrivateKey ed25519.PrivateKey
	SessionPublicKey  ed25519.PublicKey
	SessionKeyID      string

	RegistryPrivateKey ed25519.PrivateKey
	RegistryPublicKey  ed25519.PublicKey
	RegistryKeyID      string

	APITokenPepper []byte
	RefreshPepper  []byte
}

// LoadOrGenerate reads key material from dir, generating and persisting any
// missing files with restrictive permissions. dir is created if needed.
func LoadOrGenerate(dir string) (*Material, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("keys: creating key directory: %w", err)
	}

	priv, err := loadOrGenerateEd25519(filepath.Join(dir, sessionPrivateFile))
	if err != nil {
		return nil, err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("keys: unexpected public key type for session signing key")
	}

	registryPriv, err := loadOrGenerateEd25519(filepath.Join(dir, registryPrivateFile))
	if err != nil {
		return nil, err
	}
	registryPub, ok := registryPriv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("keys: unexpected public key type for registry signing key")
	}

	apiTokenPepper, err := loadOrGenerateBytes(filepath.Join(dir, apiTokenPepperFile), pepperLength)
	if err != nil {
		return nil, err
	}
	refreshPepper, err := loadOrGenerateBytes(filepath.Join(dir, refreshPepperFile), pepperLength)
	if err != nil {
		return nil, err
	}

	return &Material{
		SessionPrivateKey:  priv,
		SessionPublicKey:   pub,
		SessionKeyID:       keyID(pub),
		RegistryPrivateKey: registryPriv,
		RegistryPublicKey:  registryPub,
		RegistryKeyID:      keyID(registryPub),
		APITokenPepper:     apiTokenPepper,
		RefreshPepper:      refreshPepper,
	}, nil
}

// keyID derives a short, stable, non-secret key identifier from a public
// key, used as the JWT "kid" header.
func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

func loadOrGenerateEd25519(path string) (ed25519.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		seed, decodeErr := base64.StdEncoding.DecodeString(string(data))
		if decodeErr != nil {
			return nil, fmt.Errorf("keys: decoding %s: %w", path, decodeErr)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("keys: %s has unexpected seed length %d", path, len(seed))
		}
		return ed25519.NewKeyFromSeed(seed), nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("keys: reading %s: %w", path, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keys: generating session signing key: %w", err)
	}
	seed := priv.Seed()
	if err := writeKeyFile(path, []byte(base64.StdEncoding.EncodeToString(seed))); err != nil {
		return nil, err
	}
	return priv, nil
}

func loadOrGenerateBytes(path string, length int) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		decoded, decodeErr := base64.StdEncoding.DecodeString(string(data))
		if decodeErr != nil {
			return nil, fmt.Errorf("keys: decoding %s: %w", path, decodeErr)
		}
		if len(decoded) != length {
			return nil, fmt.Errorf("keys: %s has unexpected length %d", path, len(decoded))
		}
		return decoded, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("keys: reading %s: %w", path, err)
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("keys: generating random material for %s: %w", filepath.Base(path), err)
	}
	if err := writeKeyFile(path, []byte(base64.StdEncoding.EncodeToString(buf))); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeKeyFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("keys: writing %s: %w", path, err)
	}
	return nil
}
