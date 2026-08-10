package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/logging"
)

type fakeDNSBootstrapper struct {
	err error
}

func (f fakeDNSBootstrapper) BootstrapSelf(context.Context) error {
	return f.err
}

func TestBootstrapDNSDefersErrors(t *testing.T) {
	var buf strings.Builder
	logger, err := logging.NewWithWriter("warn", &buf)
	if err != nil {
		t.Fatalf("logging.NewWithWriter: %v", err)
	}

	bootErr := errors.New("dnsrecords: patch configmap returned 403: forbidden")
	if err := bootstrapDNS(context.Background(), logger, fakeDNSBootstrapper{err: bootErr}); err != nil {
		t.Fatalf("bootstrapDNS returned error: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "dns zone bootstrap deferred until dns support is ready") {
		t.Fatalf("expected deferred bootstrap warning, got %q", logged)
	}
	if !strings.Contains(logged, bootErr.Error()) {
		t.Fatalf("expected original dns bootstrap error in warning, got %q", logged)
	}
}
