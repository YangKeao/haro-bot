package sandbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedValuePrefix = "v1:"

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(encodedKey string) (*SecretBox, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return nil, errors.New("HARO_SANDBOX_SECRET_KEY is required when sandbox support is enabled")
	}
	key, err := decodeSecretKey(encodedKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func decodeSecretKey(value string) ([]byte, error) {
	decoders := []func(string) ([]byte, error){base64.StdEncoding.DecodeString, base64.RawStdEncoding.DecodeString, hex.DecodeString}
	for _, decode := range decoders {
		key, err := decode(value)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("HARO_SANDBOX_SECRET_KEY must encode exactly 32 bytes as base64 or hex")
}

func (b *SecretBox) Encrypt(value string, aad string) (string, error) {
	if b == nil || b.aead == nil {
		return "", errors.New("secret encryption is not configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(value), []byte(aad))
	return encryptedValuePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Decrypt(value string, aad string) (string, error) {
	if b == nil || b.aead == nil {
		return "", errors.New("secret encryption is not configured")
	}
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return "", errors.New("unsupported encrypted value version")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted value: %w", err)
	}
	if len(sealed) < b.aead.NonceSize() {
		return "", errors.New("encrypted value is truncated")
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return "", errors.New("encrypted value authentication failed")
	}
	return string(plain), nil
}
