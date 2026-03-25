package worker

import (
	"fmt"
	"os"
	"path/filepath"
)

// ensureSecureWorkDir creates a work directory with strict permissions (0700)
// and enforces permissions even if the directory already exists.
// This is critical for protecting sensitive files like kubeconfigs and dashboard tokens.
func ensureSecureWorkDir(baseDir, clusterID string) (string, error) {
	workDir := filepath.Join(baseDir, clusterID)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return "", fmt.Errorf("create work directory: %w", err)
	}

	// Enforce permissions even if directory already existed
	// This prevents permission drift or manual changes from weakening security
	if err := os.Chmod(workDir, 0700); err != nil {
		return "", fmt.Errorf("enforce work directory permissions: %w", err)
	}

	return workDir, nil
}
