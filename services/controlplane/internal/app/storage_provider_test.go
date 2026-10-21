package app

import (
	"testing"

	"appliance-code/services/controlplane/internal/config"
)

func TestStorageProviderForSelectsExplicitDatabaseDriver(t *testing.T) {
	tests := []struct {
		driver string
		want   any
	}{
		{driver: "sqlite", want: localSQLStorageProvider{}},
		{driver: " POSTGRES ", want: postgresStorageProvider{}},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			cfg := config.Default()
			cfg.Database.Driver = tt.driver
			provider, err := storageProviderFor(cfg)
			if err != nil {
				t.Fatalf("storageProviderFor: %v", err)
			}
			switch tt.want.(type) {
			case localSQLStorageProvider:
				if _, ok := provider.(localSQLStorageProvider); !ok {
					t.Fatalf("provider = %T, want localSQLStorageProvider", provider)
				}
			case postgresStorageProvider:
				if _, ok := provider.(postgresStorageProvider); !ok {
					t.Fatalf("provider = %T, want postgresStorageProvider", provider)
				}
			}
		})
	}
}

func TestStorageProviderForRejectsUnknownDriver(t *testing.T) {
	cfg := config.Default()
	cfg.Database.Driver = "mysql"
	if _, err := storageProviderFor(cfg); err == nil {
		t.Fatal("storageProviderFor accepted an unknown driver")
	}
}
