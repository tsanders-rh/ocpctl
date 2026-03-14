package worker

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleAWSDestroy handles AWS-specific cluster cleanup
// This should be called AFTER openshift-install destroy cluster completes
func (h *DestroyHandler) HandleAWSDestroy(ctx context.Context, cluster *types.Cluster, inst *installer.Installer, workDir string) error {
	log.Printf("AWS cluster cleanup: cleaning up CCO IAM roles and OIDC provider for %s", cluster.Name)

	// Run ccoctl aws delete to clean up IAM roles and OIDC provider
	// ccoctl aws delete --name <cluster-name> --region <region>
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "ccoctl", "aws", "delete",
		"--name", cluster.Name,
		"--region", cluster.Region,
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		// Check if resources were already deleted (not an error)
		if strings.Contains(outputStr, "NoSuchEntity") ||
			strings.Contains(outputStr, "not found") ||
			strings.Contains(outputStr, "does not exist") {
			log.Printf("CCO resources for %s already deleted or not found", cluster.Name)
			return nil
		}

		// Log the error but don't fail - resources might be partially deleted
		log.Printf("Warning: ccoctl aws delete encountered errors for %s: %v", cluster.Name, err)
		log.Printf("ccoctl output:\n%s", outputStr)

		// Return error for visibility but don't fail the destroy job
		return fmt.Errorf("ccoctl aws delete: %w\nOutput: %s", err, outputStr)
	}

	log.Printf("Successfully cleaned up AWS CCO resources for %s", cluster.Name)
	log.Printf("ccoctl output:\n%s", outputStr)

	return nil
}
