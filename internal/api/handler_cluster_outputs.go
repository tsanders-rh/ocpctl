package api

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/s3"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterOutputsResponse represents the cluster outputs (kubeconfig, credentials, etc.)
type ClusterOutputsResponse struct {
	ClusterID           string                 `json:"cluster_id"`
	ClusterName         string                 `json:"cluster_name"`
	Status              string                 `json:"status"`
	APIUrl              string                 `json:"api_url,omitempty"`
	ConsoleURL          string                 `json:"console_url,omitempty"`
	Kubeconfig          string                 `json:"kubeconfig,omitempty"`           // Full kubeconfig content
	KubeconfigS3URI     string                 `json:"kubeconfig_s3_uri,omitempty"`    // S3 URI to kubeconfig file
	Kubeadmin           *KubeadminCredentials  `json:"kubeadmin,omitempty"`            // Actual credentials
	KubeadminSecretRef  string                 `json:"kubeadmin_secret_ref,omitempty"` // Reference to secret location
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Check if cluster is ready
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster is not ready (status: %s). Outputs are only available for ready clusters.", cluster.Status))
	}

	// Get outputs from database
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, id)
	if err != nil {
		return ErrorBadRequest(c, "Cluster outputs are not yet available. The cluster may still be provisioning or outputs may have been cleaned up.")
	}

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

	// Use outputs from database
	if outputs.APIURL != nil {
		response.APIUrl = *outputs.APIURL
	}
	if outputs.ConsoleURL != nil {
		response.ConsoleURL = *outputs.ConsoleURL
	}
	if outputs.KubeconfigS3URI != nil {
		response.KubeconfigS3URI = *outputs.KubeconfigS3URI
	}
	if outputs.KubeadminSecretRef != nil {
		response.KubeadminSecretRef = *outputs.KubeadminSecretRef
	}

	// Read kubeconfig from disk if path is available
	if outputs.KubeconfigS3URI != nil && *outputs.KubeconfigS3URI != "" {
		// Extract file path from file:// URI
		kubeconfigPath := *outputs.KubeconfigS3URI
		LogInfo(c, "kubeconfig URI from DB", "uri", kubeconfigPath, "has_prefix", strings.HasPrefix(kubeconfigPath, "file://"))
		if strings.HasPrefix(kubeconfigPath, "file://") {
			kubeconfigPath = kubeconfigPath[7:] // Remove "file://" prefix
		}
		LogInfo(c, "reading kubeconfig", "final_path", kubeconfigPath, "length", len(kubeconfigPath))
		if kubeconfigData, err := os.ReadFile(kubeconfigPath); err == nil {
			response.Kubeconfig = string(kubeconfigData)
			LogInfo(c, "kubeconfig read successfully", "size", len(kubeconfigData))
		} else {
			LogInfo(c, "failed to read kubeconfig", "error", err.Error(), "path", kubeconfigPath)
		}
	} else {
		LogInfo(c, "no kubeconfig URI in outputs")
	}

	// Read kubeadmin password from disk if path is available
	if outputs.KubeadminSecretRef != nil && *outputs.KubeadminSecretRef != "" {
		// Extract file path from file:// URI
		passwordPath := *outputs.KubeadminSecretRef
		if strings.HasPrefix(passwordPath, "file://") {
			passwordPath = passwordPath[7:] // Remove "file://" prefix
		}
		LogInfo(c, "reading kubeadmin password", "path", passwordPath)
		if passwordData, err := os.ReadFile(passwordPath); err == nil {
			response.Kubeadmin = &KubeadminCredentials{
				Username: "kubeadmin",
				Password: string(passwordData),
			}
			LogInfo(c, "kubeadmin password read successfully")
		} else {
			LogInfo(c, "failed to read kubeadmin password", "error", err.Error())
		}
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Check if cluster is ready
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster is not ready (status: %s)", cluster.Status))
	}

	// Get kubeconfig path from database
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, id)
	if err != nil {
		return ErrorNotFound(c, "Cluster outputs not found")
	}

	if outputs.KubeconfigS3URI == nil || *outputs.KubeconfigS3URI == "" {
		return ErrorNotFound(c, "Kubeconfig not available for this cluster")
	}

	// Extract file path from file:// URI
	kubeconfigPath := *outputs.KubeconfigS3URI
	if strings.HasPrefix(kubeconfigPath, "file://") {
		kubeconfigPath = kubeconfigPath[7:] // Remove "file://" prefix
	}

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return ErrorNotFound(c, "Kubeconfig file not found at path")
	}

	// Set headers for file download
	filename := fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Response().Header().Set("Content-Type", "application/x-yaml")

	// Send file
	return c.File(kubeconfigPath)
}

// GetKubeconfigDownloadURL handles GET /api/v1/clusters/:id/kubeconfig/download-url
// Returns a pre-signed S3 URL for downloading the kubeconfig
func (h *ClusterHandler) GetKubeconfigDownloadURL(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorNotFound(c, "Cluster not found")
		}
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Check if cluster is ready
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster is not ready (status: %s)", cluster.Status))
	}

	// Get cluster outputs
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, id)
	if err != nil {
		return ErrorNotFound(c, "Cluster outputs not found")
	}

	LogInfo(c, "GetKubeconfigDownloadURL called",
		"cluster_id", cluster.ID)

	// Check if S3 URI is available
	if outputs.KubeconfigS3URI == nil || *outputs.KubeconfigS3URI == "" {
		LogInfo(c, "no kubeconfig URI found")
		return ErrorNotFound(c, "Kubeconfig S3 URI not available for this cluster")
	}

	kubeconfigURI := *outputs.KubeconfigS3URI

	prefix := kubeconfigURI
	if len(kubeconfigURI) > 20 {
		prefix = kubeconfigURI[:20]
	}

	LogInfo(c, "checking kubeconfig URI type",
		"uri", kubeconfigURI,
		"len", len(kubeconfigURI),
		"hasFilePrefix", strings.HasPrefix(kubeconfigURI, "file://"),
		"prefix", prefix)

	// Check if this is a file:// URI (IBM Cloud, local storage)
	// For file:// URIs, return the direct API download endpoint instead of S3 presigned URL
	if strings.HasPrefix(kubeconfigURI, "file://") {
		// Construct direct download URL
		baseURL := c.Scheme() + "://" + c.Request().Host
		directDownloadURL := fmt.Sprintf("%s/api/v1/clusters/%s/kubeconfig", baseURL, cluster.ID)

		LogInfo(c, "kubeconfig download URL generated (direct API)",
			"cluster_id", cluster.ID,
			"cluster_name", cluster.Name,
			"storage", "local")

		return SuccessOK(c, map[string]interface{}{
			"download_url": directDownloadURL,
			"expires_in":   "session-based",
			"filename":     fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name),
			"storage_type": "local",
		})
	}

	// S3 URI - generate presigned URL
	s3Client, err := s3.NewClient(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create S3 client: %w", err))
	}

	// Generate presigned URL (valid for 15 minutes)
	presignedURL, err := s3Client.GeneratePresignedURL(ctx, kubeconfigURI, 15)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to generate presigned URL: %w", err))
	}

	// Log the download request
	LogInfo(c, "kubeconfig download URL generated (S3 presigned)",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"storage", "s3")

	return SuccessOK(c, map[string]interface{}{
		"download_url": presignedURL,
		"expires_in":   "15 minutes",
		"filename":     fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name),
		"storage_type": "s3",
	})
}
