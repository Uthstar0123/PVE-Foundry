package runner

import (
	"strings"
	"testing"
)

func TestCommandEnvironmentRemovesServiceCredentials(t *testing.T) {
	t.Setenv("RUNNER_RUNNER_TOKEN", "runner-secret")
	t.Setenv("MANAGEMENT_ENCRYPTION_KEY", "management-secret")
	t.Setenv("PROXMOX_VE_API_TOKEN", "provider-token-placeholder")
	environment := strings.Join(commandEnvironment(), "\n")
	if strings.Contains(environment, "runner-secret") || strings.Contains(environment, "management-secret") {
		t.Fatal("service credentials leaked into the OpenTofu environment")
	}
	if !strings.Contains(environment, "PROXMOX_VE_API_TOKEN=provider-token-placeholder") {
		t.Fatal("provider environment was not preserved")
	}
}
