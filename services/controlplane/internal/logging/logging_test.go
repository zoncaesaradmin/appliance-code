package logging_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zoncaesaradmin/platformkit/ctxutil"

	"appliance-code/services/controlplane/internal/logging"
)

func TestNewProducesAWorkingLogger(t *testing.T) {
	l, err := logging.New("info")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if l == nil {
		t.Fatal("New returned a nil logger")
	}
	// Exercise every entry point once to ensure the redacting wrapper
	// implements the full Logger interface without panicking.
	l.Info("smoke test")
	l.Infow("smoke test with fields", "key", "value")
	l.WithField("k", "v").Info("chained")
	l.WithContext(context.Background()).Info("with context")
}

func TestRedactsSensitiveKeysInStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.NewWithWriter("debug", &buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	l.Infow("login attempt",
		"username", "alice",
		"password", "hunter2",
		"Authorization", "Bearer abc123",
		"apiToken", "apt_secret",
	)

	out := buf.String()
	if !strings.Contains(out, `"username":"alice"`) {
		t.Errorf("username should not be redacted, got %s", out)
	}
	for _, key := range []string{"password", "Authorization", "apiToken"} {
		if !strings.Contains(out, key+`":"[REDACTED]"`) {
			t.Errorf("%s should be redacted, got %s", key, out)
		}
	}
}

func TestRedactsSensitiveKeysViaWithField(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.NewWithWriter("debug", &buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	l.WithField("password", "hunter2").WithField("username", "alice").Info("login")

	out := buf.String()
	if !strings.Contains(out, `"username":"alice"`) {
		t.Errorf("username should not be redacted, got %s", out)
	}
	if !strings.Contains(out, `"password":"[REDACTED]"`) {
		t.Errorf("password should be redacted, got %s", out)
	}
}

func TestWithContextSurfacesRequestID(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.NewWithWriter("debug", &buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	ctx := ctxutil.WithRequestID(context.Background(), "req-123")
	l.WithContext(ctx).Info("handled request")

	out := buf.String()
	if !strings.Contains(out, `"requestId":"req-123"`) {
		t.Errorf("expected requestId to be surfaced from context, got %s", out)
	}
}
