package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrKubectlUnavailable = errors.New("kubectl CLI is not installed or unavailable")
var ErrSnapshotUnsupportedRuntime = errors.New("snapshots are not supported for the kubernetes runtime")

// KubernetesRuntime implements ContainerRuntime on top of kubectl. Each workspace is a
// single-replica Deployment running `sleep infinity`, so stop/start map to scaling the
// deployment to 0/1 replicas. Enabled with DOKLAB_RUNTIME=kubernetes.
type KubernetesRuntime struct {
	namespace string
	context   string
}

func NewKubernetesRuntime(namespace, kubeContext string) *KubernetesRuntime {
	if strings.TrimSpace(namespace) == "" {
		namespace = "docklab"
	}
	return &KubernetesRuntime{namespace: namespace, context: kubeContext}
}

func (k *KubernetesRuntime) Namespace() string {
	return k.namespace
}

// BaseArgs returns the kubectl args shared by every invocation (namespace/context).
func (k *KubernetesRuntime) BaseArgs() []string {
	args := []string{"--namespace", k.namespace}
	if strings.TrimSpace(k.context) != "" {
		args = append(args, "--context", k.context)
	}
	return args
}

func (k *KubernetesRuntime) runKubectl(ctx context.Context, args ...string) (string, error) {
	full := append(k.BaseArgs(), args...)
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrKubectlUnavailable
		}
		trimmed := strings.TrimSpace(stderr.String())
		if trimmed == "" {
			trimmed = strings.TrimSpace(stdout.String())
		}
		if trimmed == "" {
			return "", fmt.Errorf("kubectl command failed: %w", err)
		}
		return "", fmt.Errorf("kubectl command failed: %s", trimmed)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// sanitizeK8sName converts a workspace name to a valid DNS-1123 resource name.
func sanitizeK8sName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	mapped := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, lowered)
	mapped = strings.Trim(mapped, "-")
	if mapped == "" {
		mapped = "workspace"
	}
	if len(mapped) > 53 {
		mapped = mapped[:53]
	}
	return mapped
}

func (k *KubernetesRuntime) CreateWorkspace(ctx context.Context, name, image string, labels map[string]string) (string, error) {
	deploymentName := sanitizeK8sName(name)

	// Ensure the namespace exists; ignore AlreadyExists-style failures.
	_, _ = k.runKubectl(ctx, "create", "namespace", k.namespace)

	args := []string{"create", "deployment", deploymentName, "--image", image, "--replicas", "1", "--", "sleep", "infinity"}
	if _, err := k.runKubectl(ctx, args...); err != nil {
		return "", err
	}

	labelArgs := []string{"label", "deployment", deploymentName, "docklab=workspace"}
	for key, value := range labels {
		safeKey := strings.ReplaceAll(key, ".", "-")
		labelArgs = append(labelArgs, fmt.Sprintf("%s=%s", safeKey, sanitizeK8sName(value)))
	}
	_, _ = k.runKubectl(ctx, labelArgs...)

	return deploymentName, nil
}

func (k *KubernetesRuntime) StartWorkspace(ctx context.Context, containerID string) error {
	_, err := k.runKubectl(ctx, "scale", "deployment", containerID, "--replicas", "1")
	return err
}

func (k *KubernetesRuntime) StopWorkspace(ctx context.Context, containerID string) error {
	_, err := k.runKubectl(ctx, "scale", "deployment", containerID, "--replicas", "0")
	return err
}

func (k *KubernetesRuntime) DeleteWorkspace(ctx context.Context, containerID string) error {
	_, err := k.runKubectl(ctx, "delete", "deployment", containerID, "--ignore-not-found")
	return err
}

func (k *KubernetesRuntime) CommitWorkspace(ctx context.Context, containerRef, imageTag string) error {
	return ErrSnapshotUnsupportedRuntime
}

func (k *KubernetesRuntime) DeleteWorkspaceVolume(ctx context.Context, workspaceName string) error {
	// Kubernetes workspaces use the pod's writable layer; nothing to clean up.
	return nil
}
