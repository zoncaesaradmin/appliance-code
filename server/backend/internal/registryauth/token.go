package registryauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// TokenLifetime is the fixed five-minute maximum registry access-token
// lifetime the plan requires.
const TokenLifetime = 5 * time.Minute

// AccessEntry is one granted repository scope, matching the OCI
// Distribution/Docker registry token response's "access" claim shape.
type AccessEntry struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

type registryClaims struct {
	Issuer    string        `json:"iss"`
	Subject   string        `json:"sub"`
	Audience  string        `json:"aud"`
	IssuedAt  int64         `json:"iat"`
	NotBefore int64         `json:"nbf"`
	ExpiresAt int64         `json:"exp"`
	JTI       string        `json:"jti"`
	Access    []AccessEntry `json:"access"`
}

type registryHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

// IssueToken signs a short-lived registry access token granting access,
// scoped to subject (the authenticated user ID) and jti (a fresh, unique
// token identifier). zot verifies the signature using the corresponding
// public key; this package never validates its own output back, since
// verification is zot's responsibility, not the control plane's.
func IssueToken(priv ed25519.PrivateKey, kid, issuer, subject, audience, jti string, access []AccessEntry) (token string, expiresAt time.Time, err error) {
	now := time.Now().UTC()
	expiresAt = now.Add(TokenLifetime)

	header := registryHeader{Alg: "EdDSA", Typ: "JWT", Kid: kid}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("registryauth: encoding token header: %w", err)
	}

	claims := registryClaims{
		Issuer: issuer, Subject: subject, Audience: audience,
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: expiresAt.Unix(),
		JTI: jti, Access: access,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("registryauth: encoding token claims: %w", err)
	}

	signingInput := b64encode(headerJSON) + "." + b64encode(claimsJSON)
	sig := ed25519.Sign(priv, []byte(signingInput))
	return signingInput + "." + b64encode(sig), expiresAt, nil
}

func b64encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
