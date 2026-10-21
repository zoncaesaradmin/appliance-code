package configuration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// ArchiveFormat is the portable encrypted configuration archive format. The
// envelope is intentionally independent from an object-store provider, so the
// exact same bytes can be saved locally for air-gapped transfer or uploaded to
// a configured backup target.
const ArchiveFormat = "configuration-archive.zon/v1"

type ArchiveManifest struct {
	Format        string    `json:"format"`
	ArchiveID     string    `json:"archiveId"`
	CreatedAt     time.Time `json:"createdAt"`
	RevisionID    string    `json:"revisionId"`
	SchemaVersion string    `json:"schemaVersion"`
	PayloadDigest string    `json:"payloadDigest"`
	KeyID         string    `json:"keyId"`
}

type encryptedArchive struct {
	Manifest   ArchiveManifest `json:"manifest"`
	Nonce      []byte          `json:"nonce"`
	Ciphertext []byte          `json:"ciphertext"`
}

// Seal encrypts a configuration revision with AES-256-GCM. key must come
// from a purpose-separated recovery/export key; callers must not reuse
// session, token, or cursor keys. The manifest is authenticated as AEAD
// additional data, so its revision and compatibility identity cannot be
// altered independently from the payload.
func Seal(revision storage.ConfigurationRevision, key []byte, keyID string) ([]byte, ArchiveManifest, error) {
	if len(key) != 32 {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: archive key must be 32 bytes")
	}
	if revision.ID == "" || revision.SchemaVersion == "" || len(revision.Payload) == 0 || revision.PayloadDigest == "" {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: revision identity, schema, payload, and digest are required")
	}
	sum := sha256.Sum256(revision.Payload)
	if want := "sha256:" + hex.EncodeToString(sum[:]); revision.PayloadDigest != want {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: revision payload digest does not match payload")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: create archive cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: create archive AEAD: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: generate archive nonce: %w", err)
	}
	manifest := ArchiveManifest{
		Format: ArchiveFormat, ArchiveID: uuidLike(), CreatedAt: time.Now().UTC(),
		RevisionID: revision.ID, SchemaVersion: revision.SchemaVersion,
		PayloadDigest: revision.PayloadDigest, KeyID: keyID,
	}
	additionalData, err := json.Marshal(manifest)
	if err != nil {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: encode archive manifest: %w", err)
	}
	envelope := encryptedArchive{Manifest: manifest, Nonce: nonce, Ciphertext: aead.Seal(nil, nonce, revision.Payload, additionalData)}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, ArchiveManifest{}, fmt.Errorf("configuration: encode archive: %w", err)
	}
	return data, manifest, nil
}

// Open verifies and decrypts an archive before returning a payload. It does
// not apply the configuration; import callers must validate compatibility and
// choose an explicit bootstrap/recovery mode before mutating a target.
func Open(data, key []byte, expectedKeyID string) (ArchiveManifest, []byte, error) {
	if len(key) != 32 {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: archive key must be 32 bytes")
	}
	var envelope encryptedArchive
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: decode archive: %w", err)
	}
	if envelope.Manifest.Format != ArchiveFormat || envelope.Manifest.ArchiveID == "" || envelope.Manifest.RevisionID == "" || envelope.Manifest.SchemaVersion != SchemaVersion {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: unsupported or incomplete archive manifest")
	}
	if expectedKeyID != "" && envelope.Manifest.KeyID != expectedKeyID {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: archive key identifier does not match")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: create archive cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: create archive AEAD: %w", err)
	}
	if len(envelope.Nonce) != aead.NonceSize() {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: invalid archive nonce")
	}
	additionalData, err := json.Marshal(envelope.Manifest)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: encode archive manifest: %w", err)
	}
	payload, err := aead.Open(nil, envelope.Nonce, envelope.Ciphertext, additionalData)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: archive authentication failed: %w", err)
	}
	sum := sha256.Sum256(payload)
	if "sha256:"+hex.EncodeToString(sum[:]) != envelope.Manifest.PayloadDigest {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: archive payload digest does not match")
	}
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil || snapshot.APIVersion != SchemaVersion {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: archive payload is not a supported configuration snapshot")
	}
	return envelope.Manifest, payload, nil
}

func uuidLike() string {
	var random [16]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		// Seal has already consumed entropy successfully for the nonce. This is
		// only a non-secret archive label, so retain progress without exposing
		// key material if a second entropy read unexpectedly fails.
		return fmt.Sprintf("archive-%d", time.Now().UTC().UnixNano())
	}
	return "archive-" + hex.EncodeToString(random[:])
}
