package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrTerraformUnavailable = errors.New("terraform CLI is not installed or unavailable")
var ErrTerraformStateBackendConfigMissing = errors.New("terraform state backend configuration is incomplete")

type ProvisionRequest struct {
	Region         string
	InstanceType   string
	AMI            string
	KeyName        string
	WorkspaceImage string
}

type ProvisionResult struct {
	InstanceID   string
	PublicIP     string
	TerraformDir string
}

type TerraformRunner interface {
	ProvisionEC2(ctx context.Context, environmentID string, req ProvisionRequest, existingTerraformDir string) (*ProvisionResult, error)
	DestroyEC2(ctx context.Context, environmentID, terraformDir string) error
}

type TerraformWorkspaceError struct {
	TerraformDir string
	Err          error
}

type terraformBackendConfig struct {
	Bucket    string
	Region    string
	Table     string
	KeyPrefix string
	Key       string
}

type terraformBackendMarker struct {
	Mode      string `json:"mode"`
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	Table     string `json:"table,omitempty"`
	KeyPrefix string `json:"key_prefix,omitempty"`
	Key       string `json:"key,omitempty"`
}

const terraformBackendMarkerFile = ".docklab-backend.json"

func (e *TerraformWorkspaceError) Error() string {
	if e == nil || e.Err == nil {
		return "terraform workspace error"
	}
	return e.Err.Error()
}

func (e *TerraformWorkspaceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type TerraformCLIRunner struct{}

func NewTerraformCLIRunner() *TerraformCLIRunner {
	return &TerraformCLIRunner{}
}

func (r *TerraformCLIRunner) ProvisionEC2(ctx context.Context, environmentID string, req ProvisionRequest, existingTerraformDir string) (*ProvisionResult, error) {
	backendConfig, err := loadTerraformBackendConfig(environmentID)
	if err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: existingTerraformDir, Err: err}
	}

	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.InstanceType == "" {
		req.InstanceType = "t3.micro"
	}
	if req.AMI == "" {
		req.AMI = "ami-0c2b8ca1dad447f8a"
	}

	dir := strings.TrimSpace(existingTerraformDir)
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", fmt.Sprintf("docklab-tf-%s-", environmentID))
		if err != nil {
			return nil, fmt.Errorf("failed to create terraform workspace: %w", err)
		}
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to ensure terraform workspace: %w", err)
		}
	}

	if err := writeTerraformWorkspace(dir, backendConfig, environmentID, req); err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: err}
	}

	if err := runTerraform(ctx, dir, terraformInitArgs(backendConfig)...); err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: err}
	}
	if err := runTerraform(ctx, dir, "apply", "-auto-approve", "-input=false", "-no-color"); err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: err}
	}

	outputsJSON, err := runTerraformCapture(ctx, dir, "output", "-json")
	if err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: err}
	}

	instanceID, publicIP, err := parseTerraformOutputs(outputsJSON)
	if err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: err}
	}

	return &ProvisionResult{
		InstanceID:   instanceID,
		PublicIP:     publicIP,
		TerraformDir: dir,
	}, nil
}

func (r *TerraformCLIRunner) DestroyEC2(ctx context.Context, environmentID, terraformDir string) error {
	if strings.TrimSpace(terraformDir) == "" {
		var err error
		terraformDir, err = os.MkdirTemp("", fmt.Sprintf("docklab-tf-%s-", environmentID))
		if err != nil {
			return fmt.Errorf("failed to create terraform workspace: %w", err)
		}
	}

	info, err := os.Stat(terraformDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(terraformDir, 0o755); err != nil {
				return fmt.Errorf("failed to create terraform workspace: %w", err)
			}
		} else {
			return fmt.Errorf("failed to access terraform state directory: %w", err)
		}
	} else if !info.IsDir() {
		return fmt.Errorf("terraform state path is not a directory: %s", terraformDir)
	}

	_, initArgs, err := loadTerraformDestroyConfig(environmentID, terraformDir)
	if err != nil {
		return err
	}

	cfg, err := loadTerraformBackendConfig(environmentID)
	if err != nil {
		return err
	}
	req := loadTerraformVarsFromDir(terraformDir)
	if err := writeTerraformWorkspace(terraformDir, cfg, environmentID, req); err != nil {
		return err
	}

	if err := runTerraform(ctx, terraformDir, initArgs...); err != nil {
		return err
	}

	if err := runTerraform(ctx, terraformDir, "destroy", "-auto-approve", "-input=false", "-no-color"); err != nil {
		return err
	}

	if err := os.RemoveAll(terraformDir); err != nil {
		return fmt.Errorf("failed to remove terraform workspace: %w", err)
	}

	return nil
}

