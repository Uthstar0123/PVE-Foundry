package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TEST_ADDRESS", "")
	t.Setenv("TEST_ENVIRONMENT", "")

	cfg, err := Load("TEST", ":8080")
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if cfg.Address != ":8080" || cfg.Environment != "development" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadRejectsInvalidAddress(t *testing.T) {
	t.Setenv("TEST_ADDRESS", "not-an-address")
	if _, err := Load("TEST", ":8080"); err == nil {
		t.Fatal("expected an invalid address error")
	}
}

func TestLoadRejectsUnknownEnvironment(t *testing.T) {
	t.Setenv("TEST_ADDRESS", ":8080")
	t.Setenv("TEST_ENVIRONMENT", "unknown")
	if _, err := Load("TEST", ":8080"); err == nil {
		t.Fatal("expected an invalid environment error")
	}
}

func TestLoadRequiresProxmoxURLAndTokenTogether(t *testing.T) {
	t.Setenv("TEST_PROXMOX_API_URL", "https://pve.example.local:8006")
	t.Setenv("TEST_PROXMOX_API_TOKEN", "")
	if _, err := Load("TEST", ":8080"); err == nil || !strings.Contains(err.Error(), "must be provided together") {
		t.Fatalf("expected paired Proxmox configuration error, got %v", err)
	}
}

func TestLoadRejectsInsecureProxmoxTLSInProduction(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("MANAGEMENT_ENVIRONMENT", "production")
	t.Setenv("MANAGEMENT_DATABASE_URL", "postgres://example.invalid/orchestrator")
	t.Setenv("MANAGEMENT_AUTH_KEY", key)
	t.Setenv("MANAGEMENT_ENCRYPTION_KEY", key)
	t.Setenv("MANAGEMENT_PROXMOX_INSECURE_SKIP_VERIFY", "true")
	if _, err := Load("MANAGEMENT", ":8080"); err == nil || !strings.Contains(err.Error(), "forbidden in production") {
		t.Fatalf("expected insecure TLS production error, got %v", err)
	}
}

func TestLoadDevelopmentWithoutProxmox(t *testing.T) {
	t.Setenv("TEST_PROXMOX_API_URL", "")
	t.Setenv("TEST_PROXMOX_API_TOKEN", "")
	t.Setenv("TEST_PROXMOX_CA_FILE", "")
	t.Setenv("TEST_PROXMOX_INSECURE_SKIP_VERIFY", "")
	cfg, err := Load("TEST", ":8080")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxmoxAPIURL != "" || cfg.ProxmoxAPIToken != "" || cfg.ProxmoxInsecure {
		t.Fatalf("unexpected Proxmox configuration: %+v", cfg)
	}
}
