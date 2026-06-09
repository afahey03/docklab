package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTerraformWorkspaceIncludesEnvironmentID(t *testing.T) {
	dir := t.TempDir()
	cfg := terraformBackendConfig{
		Bucket:    "test-bucket",
		Region:    "us-east-1",
		Table:     "test-table",
		KeyPrefix: "docklab/environments",
		Key:       "docklab/environments/env-123/terraform.tfstate",
	}

	err := writeTerraformWorkspace(dir, cfg, "env-123", ProvisionRequest{
		Region:         "us-east-1",
		InstanceType:   "t3.micro",
		AMI:            "ami-0c2b8ca1dad447f8a",
		KeyName:        "docklab-key",
		WorkspaceImage: "alpine:3.20",
	})
	if err != nil {
		t.Fatalf("writeTerraformWorkspace failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "terraform.tfvars.json"))
	if err != nil {
		t.Fatalf("read tfvars: %v", err)
	}

	var vars map[string]string
	if err := json.Unmarshal(data, &vars); err != nil {
		t.Fatalf("parse tfvars: %v", err)
	}

	if vars["environment_id"] != "env-123" {
		t.Fatalf("expected environment_id env-123, got %q", vars["environment_id"])
	}
	if vars["workspace_image"] != "alpine:3.20" {
		t.Fatalf("expected workspace_image alpine:3.20, got %q", vars["workspace_image"])
	}
}