func terraformInitArgs(cfg terraformBackendConfig) []string {
	args := []string{"init", "-input=false", "-no-color", "-reconfigure"}
	args = append(args,
		"-backend-config=bucket="+cfg.Bucket,
		"-backend-config=key="+cfg.Key,
		"-backend-config=region="+cfg.Region,
		"-backend-config=dynamodb_table="+cfg.Table,
		"-backend-config=encrypt=true",
	)
	return args
}

func loadTerraformBackendConfig(environmentID string) (terraformBackendConfig, error) {
	bucket := strings.TrimSpace(os.Getenv("DOKLAB_TERRAFORM_STATE_BUCKET"))
	region := strings.TrimSpace(os.Getenv("DOKLAB_TERRAFORM_STATE_REGION"))
	table := strings.TrimSpace(os.Getenv("DOKLAB_TERRAFORM_STATE_TABLE"))
	prefix := strings.TrimSpace(os.Getenv("DOKLAB_TERRAFORM_STATE_KEY_PREFIX"))
	if prefix == "" {
		prefix = "docklab/environments"
	}

	if bucket == "" || region == "" || table == "" {
		return terraformBackendConfig{}, ErrTerraformStateBackendConfigMissing
	}

	return terraformBackendConfig{
		Bucket:    bucket,
		Region:    region,
		Table:     table,
		KeyPrefix: prefix,
		Key:       strings.TrimSuffix(prefix, "/") + "/" + environmentID + "/terraform.tfstate",
	}, nil
}

func loadTerraformDestroyConfig(environmentID, terraformDir string) (terraformBackendConfig, []string, error) {
	marker, markerExists, err := readTerraformBackendMarker(terraformDir)
	if err != nil {
		return terraformBackendConfig{}, nil, err
	}
	if markerExists {
		cfg := terraformBackendConfig{
			Bucket:    marker.Bucket,
			Region:    marker.Region,
			Table:     marker.Table,
			KeyPrefix: marker.KeyPrefix,
			Key:       marker.Key,
		}
		return cfg, terraformInitArgs(cfg), nil
	}

	localStatePath := filepath.Join(terraformDir, "terraform.tfstate")
	if _, err := os.Stat(localStatePath); err == nil {
		return terraformBackendConfig{}, []string{"init", "-input=false", "-no-color"}, nil
	}

	cfg, err := loadTerraformBackendConfig(environmentID)
	if err != nil {
		return terraformBackendConfig{}, nil, err
	}
	return cfg, terraformInitArgs(cfg), nil
}

func writeTerraformBackendMarker(dir string, cfg terraformBackendConfig) error {
	marker := terraformBackendMarker{
		Mode:      "s3",
		Bucket:    cfg.Bucket,
		Region:    cfg.Region,
		Table:     cfg.Table,
		KeyPrefix: cfg.KeyPrefix,
		Key:       cfg.Key,
	}

	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal terraform backend marker: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, terraformBackendMarkerFile), markerBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write terraform backend marker: %w", err)
	}

	return nil
}

func readTerraformBackendMarker(terraformDir string) (terraformBackendMarker, bool, error) {
	markerPath := filepath.Join(terraformDir, terraformBackendMarkerFile)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return terraformBackendMarker{}, false, nil
		}
		return terraformBackendMarker{}, false, fmt.Errorf("failed to read terraform backend marker: %w", err)
	}

	var marker terraformBackendMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return terraformBackendMarker{}, false, fmt.Errorf("failed to parse terraform backend marker: %w", err)
	}

	return marker, true, nil
}

func runTerraform(ctx context.Context, dir string, args ...string) error {
	_, err := runTerraformCapture(ctx, dir, args...)
	return err
}

func runTerraformCapture(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrTerraformUnavailable
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", fmt.Errorf("terraform command failed: %w", err)
		}
		return "", fmt.Errorf("terraform command failed: %s", trimmed)
	}

	return string(output), nil
}

