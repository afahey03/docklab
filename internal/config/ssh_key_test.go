package config

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestMaterializeSSHPrivateKeyNoop(t *testing.T) {
	cfg := Config{SSHPrivateKeyPath: "/some/mounted/key.pem"}
	if err := cfg.MaterializeSSHPrivateKey(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SSHPrivateKeyPath != "/some/mounted/key.pem" {
		t.Fatalf("path should be unchanged, got %s", cfg.SSHPrivateKeyPath)
	}
}

func TestMaterializeSSHPrivateKeyWritesFile(t *testing.T) {
	keyContent := "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n"
	cfg := Config{
		SSHPrivateKeyPath: "/ignored/by/b64.pem",
		SSHPrivateKeyB64:  base64.StdEncoding.EncodeToString([]byte(keyContent)),
	}

	if err := cfg.MaterializeSSHPrivateKey(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { os.Remove(cfg.SSHPrivateKeyPath) })

	if cfg.SSHPrivateKeyPath == "/ignored/by/b64.pem" {
		t.Fatal("expected path to be overridden by materialized key")
	}

	data, err := os.ReadFile(cfg.SSHPrivateKeyPath)
	if err != nil {
		t.Fatalf("read materialized key: %v", err)
	}
	if string(data) != keyContent {
		t.Fatalf("materialized key content mismatch: %q", string(data))
	}
}

func TestMaterializeSSHPrivateKeyInvalidBase64(t *testing.T) {
	cfg := Config{SSHPrivateKeyB64: "not-valid-base64!!!"}
	if err := cfg.MaterializeSSHPrivateKey(); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}
