package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ConfigureEFSHandler handles EFS storage configuration for a cluster
type ConfigureEFSHandler struct {
	config *Config
	store  *store.Store
}

// NewConfigureEFSHandler creates a new EFS configuration handler
func NewConfigureEFSHandler(config *Config, st *store.Store) *ConfigureEFSHandler {
	return &ConfigureEFSHandler{
		config: config,
		store:  st,
	}
}

// EFSScriptOutput represents the JSON output from configure-efs-storage.sh
type EFSScriptOutput struct {
	EFSID               string  `json:"efs_id"`
	EFSSecurityGroupID  string  `json:"efs_security_group_id"`
	Region              string  `json:"region"`
	StorageClass        string  `json:"storage_class"`
	AuthMode            *string `json:"auth_mode,omitempty"`
	IAMRoleARN          *string `json:"iam_role_arn,omitempty"`
}

// Handle configures EFS storage for a cluster
func (h *ConfigureEFSHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Configuring EFS storage for cluster %s", cluster.Name)

	// Verify cluster is READY - if not, return NotReadyError to defer job
	if cluster.Status != types.ClusterStatusReady {
		return &types.NotReadyError{
			Resource: "cluster",
			Current:  string(cluster.Status),
			Required: string(types.ClusterStatusReady),
		}
	}

	// Get kubeconfig path from cluster workdir
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Execute configure-efs-storage.sh script
	scriptPath := "scripts/configure-efs-storage.sh"
	log.Printf("Executing EFS configuration script: %s", scriptPath)

	cmd := exec.CommandContext(ctx, "bash", scriptPath, cluster.Name, kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("EFS configuration failed: %v\nOutput: %s", err, string(output))
		return fmt.Errorf("execute EFS script: %w", err)
	}

	log.Printf("EFS script output:\n%s", string(output))

	// Parse JSON output from script
	outputStr := string(output)
	startMarker := "OCPCTL_OUTPUT_START"
	endMarker := "OCPCTL_OUTPUT_END"

	startIdx := strings.Index(outputStr, startMarker)
	endIdx := strings.Index(outputStr, endMarker)

	if startIdx == -1 || endIdx == -1 {
		return fmt.Errorf("failed to find JSON output markers in script output")
	}

	jsonStr := outputStr[startIdx+len(startMarker):endIdx]
	jsonStr = strings.TrimSpace(jsonStr)

	var scriptOutput EFSScriptOutput
	if err := json.Unmarshal([]byte(jsonStr), &scriptOutput); err != nil {
		return fmt.Errorf("parse EFS script output: %w", err)
	}

	log.Printf("Parsed EFS output: EFS ID=%s, Security Group=%s, Auth Mode=%v",
		scriptOutput.EFSID, scriptOutput.EFSSecurityGroupID, scriptOutput.AuthMode)

	// Update cluster's storage_config
	storageConfig := types.StorageConfig{
		EFSEnabled:   true,
		LocalEFSID:   &scriptOutput.EFSID,
		LocalEFSSGID: &scriptOutput.EFSSecurityGroupID,
		AuthMode:     scriptOutput.AuthMode,
		IAMRoleARN:   scriptOutput.IAMRoleARN,
	}

	// Convert to JSON for database update
	configJSON, err := json.Marshal(storageConfig)
	if err != nil {
		return fmt.Errorf("marshal storage config: %w", err)
	}

	// Update cluster storage_config column via transaction
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

	_, err = tx.Exec(ctx, query, configJSON, cluster.ID)
	if err != nil {
		return fmt.Errorf("update cluster storage_config: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	log.Printf("Successfully configured EFS storage for cluster %s", cluster.Name)
	return nil
}
