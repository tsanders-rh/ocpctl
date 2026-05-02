package api

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
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
	DashboardToken      string                 `json:"dashboard_token,omitempty"`      // Kubernetes Dashboard bearer token
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

// validateOutputFilePath validates that an output file path is safe to access
// Prevents path traversal attacks by ensuring paths are normalized and within allowed directories
func validateOutputFilePath(path string) (string, error) {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(path)

	// Ensure the path is absolute (required for security)
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("output file path must be absolute, got: %s", path)
	}

	// Define allowed base directories for cluster outputs
	// Kubeconfig and credentials are typically stored in:
	// - /tmp/ocpctl/<clusterID>/auth/kubeconfig
	// - /opt/ocpctl/<clusterID>/auth/*
	// - /var/lib/ocpctl/<clusterID>/auth/*
	allowedBaseDirs := []string{
		"/tmp/ocpctl",
		"/opt/ocpctl",
		"/var/lib/ocpctl",
	}

	// Check if the clean path starts with any allowed base directory
	pathAllowed := false
	for _, baseDir := range allowedBaseDirs {
		cleanBase := filepath.Clean(baseDir)
		if strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) || cleanPath == cleanBase {
			pathAllowed = true
			break
		}
	}

	if !pathAllowed {
		return "", fmt.Errorf("output file path is not within allowed directories: %s", path)
	}

	// Additional check: ensure path doesn't contain suspicious patterns
	// even after cleaning (defense in depth)
	if strings.Contains(cleanPath, "..") {
		return "", fmt.Errorf("path contains invalid traversal sequences: %s", path)
	}

	return cleanPath, nil
}

// GetOutputs handles GET /api/v1/clusters/:id/outputs
//
//	@Summary		Get cluster outputs
//	@Description	Returns cluster outputs including kubeconfig, credentials, API URLs, and console URL. Only available for ready clusters.
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	ClusterOutputsResponse
//	@Failure		400	{object}	map[string]string	"Cluster not ready or outputs not available"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/outputs [get]
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
	if outputs.DashboardToken != nil {
		response.DashboardToken = *outputs.DashboardToken
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

		// Validate path to prevent path traversal attacks
		validatedPath, err := validateOutputFilePath(kubeconfigPath)
		if err != nil {
			LogInfo(c, "invalid kubeconfig path blocked", "error", err.Error(), "attempted_path", kubeconfigPath)
		} else {
			LogInfo(c, "reading kubeconfig", "final_path", validatedPath, "length", len(validatedPath))
			if kubeconfigData, err := os.ReadFile(validatedPath); err == nil {
				response.Kubeconfig = string(kubeconfigData)
				LogInfo(c, "kubeconfig read successfully", "size", len(kubeconfigData))
			} else {
				LogInfo(c, "failed to read kubeconfig", "error", err.Error(), "path", validatedPath)
			}
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

		// Validate path to prevent path traversal attacks
		validatedPath, err := validateOutputFilePath(passwordPath)
		if err != nil {
			LogInfo(c, "invalid kubeadmin password path blocked", "error", err.Error(), "attempted_path", passwordPath)
		} else {
			LogInfo(c, "reading kubeadmin password", "path", validatedPath)
			if passwordData, err := os.ReadFile(validatedPath); err == nil {
				response.Kubeadmin = &KubeadminCredentials{
					Username: "kubeadmin",
					Password: string(passwordData),
				}
				LogInfo(c, "kubeadmin password read successfully")
			} else {
				LogInfo(c, "failed to read kubeadmin password", "error", err.Error(), "path", validatedPath)
			}
		}
	}

	// Audit log credential access
	userID, _ := auth.GetUserID(c)
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	auditEvent := &types.AuditEvent{
		ID:              uuid.New().String(),
		Actor:           userID,
		Action:          "ACCESS_CLUSTER_CREDENTIALS",
		TargetClusterID: &cluster.ID,
		Status:          types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"cluster_name":    cluster.Name,
			"has_kubeconfig":  response.Kubeconfig != "",
			"has_credentials": response.Kubeadmin != nil,
		},
		IPAddress: &ipAddress,
		UserAgent: &userAgent,
		CreatedAt: time.Now(),
	}

	// Log audit event (best effort - don't fail request if logging fails)
	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		LogWarning(c, "failed to log audit event", "error", err.Error())
	}

	return SuccessOK(c, response)
}

// DownloadKubeconfig handles GET /api/v1/clusters/:id/kubeconfig
//
//	@Summary		Download kubeconfig
//	@Description	Downloads the kubeconfig file for a ready cluster as a YAML attachment
//	@Tags			Clusters
//	@Accept			json
//	@Produce		application/x-yaml
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{file}		file	"Kubeconfig YAML file"
//	@Failure		400	{object}	map[string]string	"Cluster not ready"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster or kubeconfig not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/kubeconfig [get]
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

	// Validate path to prevent path traversal attacks
	validatedPath, err := validateOutputFilePath(kubeconfigPath)
	if err != nil {
		LogInfo(c, "invalid kubeconfig download path blocked", "error", err.Error(), "attempted_path", kubeconfigPath)
		return ErrorForbidden(c, "Invalid kubeconfig path")
	}

	// Check if kubeconfig exists
	if _, err := os.Stat(validatedPath); os.IsNotExist(err) {
		return ErrorNotFound(c, "Kubeconfig file not found at path")
	}

	// Set headers for file download
	filename := fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Response().Header().Set("Content-Type", "application/x-yaml")

	// Audit log kubeconfig download
	userID, _ := auth.GetUserID(c)
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	auditEvent := &types.AuditEvent{
		ID:              uuid.New().String(),
		Actor:           userID,
		Action:          "DOWNLOAD_KUBECONFIG",
		TargetClusterID: &cluster.ID,
		Status:          types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"cluster_name": cluster.Name,
			"filename":     filename,
		},
		IPAddress: &ipAddress,
		UserAgent: &userAgent,
		CreatedAt: time.Now(),
	}

	// Log audit event (best effort - don't fail download if logging fails)
	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		LogWarning(c, "failed to log audit event", "error", err.Error())
	}

	// Send file
	return c.File(validatedPath)
}

// GetKubeconfigDownloadURL handles GET /api/v1/clusters/:id/kubeconfig/download-url
//
//	@Summary		Get kubeconfig download URL
//	@Description	Returns a pre-signed S3 URL for downloading the kubeconfig (15-minute expiration)
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]string	"Contains download_url field"
//	@Failure		400	{object}	map[string]string	"Cluster not ready or S3 storage not configured"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/kubeconfig/download-url [get]
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

	// Audit log presigned URL generation
	userID, _ := auth.GetUserID(c)
	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	auditEvent := &types.AuditEvent{
		ID:              uuid.New().String(),
		Actor:           userID,
		Action:          "GENERATE_KUBECONFIG_URL",
		TargetClusterID: &cluster.ID,
		Status:          types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"cluster_name": cluster.Name,
			"storage_type": "s3",
			"expires_in":   "15 minutes",
		},
		IPAddress: &ipAddress,
		UserAgent: &userAgent,
		CreatedAt: time.Now(),
	}

	// Log audit event (best effort - don't fail request if logging fails)
	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		LogWarning(c, "failed to log audit event", "error", err.Error())
	}

	return SuccessOK(c, map[string]interface{}{
		"download_url": presignedURL,
		"expires_in":   "15 minutes",
		"filename":     fmt.Sprintf("kubeconfig-%s.yaml", cluster.Name),
		"storage_type": "s3",
	})
}
