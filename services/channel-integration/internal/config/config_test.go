package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.json")
	if err := os.WriteFile(path, []byte(`{"verify_token":"verify","app_secret":"secret","graph_access_token":"token","phone_number_id":"123","graph_api_version":"v21.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	get := func(key string) string {
		if key == "CAPP_WHATSAPP_CONFIG_FILE" {
			return path
		}
		return ""
	}
	got, err := Load(get)
	if err != nil {
		t.Fatal(err)
	}
	if got.PhoneNumberID != "123" || got.ListenAddress != ":8080" {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestLoadRejectsMissingRuntime(t *testing.T) {
	if _, err := Load(func(string) string { return "" }); err == nil {
		t.Fatal("expected missing runtime error")
	}
}
