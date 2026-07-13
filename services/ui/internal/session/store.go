package session

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type Record struct {
	ID              string
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Store struct {
	mu  sync.RWMutex
	now func() time.Time
	byID map[string]Record
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, byID: map[string]Record{}}
}

func (s *Store) Create(accessToken, refreshToken string, accessExpiresAt time.Time) (Record, error) {
	id, err := newID()
	if err != nil {
		return Record{}, err
	}
	now := s.now().UTC()
	rec := Record{
		ID:              id,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		AccessExpiresAt: accessExpiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[id] = rec
	return rec, nil
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byID[id]
	return rec, ok
}

func (s *Store) Update(rec Record) {
	rec.UpdatedAt = s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[rec.ID] = rec
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
}

func newID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
