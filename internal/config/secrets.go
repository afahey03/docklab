package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// LoadSecretsFromAWS optionally hydrates process environment variables from an AWS
// Secrets Manager secret before config.Load runs. The secret value must be a flat JSON
// object of key/value pairs (e.g. {"JWT_SECRET": "...", "AWS_SECRET_ACCESS_KEY": "..."}).
//
// Controlled by:
//   - DOKLAB_SECRETS_MANAGER_SECRET_ID — secret name or ARN; empty disables loading
//   - DOKLAB_SECRETS_MANAGER_REGION    — optional region override
//
// Values already present in the environment are never overwritten, so local overrides
// always win. This removes the need for plaintext production secrets in env files.
func LoadSecretsFromAWS(ctx context.Context) (int, error) {
	secretID := strings.TrimSpace(os.Getenv("DOKLAB_SECRETS_MANAGER_SECRET_ID"))
	if secretID == "" {
		return 0, nil
	}

	region := strings.TrimSpace(os.Getenv("DOKLAB_SECRETS_MANAGER_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		region = "us-east-1"
	}

	loadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(loadCtx, awsconfig.WithRegion(region))
	if err != nil {
		return 0, fmt.Errorf("load aws config for secrets manager: %w", err)
	}

	client := secretsmanager.NewFromConfig(awsCfg)
	output, err := client.GetSecretValue(loadCtx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretID,
	})
	if err != nil {
		return 0, fmt.Errorf("get secret %s: %w", secretID, err)
	}
	if output.SecretString == nil {
		return 0, fmt.Errorf("secret %s has no string value", secretID)
	}

	var values map[string]string
	if err := json.Unmarshal([]byte(*output.SecretString), &values); err != nil {
		return 0, fmt.Errorf("parse secret %s as JSON object: %w", secretID, err)
	}

	applied := 0
	for key, value := range values {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return applied, fmt.Errorf("set env %s from secret: %w", key, err)
		}
		applied++
	}

	return applied, nil
}
