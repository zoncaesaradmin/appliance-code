package main

import (
	"context"
	"sync"
	"time"
)

type fakeEngine struct {
	mu        sync.Mutex
	applied   []enginePodSpec
	deleted   int
	ready     bool
	exists    bool
	oom       bool
	phase     string
	message   string
	applyErr  error
	deleteErr error
	waitErr   error
}

func (f *fakeEngine) EnsureService(context.Context) error { return nil }

func (f *fakeEngine) ApplyEngine(_ context.Context, spec enginePodSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, spec)
	f.exists = true
	f.ready = true
	f.phase = "Running"
	return nil
}

func (f *fakeEngine) DeleteEngine(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted++
	f.exists = false
	f.ready = false
	f.phase = ""
	return nil
}

func (f *fakeEngine) WaitReady(context.Context, time.Duration) error {
	if f.waitErr != nil {
		return f.waitErr
	}
	return nil
}

func (f *fakeEngine) EngineStatus(context.Context) (engineStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return engineStatus{
		Exists:        f.exists,
		Ready:         f.ready,
		Replicas:      1,
		ReadyReplicas: 1,
		Phase:         f.phase,
		Message:       f.message,
		OOMKilled:     f.oom,
	}, nil
}

func (f *fakeEngine) lastApplied() (enginePodSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 {
		return enginePodSpec{}, false
	}
	return f.applied[len(f.applied)-1], true
}
