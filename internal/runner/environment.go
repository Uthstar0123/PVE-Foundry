package runner

import (
	"os"
	"strings"
)

var blockedCommandEnvironment = map[string]bool{
	"MANAGEMENT_AUTH_KEY":          true,
	"MANAGEMENT_DATABASE_URL":      true,
	"MANAGEMENT_ENCRYPTION_KEY":    true,
	"MANAGEMENT_PROXMOX_API_TOKEN": true,
	"MANAGEMENT_RUNNER_TOKEN":      true,
	"RUNNER_RUNNER_TOKEN":          true,
}

func commandEnvironment() []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !blockedCommandEnvironment[name] {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
