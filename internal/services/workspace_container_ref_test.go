package services

import (
	"testing"

	"github.com/afahey03/docklab/internal/models"
)

func TestWorkspaceContainerRefUsesRemoteName(t *testing.T) {
	env := &models.Environment{
		ID:            "env-123",
		RuntimeTarget: runtimeTargetRemote,
		ContainerID:   "sha256:deadbeef",
	}

	ref := workspaceContainerRef(env)
	if ref != "docklab-env-123" {
		t.Fatalf("expected docklab-env-123, got %q", ref)
	}
}

func TestWorkspaceContainerRefUsesLocalContainerID(t *testing.T) {
	env := &models.Environment{
		ID:            "env-123",
		RuntimeTarget: runtimeTargetLocal,
		ContainerID:   "local-container-id",
	}

	ref := workspaceContainerRef(env)
	if ref != "local-container-id" {
		t.Fatalf("expected local-container-id, got %q", ref)
	}
}

func TestIsWorkspaceStopIgnorable(t *testing.T) {
	if !isWorkspaceStopIgnorable(&workspaceStopTestError{msg: "remote command failed: Error response from daemon: page not found"}) {
		t.Fatal("expected page not found to be ignorable")
	}
	if isWorkspaceStopIgnorable(&workspaceStopTestError{msg: "permission denied"}) {
		t.Fatal("expected permission denied to remain actionable")
	}
}

type workspaceStopTestError struct {
	msg string
}

func (e *workspaceStopTestError) Error() string {
	return e.msg
}
