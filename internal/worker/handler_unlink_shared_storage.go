package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// UnlinkSharedStorageHandler handles unlinking a cluster from shared storage
type UnlinkSharedStorageHandler struct {
	config *Config
	store  *store.Store
}

// NewUnlinkSharedStorageHandler creates a new unlink shared storage handler
func NewUnlinkSharedStorageHandler(config *Config, st *store.Store) *UnlinkSharedStorageHandler {
	return &UnlinkSharedStorageHandler{
		config: config,
		store:  st,
	}
}

// Handle unlinks a cluster from shared storage and cleans up if last link
func (h *UnlinkSharedStorageHandler) Handle(ctx context.Context, job *types.Job) error {
	// Extract storage group ID from job metadata
	storageGroupIDRaw, ok := job.Metadata["storage_group_id"]
	if !ok {
		return fmt.Errorf("storage_group_id not found in job metadata")
	}

	storageGroupID, ok := storageGroupIDRaw.(string)
	if !ok {
		return fmt.Errorf("storage_group_id is not a string")
	}

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Unlinking cluster %s from storage group %s", cluster.Name, storageGroupID)

	// Get storage group
	storageGroup, err := h.store.StorageGroups.GetByID(ctx, storageGroupID)
	if err != nil {
		return fmt.Errorf("get storage group: %w", err)
	}

	// Delete cluster storage link
	if err := h.store.ClusterStorageLinks.Delete(ctx, cluster.ID, storageGroupID); err != nil {
		return fmt.Errorf("delete cluster storage link: %w", err)
	}

	log.Printf("Deleted cluster storage link for cluster %s", cluster.Name)

	// Update cluster's storage_config to remove shared storage info
	if err := h.updateClusterStorageConfig(ctx, cluster.ID); err != nil {
		return fmt.Errorf("update cluster storage config: %w", err)
	}

	// Check if any other clusters are linked to this storage group
	remainingLinks, err := h.store.ClusterStorageLinks.CountByStorageGroupID(ctx, storageGroupID)
	if err != nil {
		return fmt.Errorf("count remaining links: %w", err)
	}

	log.Printf("Remaining links for storage group %s: %d", storageGroupID, remainingLinks)

	// If no links remain, perform automatic cleanup
	if remainingLinks == 0 {
		log.Printf("No remaining links - performing automatic cleanup of AWS resources")

		// Update storage group status to DELETING
		if err := h.store.StorageGroups.UpdateStatus(ctx, storageGroupID, types.StorageGroupStatusDeleting); err != nil {
			log.Printf("Warning: failed to update storage group status: %v", err)
		}

		// Delete AWS resources
		if err := h.cleanupAWSResources(ctx, storageGroup); err != nil {
			log.Printf("Error cleaning up AWS resources: %v", err)
			// Continue to delete database record even if AWS cleanup fails
		}

		// Delete storage group record from database
		if err := h.store.StorageGroups.Delete(ctx, storageGroupID); err != nil {
			return fmt.Errorf("delete storage group: %w", err)
		}

		log.Printf("Successfully deleted storage group %s and all AWS resources", storageGroupID)
	} else {
		log.Printf("Storage group %s still has %d linked cluster(s) - skipping cleanup", storageGroupID, remainingLinks)
	}

	return nil
}

