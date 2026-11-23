package identity

import (
	"testing"
	"time"
)

func TestPostgresRequestLimiterRequiresConfiguration(t *testing.T) {
	if _, err := NewPostgresRequestLimiter(nil, nil, time.Minute, map[string]int{"verify": 1}); err == nil {
		t.Fatal("missing database and key accepted")
	}
}
