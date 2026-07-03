package mcp

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// session is the in-memory, ephemeral state MCP's Streamable HTTP transport
// requires for version negotiation. It is not appliance product state: it
// never persists across a restart and holds nothing beyond protocol
// bookkeeping, matching the plan's "no long-lived server state unless a
// concrete v1 client flow requires it."
type session struct {
	ID              string
	ProtocolVersion string
	Initialized     bool
	CreatedAt       time.Time
	LastUsedAt      time.Time
}

// ErrSessionCapacityReached is returned by sessionStore.create when the
// configured concurrency limit is already in use.
var ErrSessionCapacityReached = errors.New("mcp: session capacity reached")

// sessionStore is a bounded, mutex-protected map of active MCP sessions,
// giving /mcp its own independent concurrency limit per the plan's
// requirement.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	max      int
}

func newSessionStore(max int) *sessionStore {
	return &sessionStore{sessions: make(map[string]*session), max: max}
}

func (s *sessionStore) create(protocolVersion string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) >= s.max {
		return nil, ErrSessionCapacityReached
	}

	now := time.Now().UTC()
	sess := &session{
		ID: uuid.Must(uuid.NewV7()).String(), ProtocolVersion: protocolVersion,
		CreatedAt: now, LastUsedAt: now,
	}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *sessionStore) get(id string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return session{}, false
	}
	return *sess, true
}

func (s *sessionStore) touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.LastUsedAt = time.Now().UTC()
	}
}

func (s *sessionStore) markInitialized(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return false
	}
	sess.Initialized = true
	return true
}

func (s *sessionStore) delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	return true
}