// updateClusterStorageConfig removes shared storage info from cluster's storage_config
func (h *UnlinkSharedStorageHandler) updateClusterStorageConfig(ctx context.Context, clusterID string) error {
	// Get current cluster
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	// Parse existing storage_config
	var storageConfig types.StorageConfig
	if cluster.StorageConfig != nil {
		storageConfig = *cluster.StorageConfig
	}

	// Remove shared storage info
	storageConfig.SharedEFSID = nil
	storageConfig.SharedS3Bucket = nil
	storageConfig.StorageGroupID = nil

	// Convert to JSON
	configJSON, err := json.Marshal(storageConfig)
	if err != nil {
		return fmt.Errorf("marshal storage config: %w", err)
	}

	// Update cluster
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		UPDATE clusters
		SET storage_config = $1, updated_at = NOW()
		WHERE id = $2
	`

	_, err = tx.Exec(ctx, query, configJSON, clusterID)
	if err != nil {
		return fmt.Errorf("update cluster storage_config: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// cleanupAWSResources deletes EFS and S3 resources for a storage group
func (h *UnlinkSharedStorageHandler) cleanupAWSResources(ctx context.Context, storageGroup *types.StorageGroup) error {
	region := storageGroup.Region

	// Delete EFS mount targets first
	if storageGroup.EFSID != nil && *storageGroup.EFSID != "" {
		log.Printf("Deleting EFS mount targets for %s", *storageGroup.EFSID)

		// List mount targets
		cmd := exec.CommandContext(ctx, "aws", "efs", "describe-mount-targets",
			"--region", region,
			"--file-system-id", *storageGroup.EFSID,
			"--query", "MountTargets[*].MountTargetId",
			"--output", "text")

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Warning: failed to list mount targets: %v", err)
		} else {
			mountTargets := string(output)
			if mountTargets != "" {
				// Delete each mount target
				for _, mtID := range splitWhitespace(mountTargets) {
					if mtID == "" {
						continue
					}
					log.Printf("Deleting mount target: %s", mtID)
					deleteCmd := exec.CommandContext(ctx, "aws", "efs", "delete-mount-target",
						"--region", region,
						"--mount-target-id", mtID)
					if output, err := deleteCmd.CombinedOutput(); err != nil {
						log.Printf("Warning: failed to delete mount target %s: %v\nOutput: %s", mtID, err, string(output))
					}
				}

				// Wait for mount targets to be deleted (polling with Go, no shell interpolation)
				log.Printf("Waiting for mount targets to be deleted...")
				if err := h.waitForMountTargetsDeletion(ctx, region, *storageGroup.EFSID); err != nil {
					log.Printf("Warning: timeout waiting for mount targets to be deleted: %v", err)
				}
			}
		}

		// Delete EFS file system
		log.Printf("Deleting EFS file system: %s", *storageGroup.EFSID)
		deleteEFSCmd := exec.CommandContext(ctx, "aws", "efs", "delete-file-system",
			"--region", region,
			"--file-system-id", *storageGroup.EFSID)

		if output, err := deleteEFSCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to delete EFS: %v\nOutput: %s", err, string(output))
		} else {
			log.Printf("Successfully deleted EFS file system")
		}
	}

	// Delete S3 bucket (force delete all objects first)
	if storageGroup.S3Bucket != nil && *storageGroup.S3Bucket != "" {
		log.Printf("Deleting S3 bucket: %s", *storageGroup.S3Bucket)

		// Delete all objects in bucket first
		deleteObjectsCmd := exec.CommandContext(ctx, "aws", "s3", "rm",
			fmt.Sprintf("s3://%s", *storageGroup.S3Bucket),
			"--recursive",
			"--region", region)

		if output, err := deleteObjectsCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to delete S3 objects: %v\nOutput: %s", err, string(output))
		}

		// Delete bucket
		deleteBucketCmd := exec.CommandContext(ctx, "aws", "s3", "rb",
			fmt.Sprintf("s3://%s", *storageGroup.S3Bucket),
			"--region", region)

		if output, err := deleteBucketCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to delete S3 bucket: %v\nOutput: %s", err, string(output))
		} else {
			log.Printf("Successfully deleted S3 bucket")
		}
	}

	// Delete security group
	if storageGroup.EFSSecurityGroupID != nil && *storageGroup.EFSSecurityGroupID != "" {
		log.Printf("Deleting security group: %s", *storageGroup.EFSSecurityGroupID)

		deleteSGCmd := exec.CommandContext(ctx, "aws", "ec2", "delete-security-group",
			"--region", region,
			"--group-id", *storageGroup.EFSSecurityGroupID)

		if output, err := deleteSGCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to delete security group: %v\nOutput: %s", err, string(output))
		} else {
			log.Printf("Successfully deleted security group")
		}
	}

	return nil
}

// waitForMountTargetsDeletion polls AWS to wait for all mount targets to be deleted
// Uses proper exec.CommandContext with argument arrays to prevent command injection
func (h *UnlinkSharedStorageHandler) waitForMountTargetsDeletion(ctx context.Context, region, efsID string) error {
	maxAttempts := 30
	pollInterval := 10 * time.Second

	for i := 0; i < maxAttempts; i++ {
		// Use exec.CommandContext with array args - NOT shell interpolation
		cmd := exec.CommandContext(ctx, "aws", "efs", "describe-mount-targets",
			"--region", region,
			"--file-system-id", efsID,
			"--query", "length(MountTargets)",
			"--output", "text")

		output, err := cmd.CombinedOutput()
		if err != nil {
			// Mount targets might already be deleted, which is fine
			log.Printf("Attempt %d/%d: Error checking mount targets (may be deleted): %v", i+1, maxAttempts, err)
			return nil
		}

		// Parse the count
		countStr := strings.TrimSpace(string(output))
		count, err := strconv.Atoi(countStr)
		if err != nil {
			log.Printf("Attempt %d/%d: Could not parse mount target count: %v", i+1, maxAttempts, err)
			// Assume 0 if we can't parse
			return nil
		}

		if count == 0 {
			log.Printf("All mount targets deleted after %d attempts", i+1)
			return nil
		}

		log.Printf("Attempt %d/%d: %d mount targets still exist, waiting %v...", i+1, maxAttempts, count, pollInterval)

		// Context-aware sleep
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue to next iteration
		}
	}

	return fmt.Errorf("timeout after %d attempts waiting for mount targets to be deleted", maxAttempts)
}

// splitWhitespace splits a string by whitespace
func splitWhitespace(s string) []string {
	var result []string
	current := ""
	for _, char := range s {
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
