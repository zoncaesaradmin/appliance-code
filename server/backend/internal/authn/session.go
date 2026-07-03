package authn

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionAccessLifetime is the fixed ADR 0010 session access-token
// lifetime.
const SessionAccessLifetime = 15 * time.Minute

// SessionClockSkew is the maximum clock skew ADR 0010 tolerates when
// validating session token time claims.
const SessionClockSkew = 60 * time.Second

// SessionClaims are the JWT claims carried by a short-lived interactive
// session access token. It is deliberately hand-rolled rather than built on
// an external JWT library: the claim set is small and fixed, and avoiding a
// general-purpose JWT dependency shrinks this security-critical package's
// supply-chain surface. The wire format is still a standard compact JWT
// using EdDSA (RFC 8037), so it remains interoperable with standard
// verifiers if ever needed.
type SessionClaims struct {
	Issuer            string    `json:"iss"`
	Audience          string    `json:"aud"`
	Subject           string    `json:"sub"`
	JTI               string    `json:"jti"`
	FamilyID          string    `json:"familyId"`
	CredentialVersion int       `json:"credentialVersion"`
	IssuedAt          time.Time `json:"-"`
	NotBefore         time.Time `json:"-"`
	ExpiresAt         time.Time `json:"-"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

type jwtPayload struct {
	Issuer            string `json:"iss"`
	Audience          string `json:"aud"`
	Subject           string `json:"sub"`
	JTI               string `json:"jti"`
	FamilyID          string `json:"familyId"`
	CredentialVersion int    `json:"credentialVersion"`
	IssuedAt          int64  `json:"iat"`
	NotBefore         int64  `json:"nbf"`
	ExpiresAt         int64  `json:"exp"`
}

func b64encode(b []byte) string          { return base64.RawURLEncoding.EncodeToString(b) }
func b64decode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// IssueSessionJWT signs claims as a compact EdDSA JWT using priv, tagging
// the header with kid so key rotation can select the correct public key at
// validation time.
func IssueSessionJWT(priv ed25519.PrivateKey, kid string, claims SessionClaims) (string, error) {
	header := jwtHeader{Alg: "EdDSA", Typ: "JWT", Kid: kid}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("authn: encoding jwt header: %w", err)
	}

	payload := jwtPayload{
		Issuer: claims.Issuer, Audience: claims.Audience, Subject: claims.Subject,
		JTI: claims.JTI, FamilyID: claims.FamilyID, CredentialVersion: claims.CredentialVersion,
		IssuedAt: claims.IssuedAt.Unix(), NotBefore: claims.NotBefore.Unix(), ExpiresAt: claims.ExpiresAt.Unix(),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("authn: encoding jwt payload: %w", err)
	}

	signingInput := b64encode(headerJSON) + "." + b64encode(payloadJSON)
	sig := ed25519.Sign(priv, []byte(signingInput))
	return signingInput + "." + b64encode(sig), nil
}

// ErrInvalidSessionToken is returned for any structurally invalid, badly
// signed, or claim-invalid session token, deliberately without
// distinguishing the exact cause to callers outside this package.
var ErrInvalidSessionToken = errors.New("authn: invalid session token")

// ValidateSessionJWT verifies token's signature against pub (selected by
// the caller using the token's kid header) and checks issuer, audience, and
// time claims within SessionClockSkew.
func ValidateSessionJWT(pub ed25519.PublicKey, expectedKid, issuer, audience, token string) (SessionClaims, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return SessionClaims{}, "", ErrInvalidSessionToken
	}

	headerJSON, err := b64decode(parts[0])
	if err != nil {
		return SessionClaims{}, "", ErrInvalidSessionToken
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return SessionClaims{}, "", ErrInvalidSessionToken
	}
	if header.Alg != "EdDSA" {
		return SessionClaims{}, "", ErrInvalidSessionToken
	}
	if expectedKid != "" && header.Kid != expectedKid {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}

	sig, err := b64decode(parts[2])
	if err != nil {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}
	signingInput := parts[0] + "." + parts[1]
	if !ed25519.Verify(pub, []byte(signingInput), sig) {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}

	payloadJSON, err := b64decode(parts[1])
	if err != nil {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}
	var payload jwtPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}

	if payload.Issuer != issuer || payload.Audience != audience {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}

	now := time.Now()
	notBefore := time.Unix(payload.NotBefore, 0)
	expiresAt := time.Unix(payload.ExpiresAt, 0)
	if now.Add(SessionClockSkew).Before(notBefore) {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}
	if now.Add(-SessionClockSkew).After(expiresAt) {
		return SessionClaims{}, header.Kid, ErrInvalidSessionToken
	}

	claims := SessionClaims{
		Issuer: payload.Issuer, Audience: payload.Audience, Subject: payload.Subject,
		JTI: payload.JTI, FamilyID: payload.FamilyID, CredentialVersion: payload.CredentialVersion,
		IssuedAt: time.Unix(payload.IssuedAt, 0), NotBefore: notBefore, ExpiresAt: expiresAt,
	}
	return claims, header.Kid, nil
}
