package services

import (
	"log/slog"
	"os"
	"testing"
)

func TestNewCloudLifecycleServiceDefaultPolicy(t *testing.T) {
	service := NewCloudLifecycleService(nil, nil, nil, nil, nil, 60, 0, 0, true, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	policy := service.Policy()
	if !policy.Enabled {
		t.Fatal("expected cloud idle policy to be enabled")
	}
	if policy.WorkspaceIdleStopMinutes != 60 {
		t.Fatalf("expected workspace idle stop 60, got %d", policy.WorkspaceIdleStopMinutes)
	}
	if policy.CloudIdleStopMinutes != 120 {
		t.Fatalf("expected default cloud idle stop 120, got %d", policy.CloudIdleStopMinutes)
	}
	if policy.CloudIdleTerminateMinutes != 24*60 {
		t.Fatalf("expected default cloud idle terminate 1440, got %d", policy.CloudIdleTerminateMinutes)
	}
}

func TestIsEC2InstanceTerminated(t *testing.T) {
	if !isEC2InstanceTerminated("terminated") {
		t.Fatal("expected terminated state to be terminal")
	}
	if !isEC2InstanceTerminated("shutting-down") {
		t.Fatal("expected shutting-down state to be terminal")
	}
	if isEC2InstanceTerminated("stopped") {
		t.Fatal("expected stopped state to remain manageable")
	}
}
