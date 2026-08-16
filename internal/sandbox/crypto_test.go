package sandbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSecretBoxRoundTripAndAAD(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt("mysql://user:password@database", "agent:7:MYSQL_DSN")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ciphertext, "v1:") || strings.Contains(ciphertext, "password") {
		t.Fatalf("unexpected ciphertext %q", ciphertext)
	}
	plain, err := box.Decrypt(ciphertext, "agent:7:MYSQL_DSN")
	if err != nil || plain != "mysql://user:password@database" {
		t.Fatalf("round trip = %q, %v", plain, err)
	}
	if _, err := box.Decrypt(ciphertext, "agent:8:MYSQL_DSN"); err == nil {
		t.Fatal("expected authentication failure with different AAD")
	}
}

func TestSecretBoxRejectsInvalidKey(t *testing.T) {
	if _, err := NewSecretBox("too-short"); err == nil {
		t.Fatal("expected invalid key error")
	}
}
