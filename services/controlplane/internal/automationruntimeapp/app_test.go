package automationruntimeapp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"appliance-code/services/controlplane/internal/automationruntimeconfig"
	"appliance-code/services/controlplane/internal/logging"
)

func TestNewRequiresOnlyAPITokenPepperFromReadOnlyKeysMount(t *testing.T) {
	dataDir := t.TempDir()
	keysDir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(keysDir, 0o750); err != nil {
		t.Fatal(err)
	}
	pepper := make([]byte, 32)
	if err := os.WriteFile(filepath.Join(keysDir, "api_token_pepper.key"), []byte(base64.StdEncoding.EncodeToString(pepper)), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keysDir, 0o550); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(keysDir, 0o750) })
	cfg := automationruntimeconfig.Default()
	cfg.DataDir = dataDir
	cfg.ApplicationLogPath = filepath.Join(dataDir, "application.log")
	logger, err := logging.NewWithWriter("info", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(cfg, logger, logger)
	if err != nil {
		t.Fatalf("New with an API-token-only key mount: %v", err)
	}
	defer app.db.Close()
}
