package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaterializeSSHPrivateKey writes a base64-encoded SSH private key (delivered via
// DOKLAB_SSH_PRIVATE_KEY_B64, typically hydrated from AWS Secrets Manager) to a
// file with 0600 permissions and points SSHPrivateKeyPath at it. This lets
// production hosts run without a .pem file or .env entry on disk; the key only
// exists inside the backend container's filesystem.
//
// A no-op when DOKLAB_SSH_PRIVATE_KEY_B64 is unset, in which case the existing
// DOKLAB_SSH_PRIVATE_KEY_PATH (a mounted file) is used as before.
func (c *Config) MaterializeSSHPrivateKey() error {
	encoded := strings.TrimSpace(c.SSHPrivateKeyB64)
	if encoded == "" {
		return nil
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode DOKLAB_SSH_PRIVATE_KEY_B64: %w", err)
	}

	path := filepath.Join(os.TempDir(), "docklab-ssh-key.pem")
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return fmt.Errorf("write materialized ssh key: %w", err)
	}

	c.SSHPrivateKeyPath = path
	return nil
}
