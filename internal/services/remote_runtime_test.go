package services

import (
	"testing"

	"github.com/afahey03/docklab/internal/models"
)

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"":        "''",
		"abc":     "'abc'",
		"it's":    "'it'\\''s'",
		"ami-123": "'ami-123'",
	}

	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUsesRemoteRuntime(t *testing.T) {
	if UsesRemoteRuntime(&models.Environment{RuntimeTarget: runtimeTargetRemote}) != true {
		t.Fatal("expected remote runtime")
	}
	if UsesRemoteRuntime(&models.Environment{RuntimeTarget: runtimeTargetLocal}) {
		t.Fatal("expected local runtime")
	}
}
