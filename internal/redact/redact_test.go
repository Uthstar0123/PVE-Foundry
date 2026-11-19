package redact

import (
	"strings"
	"testing"
)

func TestTextMasksCredentialsAndExplicitSecrets(t *testing.T) {
	input := "PVEAPIToken=user@pve!token=supersecret Authorization: Bearer abc.def password=hunter2 api_key: xyz custom-value"
	output := Text(input, "custom-value")
	for _, secret := range []string{"supersecret", "abc.def", "hunter2", "xyz", "custom-value"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q remained in %q", secret, output)
		}
	}
	if !strings.Contains(output, "PVEAPIToken=") || !strings.Contains(output, "password=") {
		t.Fatalf("log context was removed: %q", output)
	}
	if strings.Count(output, replacement) < 5 {
		t.Fatalf("not enough values masked: %q", output)
	}
}

func TestTextMasksPrivateKeys(t *testing.T) {
	output := Text("before\n-----BEGIN PRIVATE KEY-----\nsecret-key-material\n-----END PRIVATE KEY-----\nafter")
	if strings.Contains(output, "secret-key-material") || !strings.Contains(output, "before") || !strings.Contains(output, "after") {
		t.Fatalf("private key redaction failed: %q", output)
	}
}
