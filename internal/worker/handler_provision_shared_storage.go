package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ProvisionSharedStorageHandler handles shared storage provisioning between two clusters
type ProvisionSharedStorageHandler struct {
	config *Config
	store  *store.Store
}

// NewProvisionSharedStorageHandler creates a new shared storage provisioning handler
func NewProvisionSharedStorageHandler(config *Config, st *store.Store) *ProvisionSharedStorageHandler {
	return &ProvisionSharedStorageHandler{
		config: config,
		store:  st,
	}
}

// SharedStorageScriptOutput represents the JSON output from configure-shared-migration-storage.sh
type SharedStorageScriptOutput struct {
	EFSID               string `json:"efs_id"`
	EFSAccessPointID    string `json:"efs_access_point_id"`
	EFSSecurityGroupID  string `json:"efs_security_group_id"`
	S3Bucket            string `json:"s3_bucket"`
	Region              string `json:"region"`
	SourceCluster       string `json:"source_cluster"`
	TargetCluster       string `json:"target_cluster"`
}

// Handle provisions shared storage between two clusters
func (h *ProvisionSharedStorageHandler) Handle(ctx context.Context, job *types.Job) error {
	// Extract target cluster ID from job metadata
	targetClusterIDRaw, ok := job.Metadata["target_cluster_id"]
	if !ok {
		return fmt.Errorf("target_cluster_id not found in job metadata")
	}

	targetClusterID, ok := targetClusterIDRaw.(string)
	if !ok {
		return fmt.Errorf("target_cluster_id is not a string")
	}

	// Get source cluster (the one referenced in job.ClusterID)
	sourceCluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get source cluster: %w", err)
	}

	// Get target cluster
	targetCluster, err := h.store.Clusters.GetByID(ctx, targetClusterID)
	if err != nil {
		return fmt.Errorf("get target cluster: %w", err)
	}

	log.Printf("Provisioning shared storage between %s and %s",
		sourceCluster.Name, targetCluster.Name)

	// Validate both clusters are READY
	if sourceCluster.Status != types.ClusterStatusReady {
		return fmt.Errorf("source cluster must be READY, current state: %s", sourceCluster.Status)
	}
	if targetCluster.Status != types.ClusterStatusReady {
		return fmt.Errorf("target cluster must be READY, current state: %s", targetCluster.Status)
	}

	// Validate same region
	if sourceCluster.Region != targetCluster.Region {
		return fmt.Errorf("clusters must be in same region: source=%s, target=%s",
			sourceCluster.Region, targetCluster.Region)
	}

	// Create storage group with PROVISIONING status
	storageGroupID := uuid.New().String()
	storageGroupName := fmt.Sprintf("%s-%s-migration", sourceCluster.Name, targetCluster.Name)

	storageGroup := &types.StorageGroup{
		ID:        storageGroupID,
		Name:      storageGroupName,
		Region:    sourceCluster.Region,
		Status:    types.StorageGroupStatusProvisioning,
		Metadata:  types.StorageGroupMetadata{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.StorageGroups.Create(ctx, storageGroup); err != nil {
		return fmt.Errorf("create storage group: %w", err)
	}

	log.Printf("Created storage group: %s", storageGroupID)

	// Execute configure-shared-migration-storage.sh script
	scriptPath := "scripts/configure-shared-migration-storage.sh"
	log.Printf("Executing shared storage script: %s", scriptPath)

	cmd := exec.CommandContext(ctx, "bash", scriptPath,
		sourceCluster.Name,
		targetCluster.Name,
		sourceCluster.Region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Shared storage script failed: %v\nOutput: %s", err, string(output))

		// Update storage group status to FAILED
		_ = h.store.StorageGroups.UpdateStatus(ctx, storageGroupID, types.StorageGroupStatusFailed)

		return fmt.Errorf("execute shared storage script: %w", err)
	}

	log.Printf("Shared storage script output:\n%s", string(output))

	// Parse JSON output from script
	outputStr := string(output)
	startMarker := "OCPCTL_OUTPUT_START"
	endMarker := "OCPCTL_OUTPUT_END"

	startIdx := strings.Index(outputStr, startMarker)
	endIdx := strings.Index(outputStr, endMarker)

	if startIdx == -1 || endIdx == -1 {
		_ = h.store.StorageGroups.UpdateStatus(ctx, storageGroupID, types.StorageGroupStatusFailed)
		return fmt.Errorf("failed to find JSON output markers in script output")
	}

	jsonStr := outputStr[startIdx+len(startMarker):endIdx]
	jsonStr = strings.TrimSpace(jsonStr)

	var scriptOutput SharedStorageScriptOutput
	if err := json.Unmarshal([]byte(jsonStr), &scriptOutput); err != nil {
		_ = h.store.StorageGroups.UpdateStatus(ctx, storageGroupID, types.StorageGroupStatusFailed)
		return fmt.Errorf("parse shared storage script output: %w", err)
	}

	log.Printf("Parsed shared storage output: EFS=%s, S3=%s",
		scriptOutput.EFSID, scriptOutput.S3Bucket)

	// Update storage group with AWS resource IDs
	storageGroup.EFSID = &scriptOutput.EFSID
	storageGroup.EFSSecurityGroupID = &scriptOutput.EFSSecurityGroupID
	storageGroup.S3Bucket = &scriptOutput.S3Bucket
	storageGroup.Status = types.StorageGroupStatusReady
	storageGroup.UpdatedAt = time.Now()

	// Store access point in metadata
	storageGroup.Metadata["efs_access_point_id"] = scriptOutput.EFSAccessPointID

	if err := h.store.StorageGroups.Update(ctx, storageGroup); err != nil {
		return fmt.Errorf("update storage group: %w", err)
	}

	// Create cluster storage links for both clusters
	sourceLink := &types.ClusterStorageLink{
		ID:             uuid.New().String(),
		ClusterID:      sourceCluster.ID,
		StorageGroupID: storageGroupID,
		Role:           types.ClusterStorageLinkRoleSource,
		CreatedAt:      time.Now(),
	}

	targetLink := &types.ClusterStorageLink{
		ID:             uuid.New().String(),
		ClusterID:      targetCluster.ID,
		StorageGroupID: storageGroupID,
		Role:           types.ClusterStorageLinkRoleTarget,
		CreatedAt:      time.Now(),
	}

	if err := h.store.ClusterStorageLinks.Create(ctx, sourceLink); err != nil {
		return fmt.Errorf("create source cluster link: %w", err)
	}

	if err := h.store.ClusterStorageLinks.Create(ctx, targetLink); err != nil {
		return fmt.Errorf("create target cluster link: %w", err)
	}

	log.Printf("Created cluster storage links for both clusters")

	// Update both clusters' storage_config
	if err := h.updateClusterStorageConfig(ctx, sourceCluster.ID, storageGroupID,
		scriptOutput.EFSID, scriptOutput.S3Bucket); err != nil {
		return fmt.Errorf("update source cluster storage config: %w", err)
	}

	if err := h.updateClusterStorageConfig(ctx, targetCluster.ID, storageGroupID,
		scriptOutput.EFSID, scriptOutput.S3Bucket); err != nil {
		return fmt.Errorf("update target cluster storage config: %w", err)
	}

	log.Printf("Successfully provisioned shared storage between %s and %s",
		sourceCluster.Name, targetCluster.Name)

	return nil
}

// updateClusterStorageConfig updates a cluster's storage_config with shared storage info
func (h *ProvisionSharedStorageHandler) updateClusterStorageConfig(
	ctx context.Context,
	clusterID string,
	storageGroupID string,
	efsID string,
	s3Bucket string,
) error {
	// Get current cluster to merge with existing storage_config
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	// Parse existing storage_config
	var storageConfig types.StorageConfig
	if cluster.StorageConfig != nil {
		storageConfig = *cluster.StorageConfig
	}

	// Add shared storage info
	storageConfig.SharedEFSID = &efsID
	storageConfig.SharedS3Bucket = &s3Bucket
	storageConfig.StorageGroupID = &storageGroupID

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
