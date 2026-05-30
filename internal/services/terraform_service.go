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

type ProvisionRequest struct {
	Region       string
	InstanceType string
	AMI          string
	KeyName      string
}

type ProvisionResult struct {
	InstanceID   string
	PublicIP     string
	TerraformDir string
}

type TerraformRunner interface {
	ProvisionEC2(ctx context.Context, environmentID string, req ProvisionRequest, existingTerraformDir string) (*ProvisionResult, error)
	DestroyEC2(ctx context.Context, terraformDir string) error
}

type TerraformWorkspaceError struct {
	TerraformDir string
	Err          error
}

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

	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(terraformMainTemplate), 0o644); err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: fmt.Errorf("failed to write terraform main.tf: %w", err)}
	}

	vars := map[string]string{
		"aws_region":    req.Region,
		"instance_type": req.InstanceType,
		"ami_id":        req.AMI,
		"key_name":      req.KeyName,
	}
	varBytes, err := json.Marshal(vars)
	if err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: fmt.Errorf("failed to marshal terraform vars: %w", err)}
	}
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars.json"), varBytes, 0o644); err != nil {
		return nil, &TerraformWorkspaceError{TerraformDir: dir, Err: fmt.Errorf("failed to write terraform tfvars: %w", err)}
	}

	if err := runTerraform(ctx, dir, "init", "-input=false", "-no-color"); err != nil {
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

func (r *TerraformCLIRunner) DestroyEC2(ctx context.Context, terraformDir string) error {
	if strings.TrimSpace(terraformDir) == "" {
		return nil
	}

	info, err := os.Stat(terraformDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("terraform state directory not found: %s", terraformDir)
		}
		return fmt.Errorf("failed to access terraform state directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("terraform state path is not a directory: %s", terraformDir)
	}

	if err := runTerraform(ctx, terraformDir, "init", "-input=false", "-no-color"); err != nil {
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

resource "aws_instance" "docklab" {
  ami           = var.ami_id
  instance_type = var.instance_type
  key_name      = var.key_name != "" ? var.key_name : null

  tags = {
    Name = "docklab-workspace"
  }
}

output "instance_id" {
  value = aws_instance.docklab.id
}

output "public_ip" {
  value = aws_instance.docklab.public_ip
}
`