func parseTerraformOutputs(raw string) (string, string, error) {
	type valueContainer struct {
		Value any `json:"value"`
	}

	var out map[string]valueContainer
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", "", fmt.Errorf("failed to parse terraform outputs: %w", err)
	}

	instanceID := ""
	if v, ok := out["instance_id"]; ok && v.Value != nil {
		instanceID = fmt.Sprint(v.Value)
	}

	publicIP := ""
	if v, ok := out["public_ip"]; ok && v.Value != nil {
		publicIP = fmt.Sprint(v.Value)
	}

	return instanceID, publicIP, nil
}

func writeTerraformWorkspace(dir string, backendConfig terraformBackendConfig, environmentID string, req ProvisionRequest) error {
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(terraformMainTemplate), 0o644); err != nil {
		return fmt.Errorf("failed to write terraform main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(terraformBackendTemplate), 0o644); err != nil {
		return fmt.Errorf("failed to write terraform backend.tf: %w", err)
	}
	if err := writeTerraformBackendMarker(dir, backendConfig); err != nil {
		return err
	}

	req = normalizeProvisionRequest(req)
	vars := map[string]string{
		"aws_region":       req.Region,
		"instance_type":    req.InstanceType,
		"ami_id":           req.AMI,
		"key_name":         req.KeyName,
		"environment_id":   environmentID,
		"workspace_image":  req.WorkspaceImage,
	}
	varBytes, err := json.Marshal(vars)
	if err != nil {
		return fmt.Errorf("failed to marshal terraform vars: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars.json"), varBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write terraform tfvars: %w", err)
	}

	return nil
}

func loadTerraformVarsFromDir(dir string) ProvisionRequest {
	req := ProvisionRequest{}
	data, err := os.ReadFile(filepath.Join(dir, "terraform.tfvars.json"))
	if err != nil {
		return req
	}

	var vars map[string]string
	if err := json.Unmarshal(data, &vars); err != nil {
		return req
	}

	req.Region = vars["aws_region"]
	req.InstanceType = vars["instance_type"]
	req.AMI = vars["ami_id"]
	req.KeyName = vars["key_name"]
	req.WorkspaceImage = vars["workspace_image"]
	return req
}

func normalizeProvisionRequest(req ProvisionRequest) ProvisionRequest {
	if req.Region == "" {
		req.Region = "us-east-1"
	}
	if req.InstanceType == "" {
		req.InstanceType = "t3.micro"
	}
	if req.AMI == "" {
		req.AMI = "ami-0c2b8ca1dad447f8a"
	}
	return req
}

const terraformBackendTemplate = `
terraform {
	required_version = ">= 1.5.0"
	backend "s3" {}
}
`

const terraformMainTemplate = `
terraform {
	required_version = ">= 1.5.0"
	required_providers {
		aws = {
			source  = "hashicorp/aws"
			version = "~> 5.0"
		}
	}
}

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  type = string
}

variable "instance_type" {
  type = string
}

variable "ami_id" {
  type = string
}

variable "key_name" {
  type = string
}

variable "environment_id" {
  type = string
}

variable "workspace_image" {
  type    = string
  default = ""
}

data "aws_vpc" "default" {
  default = true
}

resource "aws_security_group" "docklab" {
  name        = "docklab-${var.environment_id}"
  description = "DockLab workspace SSH access for ${var.environment_id}"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name         = "docklab-workspace-sg"
    DockLabEnvID = var.environment_id
  }
}

resource "aws_instance" "docklab" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name != "" ? var.key_name : null
  vpc_security_group_ids      = [aws_security_group.docklab.id]
  associate_public_ip_address = true

  user_data = <<-EOF
              #!/bin/bash
              set -euxo pipefail
              if command -v dnf >/dev/null 2>&1; then
                dnf install -y docker
                systemctl enable docker
                systemctl start docker
                usermod -aG docker ec2-user
              elif command -v yum >/dev/null 2>&1; then
                yum install -y docker
                systemctl enable docker
                systemctl start docker
                usermod -aG docker ec2-user
              elif command -v apt-get >/dev/null 2>&1; then
                apt-get update
                apt-get install -y docker.io
                systemctl enable docker
                systemctl start docker
                usermod -aG docker ubuntu
              fi
              if [ -n "${var.workspace_image}" ]; then
                docker pull "${var.workspace_image}"
              fi
              EOF

  tags = {
    Name         = "docklab-workspace"
    DockLabEnvID = var.environment_id
  }
}

output "instance_id" {
  value = aws_instance.docklab.id
}

output "public_ip" {
  value = aws_instance.docklab.public_ip
}
`
