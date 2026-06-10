package services

import (
	"strings"
	"testing"
)

func TestWorkspaceSnapshotScripts(t *testing.T) {
	if stageWorkspaceSnapshotScript == "" || restoreWorkspaceSnapshotScript == "" {
		t.Fatal("snapshot scripts must not be empty")
	}
	for _, needle := range []string{workspaceSnapshotArchive, "/workspace"} {
		if !strings.Contains(stageWorkspaceSnapshotScript, needle) {
			t.Fatalf("stage script missing %q: %s", needle, stageWorkspaceSnapshotScript)
		}
	}
	for _, needle := range []string{workspaceSnapshotArchive, "/workspace", "find"} {
		if !strings.Contains(restoreWorkspaceSnapshotScript, needle) {
			t.Fatalf("restore script missing %q: %s", needle, restoreWorkspaceSnapshotScript)
		}
	}
}
