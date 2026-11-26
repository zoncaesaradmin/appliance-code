package messaging

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// Seal protects a short-lived sensitive event field for broker transport.
// The returned bytes contain the nonce followed by the authenticated GCM
// ciphertext. Callers must still avoid logging the result or plaintext.
func Seal(key, plaintext []byte) ([]byte, error) {
	block, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open authenticates and decrypts a value produced by Seal.
func Open(key, ciphertext []byte) ([]byte, error) {
	block, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("messaging: encrypted value is too short")
	}
	nonce, payload := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, payload, nil)
}

func newCipher(key []byte) (cipher.Block, error) {
	if len(key) != 32 {
		return nil, errors.New("messaging: encryption key must be 32 bytes")
	}
	return aes.NewCipher(key)
}
