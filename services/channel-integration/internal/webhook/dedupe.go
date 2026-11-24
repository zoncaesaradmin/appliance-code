package webhook

import (
	"sync"
	"time"
)

const (
	messageDeduplicationTTL = 15 * time.Minute
	messageDeduplicationMax = 10_000
)

// messageDeduplicator suppresses provider retries while the channel adapter
// process is alive. A message is reserved before identity resolution or
// delivery, so concurrent retries cannot send two replies. Failed deliveries
// release the reservation and may be retried; successful deliveries remain
// suppressed until the TTL expires.
//
// The current channel deployment is a single replica. If it is scaled out,
// this contract must move to a shared durable store before doing so.
type messageDeduplicator struct {
	mu      sync.Mutex
	entries map[string]time.Time
	now     func() time.Time
}

func newMessageDeduplicator(now func() time.Time) *messageDeduplicator {
	if now == nil {
		now = time.Now
	}
	return &messageDeduplicator{entries: make(map[string]time.Time), now: now}
}

func (d *messageDeduplicator) reserve(id string) bool {
	if d == nil || id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for key, expires := range d.entries {
		if !expires.After(now) {
			delete(d.entries, key)
		}
	}
	if _, exists := d.entries[id]; exists {
		return false
	}
	if len(d.entries) >= messageDeduplicationMax {
		oldestKey := ""
		var oldest time.Time
		for key, expires := range d.entries {
			if oldestKey == "" || expires.Before(oldest) {
				oldestKey, oldest = key, expires
			}
		}
		if oldestKey != "" {
			delete(d.entries, oldestKey)
		}
	}
	d.entries[id] = now.Add(messageDeduplicationTTL)
	return true
}

func (d *messageDeduplicator) release(id string) {
	if d == nil || id == "" {
		return
	}
	d.mu.Lock()
	delete(d.entries, id)
	d.mu.Unlock()
}
