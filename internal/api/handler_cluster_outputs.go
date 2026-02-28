package api

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterOutputsResponse represents the cluster outputs (kubeconfig, credentials, etc.)
type ClusterOutputsResponse struct {
	ClusterID   string                 `json:"cluster_id"`
	ClusterName string                 `json:"cluster_name"`
	Status      string                 `json:"status"`
	APIUrl      string                 `json:"api_url,omitempty"`
	ConsoleURL  string                 `json:"console_url,omitempty"`
	Kubeconfig  string                 `json:"kubeconfig,omitempty"`
	Kubeadmin   *KubeadminCredentials  `json:"kubeadmin,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// KubeadminCredentials holds the kubeadmin username and password
type KubeadminCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GetOutputs handles GET /api/v1/clusters/:id/outputs
// Returns cluster outputs including kubeconfig, credentials, and URLs
func (h *ClusterHandler) GetOutputs(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorNotFound(c, "Cluster not found")
		}
		return ErrorInternal(c, "Failed to retrieve cluster: "+err.Error())
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Check if cluster is ready
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster is not ready (status: %s). Outputs are only available for ready clusters.", cluster.Status))
	}

	// Get work directory for this cluster
	workDir := os.Getenv("WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/ocpctl"
	}
	clusterWorkDir := filepath.Join(workDir, cluster.ID)

	// Build response
	response := &ClusterOutputsResponse{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		Status:      string(cluster.Status),
		Metadata: map[string]interface{}{
			"platform":     cluster.Platform,
			"region":       cluster.Region,
			"version":      cluster.Version,
			"base_domain":  cluster.BaseDomain,
			"profile":      cluster.Profile,
			"created_at":   cluster.CreatedAt,
			"destroy_at":   cluster.DestroyAt,
		},
	}

	// Construct URLs
	if cluster.BaseDomain != "" {
		response.APIUrl = fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, cluster.BaseDomain)
		response.ConsoleURL = fmt.Sprintf("https://console-openshift-console.apps.%s.%s", cluster.Name, cluster.BaseDomain)
	}

	// Read kubeconfig if it exists
	kubeconfigPath := filepath.Join(clusterWorkDir, "auth", "kubeconfig")
	if kubeconfigData, err := os.ReadFile(kubeconfigPath); err == nil {
		response.Kubeconfig = string(kubeconfigData)
	}

	// Read kubeadmin password if it exists
	kubeadminPasswordPath := filepath.Join(clusterWorkDir, "auth", "kubeadmin-password")
	if passwordData, err := os.ReadFile(kubeadminPasswordPath); err == nil {
		response.Kubeadmin = &KubeadminCredentials{
			Username: "kubeadmin",
			Password: string(passwordData),
		}
	}

	// If no outputs are available yet, inform the user
	if response.Kubeconfig == "" && response.Kubeadmin == nil {
		return ErrorBadRequest(c, "Cluster outputs are not yet available. The cluster may still be provisioning or outputs may have been cleaned up.")
	}

	return SuccessOK(c, response)
}

// DownloadKubeconfig handles GET /api/v1/clusters/:id/kubeconfig
// Returns the kubeconfig file as a downloadable attachment
func (h *ClusterHandler) DownloadKubeconfig(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorNotFound(c, "Cluster not found")
		}
		return ErrorInternal(c, "Failed to retrieve cluster: "+err.Error())
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Check if cluster is ready
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster is not ready (status: %s)", cluster.Status))
	}

	// Get work directory
	workDir := os.Getenv("WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/ocpctl"
	}
	kubeconfigPath := filepath.Join(workDir, cluster.ID, "auth", "kubeconfig")

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return ErrorNotFound(c, "Kubeconfig not found for this cluster")
	}

	// Set headers for file download
	filename := fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Response().Header().Set("Content-Type", "application/x-yaml")

	// Send file
	return c.File(kubeconfigPath)
}
