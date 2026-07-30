package config

import "testing"

func TestLoadAppliesCanonicalHostAgentEnvironment(t *testing.T) {
	cfg, err := Load([]string{
		"HOST_AGENT_ADDR=127.0.0.1:19000",
		"HOST_AGENT_SOCKET_PATH=/run/test/agent.sock",
		"HOST_AGENT_APPLICATION_LOG_PATH=/tmp/host-agent.log",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:19000" {
		t.Fatalf("Addr = %q, want 127.0.0.1:19000", cfg.Addr)
	}
	if cfg.SocketPath != "/run/test/agent.sock" {
		t.Fatalf("SocketPath = %q, want /run/test/agent.sock", cfg.SocketPath)
	}
	if cfg.ApplicationLogPath != "/tmp/host-agent.log" {
		t.Fatalf("ApplicationLogPath = %q, want /tmp/host-agent.log", cfg.ApplicationLogPath)
	}
}

func TestValidateRejectsRelativeSocketPath(t *testing.T) {
	cfg := Default()
	cfg.SocketPath = "relative.sock"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should reject a relative socket path")
	}
}
