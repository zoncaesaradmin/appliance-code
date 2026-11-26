package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
)

// PostgresStore persists onboarding and channel identity state. Authentication
// tables are owned and mutated by Auth Service; this store only uses the
// shared session table for channel-created sessions and domain lookups.
type PostgresStore struct {
	db  *sql.DB
	key []byte
}

func NewPostgresStore(db *sql.DB, key []byte) (*PostgresStore, error) {
	if db == nil || len(key) < 32 {
		return nil, errors.New("invalid postgres identity configuration")
	}
	return &PostgresStore{db: db, key: append([]byte(nil), key...)}, nil
}

func (s *PostgresStore) rateDigest(kind, value string) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(kind + ":" + value))
	return mac.Sum(nil)
}
