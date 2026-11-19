package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordHashAndVerification(t *testing.T) {
	hash, err := HashPassword("a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "sufficiently") {
		t.Fatalf("unexpected hash: %s", hash)
	}
	if !VerifyPassword(hash, "a sufficiently long password") || VerifyPassword(hash, "wrong password") {
		t.Fatal("password verification returned an incorrect result")
	}
}

func TestRejectsShortPasswordAndMalformedHash(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
	if VerifyPassword("not-a-hash", "a sufficiently long password") {
		t.Fatal("malformed hash accepted")
	}
}

func TestSecretEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	ciphertext, err := EncryptSecret(key, "token-value")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "token-value") {
		t.Fatal("plaintext leaked")
	}
	plaintext, err := DecryptSecret(key, ciphertext)
	if err != nil || plaintext != "token-value" {
		t.Fatalf("round trip failed: %q %v", plaintext, err)
	}
	tampered := []byte(ciphertext)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	if _, err := DecryptSecret(key, string(tampered)); err == nil {
		t.Fatal("tampering was not detected")
	}
}
