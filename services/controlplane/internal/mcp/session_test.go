package mcp

import (
	"errors"
	"testing"
)

func TestSessionStoreCapacityLimit(t *testing.T) {
	store := newSessionStore(2)

	if _, err := store.create(ProtocolVersion); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := store.create(ProtocolVersion); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if _, err := store.create(ProtocolVersion); !errors.Is(err, ErrSessionCapacityReached) {
		t.Errorf("third create error = %v, want ErrSessionCapacityReached", err)
	}
}

func TestSessionStoreLifecycle(t *testing.T) {
	store := newSessionStore(10)

	sess, err := store.create(ProtocolVersion)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, ok := store.get(sess.ID); !ok {
		t.Fatal("get should find the created session")
	}
	if _, ok := store.get("unknown"); ok {
		t.Error("get should not find an unknown session id")
	}

	if !store.markInitialized(sess.ID) {
		t.Error("markInitialized should succeed for a known session")
	}
	got, _ := store.get(sess.ID)
	if !got.Initialized {
		t.Error("session should be marked initialized")
	}

	if !store.delete(sess.ID) {
		t.Error("delete should succeed for a known session")
	}
	if _, ok := store.get(sess.ID); ok {
		t.Error("session should be gone after delete")
	}
	if store.delete(sess.ID) {
		t.Error("deleting an already-deleted session should report false")
	}
}
