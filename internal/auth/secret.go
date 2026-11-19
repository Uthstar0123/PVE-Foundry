package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

func EncryptSecret(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create secret envelope: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte("proxmox-orchestrator/secret/v1"))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func DecryptSecret(key []byte, encoded string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("invalid encrypted secret encoding")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("invalid secret key")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(data) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted secret")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte("proxmox-orchestrator/secret/v1"))
	if err != nil {
		return "", errors.New("secret authentication failed")
	}
	return string(plaintext), nil
}
