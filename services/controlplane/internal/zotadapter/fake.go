package zotadapter

import (
	"context"
	"sync"
)

// Fake is an in-process Client used by unit and HTTP contract tests so they
// never require a real zot instance, per the plan's local-first testing
// rule. Integration lanes exercising the pinned zot release are separate
// and gated on real infrastructure.
type Fake struct {
	mu sync.Mutex

	// Tags maps repository name to its tags. A repository with no entry is
	// treated as not found.
	Tags map[string][]string

	// Referrers maps "repository@digest" to its referrer descriptors.
	Referrers map[string][]Descriptor

	HealthErr error
}

// NewFake returns an empty Fake ready for tests to populate.
func NewFake() *Fake {
	return &Fake{Tags: map[string][]string{}, Referrers: map[string][]Descriptor{}}
}

func (f *Fake) ListRepositories(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.Tags))
	for name := range f.Tags {
		names = append(names, name)
	}
	return names, nil
}

func (f *Fake) ListTags(_ context.Context, repository string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tags, ok := f.Tags[repository]
	if !ok {
		return nil, ErrNotFound
	}
	return tags, nil
}

func (f *Fake) ListReferrers(_ context.Context, repository, digest string) ([]Descriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Referrers[repository+"@"+digest], nil
}

func (f *Fake) Health(_ context.Context) error {
	return f.HealthErr
}
