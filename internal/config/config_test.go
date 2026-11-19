package config

import "testing"

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
