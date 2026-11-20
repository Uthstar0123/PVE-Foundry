package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
)

type Config struct {
	Address          string
	DatabaseURL      string
	Environment      string
	AuthKey          []byte
	EncryptionKey    []byte
	ProxmoxAPIURL    string
	ProxmoxAPIToken  string
	ProxmoxCAFile    string
	ProxmoxInsecure  bool
	RunnerToken      string
	ManagementURL    string
	ManagementCAFile string
	OpenTofuBinary   string
	WorkRoot         string
	StateRoot        string
}

func Load(prefix, defaultAddress string) (Config, error) {
	cfg := Config{
		Address:     valueOrDefault(prefix+"_ADDRESS", defaultAddress),
		DatabaseURL: os.Getenv(prefix + "_DATABASE_URL"),
		Environment: valueOrDefault(prefix+"_ENVIRONMENT", "development"),
	}
	if _, _, err := net.SplitHostPort(cfg.Address); err != nil {
		return Config{}, fmt.Errorf("invalid %s_ADDRESS %q: %w", prefix, cfg.Address, err)
	}
	switch cfg.Environment {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("invalid %s_ENVIRONMENT %q", prefix, cfg.Environment)
	}
	var err error
	if cfg.AuthKey, err = loadKey(prefix+"_AUTH_KEY", cfg.Environment == "production"); err != nil {
		return Config{}, err
	}
	if cfg.EncryptionKey, err = loadKey(prefix+"_ENCRYPTION_KEY", cfg.Environment == "production"); err != nil {
		return Config{}, err
	}
	if prefix == "MANAGEMENT" && cfg.Environment == "production" && cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("MANAGEMENT_DATABASE_URL is required in production")
	}
	cfg.ProxmoxAPIURL = os.Getenv(prefix + "_PROXMOX_API_URL")
	cfg.ProxmoxAPIToken = os.Getenv(prefix + "_PROXMOX_API_TOKEN")
	cfg.ProxmoxCAFile = os.Getenv(prefix + "_PROXMOX_CA_FILE")
	cfg.ProxmoxInsecure = os.Getenv(prefix+"_PROXMOX_INSECURE_SKIP_VERIFY") == "true"
	if (cfg.ProxmoxAPIURL == "") != (cfg.ProxmoxAPIToken == "") {
		return Config{}, fmt.Errorf("%s_PROXMOX_API_URL and %s_PROXMOX_API_TOKEN must be provided together", prefix, prefix)
	}
	if cfg.ProxmoxInsecure && cfg.Environment == "production" {
		return Config{}, fmt.Errorf("%s_PROXMOX_INSECURE_SKIP_VERIFY is forbidden in production", prefix)
	}
	cfg.RunnerToken = os.Getenv(prefix + "_RUNNER_TOKEN")
	if prefix == "MANAGEMENT" && cfg.Environment == "production" && len(cfg.RunnerToken) < 32 {
		return Config{}, fmt.Errorf("MANAGEMENT_RUNNER_TOKEN must contain at least 32 characters in production")
	}
	if prefix == "RUNNER" {
		cfg.ManagementURL = os.Getenv("RUNNER_MANAGEMENT_URL")
		cfg.ManagementCAFile = os.Getenv("RUNNER_MANAGEMENT_CA_FILE")
		cfg.OpenTofuBinary = valueOrDefault("RUNNER_OPENTOFU_BINARY", "tofu")
		cfg.WorkRoot = valueOrDefault("RUNNER_WORK_ROOT", "/var/lib/pve-foundry/jobs")
		cfg.StateRoot = valueOrDefault("RUNNER_STATE_ROOT", "/var/lib/pve-foundry/state")
		if len(cfg.RunnerToken) < 32 || cfg.ManagementURL == "" {
			return Config{}, fmt.Errorf("RUNNER_MANAGEMENT_URL and a RUNNER_RUNNER_TOKEN of at least 32 characters are required")
		}
		if cfg.Environment == "production" && len(cfg.ManagementURL) >= 8 && cfg.ManagementURL[:8] != "https://" {
			return Config{}, fmt.Errorf("RUNNER_MANAGEMENT_URL must use HTTPS in production")
		}
	}
	return cfg, nil
}

func loadKey(name string, required bool) ([]byte, error) {
	encoded := os.Getenv(name)
	if encoded == "" {
		if required {
			return nil, fmt.Errorf("%s is required in production", name)
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate development key: %w", err)
		}
		return key, nil
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must be a base64-encoded 32-byte key", name)
	}
	return key, nil
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
