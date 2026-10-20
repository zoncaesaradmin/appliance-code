package postgres

import (
	"context"
	"testing"
)

func TestOpenRejectsMissingOrInvalidPoolConfiguration(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); err == nil {
		t.Fatal("Open accepted an empty DSN")
	}
	if _, err := Open(context.Background(), Config{DSN: "postgres://example", MaxOpenConns: 0}); err == nil {
		t.Fatal("Open accepted a zero maximum connection count")
	}
	if _, err := Open(context.Background(), Config{DSN: "postgres://example", MaxOpenConns: 1, MaxIdleConns: 2}); err == nil {
		t.Fatal("Open accepted more idle than open connections")
	}
}
