package services

import (
	"strings"
	"testing"
)

func TestNormalizeEC2KeyName(t *testing.T) {
	tests := map[string]string{
		"docklab-key":     "docklab-key",
		"docklab-key.pem": "docklab-key",
		"docklab-key.PEM": "docklab-key",
		"  my-key.pem  ":  "my-key",
		"":                "",
	}

	for input, want := range tests {
		if got := normalizeEC2KeyName(input); got != want {
			t.Fatalf("normalizeEC2KeyName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateProvisionRequestNormalizesKeyName(t *testing.T) {
	req, err := validateProvisionRequest(ProvisionRequest{
		Region:       "us-east-1",
		InstanceType: "t3.micro",
		AMI:          "ami-0c2b8ca1dad447f8a",
		KeyName:      "docklab-key.pem",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.KeyName != "docklab-key" {
		t.Fatalf("expected normalized key name docklab-key, got %q", req.KeyName)
	}
}

func TestNormalizeCreateTarget(t *testing.T) {
	tests := map[string]string{
		"":        createTargetLocal,
		"local":   createTargetLocal,
		"LOCAL":   createTargetLocal,
		" cloud ": createTargetCloud,
		"cloud":   createTargetCloud,
	}

	for input, want := range tests {
		got, err := normalizeCreateTarget(input)
		if err != nil {
			t.Fatalf("normalizeCreateTarget(%q) unexpected error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeCreateTarget(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := normalizeCreateTarget("hybrid"); err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestIsPlaceholderContainerID(t *testing.T) {
	if !isPlaceholderContainerID("pending-deadbeef") {
		t.Fatal("expected pending container id to be recognized")
	}
	if isPlaceholderContainerID("abc123") {
		t.Fatal("expected real container id to not be placeholder")
	}
}

func TestNewPlaceholderContainerID(t *testing.T) {
	first := newPlaceholderContainerID()
	second := newPlaceholderContainerID()
	if !strings.HasPrefix(first, "pending-") {
		t.Fatalf("expected pending prefix, got %q", first)
	}
	if first == second {
		t.Fatal("expected unique placeholder ids")
	}
}
