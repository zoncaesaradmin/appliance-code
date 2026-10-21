package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"appliance-code/services/controlplane/internal/storage"
)

type bootstrapConfigurationImporter struct {
	payload []byte
	actor   string
	err     error
}

func (i *bootstrapConfigurationImporter) Import(_ context.Context, payload []byte, actor string) (storage.ConfigurationRevision, error) {
	i.payload = append([]byte(nil), payload...)
	i.actor = actor
	return storage.ConfigurationRevision{}, i.err
}

func TestImportBootstrapConfiguration(t *testing.T) {
	t.Run("missing optional payload is ignored", func(t *testing.T) {
		importer := &bootstrapConfigurationImporter{}
		if err := importBootstrapConfiguration(t.Context(), filepath.Join(t.TempDir(), "missing.json"), importer); err != nil {
			t.Fatal(err)
		}
		if importer.actor != "" {
			t.Fatal("missing payload must not invoke importer")
		}
	})

	t.Run("present payload uses clean target importer", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "payload.json")
		if err := os.WriteFile(path, []byte(`{"schemaVersion":"zon.configuration/v1"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		importer := &bootstrapConfigurationImporter{}
		if err := importBootstrapConfiguration(t.Context(), path, importer); err != nil {
			t.Fatal(err)
		}
		if importer.actor != "installer-bootstrap" || string(importer.payload) != `{"schemaVersion":"zon.configuration/v1"}` {
			t.Fatalf("unexpected importer call: actor=%q payload=%s", importer.actor, importer.payload)
		}
	})

	t.Run("invalid payload is rejected by importer", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "payload.json")
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		want := errors.New("not clean")
		if err := importBootstrapConfiguration(t.Context(), path, &bootstrapConfigurationImporter{err: want}); !errors.Is(err, want) {
			t.Fatalf("error = %v, want wrapped %v", err, want)
		}
	})
}
