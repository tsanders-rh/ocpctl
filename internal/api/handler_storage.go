package api

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// StorageHandler handles storage-related API endpoints
type StorageHandler struct {
	store  *store.Store
	policy *policy.Engine
}

// NewStorageHandler creates a new storage handler
func NewStorageHandler(s *store.Store, p *policy.Engine) *StorageHandler {
	return &StorageHandler{
		store:  s,
		policy: p,
	}
}

// LinkStorageRequest represents the request to link storage between clusters
type LinkStorageRequest struct {
	TargetClusterID string `json:"target_cluster_id" validate:"required"`
}

// StorageGroupResponse represents the response for a storage group
type StorageGroupResponse struct {
	ID                 string                       `json:"id"`
	Name               string                       `json:"name"`
	EFSID              *string                      `json:"efs_id,omitempty"`
	EFSSecurityGroupID *string                      `json:"efs_security_group_id,omitempty"`
	S3Bucket           *string                      `json:"s3_bucket,omitempty"`
	Region             string                       `json:"region"`
	Status             types.StorageGroupStatus     `json:"status"`
	LinkedClusters     []ClusterStorageLinkResponse `json:"linked_clusters"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

// ClusterStorageLinkResponse represents a cluster linked to a storage group
type ClusterStorageLinkResponse struct {
	ClusterID   string                      `json:"cluster_id"`
	ClusterName string                      `json:"cluster_name"`
	Role        types.ClusterStorageLinkRole `json:"role"`
	LinkedAt    time.Time                   `json:"linked_at"`
}

// LinkToCluster handles POST /api/v1/clusters/:id/storage/link
//
//	@Summary		Link storage to cluster
//	@Description	Links persistent storage from another cluster to this cluster
//	@Tags			Storage
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Cluster ID"
//	@Param			body	body		LinkStorageRequest	true	"Link storage request"
//	@Success		200		{object}	StorageGroupResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		403		{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404		{object}	map[string]string	"Cluster not found"
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/storage/link [post]
func (h *StorageHandler) LinkToCluster(c echo.Context) error {
	clusterID := c.Param("id")

	var req LinkStorageRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	// Validate request
	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get source cluster
	sourceCluster, err := h.store.Clusters.GetByID(c.Request().Context(), clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Check access to source cluster
	if err := h.checkClusterAccess(c, sourceCluster); err != nil {
		return err
	}

	// Get target cluster
	targetCluster, err := h.store.Clusters.GetByID(c.Request().Context(), req.TargetClusterID)
	if err != nil {
		return ErrorNotFound(c, "Target cluster not found")
	}

	// Check access to target cluster
	if err := h.checkClusterAccess(c, targetCluster); err != nil {
		return err
	}

	// Validate clusters
	if sourceCluster.ID == targetCluster.ID {
		return ErrorBadRequest(c, "Cannot link cluster to itself")
	}

	if sourceCluster.Region != targetCluster.Region {
		return ErrorBadRequest(c, fmt.Sprintf("Clusters must be in same region: source=%s, target=%s",
			sourceCluster.Region, targetCluster.Region))
	}

	if sourceCluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Source cluster must be READY, current state: %s", sourceCluster.Status))
	}

	if targetCluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Target cluster must be READY, current state: %s", targetCluster.Status))
	}

	// Check if clusters are already linked
	existingLinks, err := h.store.ClusterStorageLinks.GetByClusterID(c.Request().Context(), sourceCluster.ID)
	if err == nil {
		for _, link := range existingLinks {
			// Get the storage group to check if target cluster is already linked
			linkedClusters, err := h.store.ClusterStorageLinks.GetByStorageGroupID(c.Request().Context(), link.StorageGroupID)
			if err == nil {
				for _, linkedCluster := range linkedClusters {
					if linkedCluster.ClusterID == targetCluster.ID {
						// Already linked - return existing storage group (idempotent)
						storageGroup, err := h.store.StorageGroups.GetByID(c.Request().Context(), link.StorageGroupID)
						if err == nil {
							return c.JSON(200, h.buildStorageGroupResponse(c, storageGroup))
						}
					}
				}
			}
		}
	}

	// Create provision job
	jobID := uuid.New().String()
	job := &types.Job{
		ID:          jobID,
		ClusterID:   sourceCluster.ID,
		JobType:     types.JobTypeProvisionSharedStorage,
		Status:      types.JobStatusPending,
		MaxAttempts: 3,
		Attempt:     0,
		Metadata: types.JobMetadata{
			"target_cluster_id": targetCluster.ID,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.Jobs.Create(c.Request().Context(), job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create provision job: %w", err))
	}

	// Return 202 Accepted with job ID
	return c.JSON(202, map[string]interface{}{
		"message":          "Shared storage provisioning initiated",
		"job_id":           jobID,
		"source_cluster":   sourceCluster.Name,
		"target_cluster":   targetCluster.Name,
		"estimated_time":   "5-10 minutes",
	})
}

// GetStorage handles GET /api/v1/clusters/:id/storage
//
//	@Summary		Get cluster storage
//	@Description	Returns all storage groups linked to this cluster
//	@Tags			Storage
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{array}		StorageGroupResponse
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/storage [get]
func (h *StorageHandler) GetStorage(c echo.Context) error {
	clusterID := c.Param("id")

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(c.Request().Context(), clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Get all storage links for this cluster
	links, err := h.store.ClusterStorageLinks.GetByClusterID(c.Request().Context(), clusterID)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get storage links: %w", err))
	}

	// Build response for each storage group
	responses := []StorageGroupResponse{}
	for _, link := range links {
		storageGroup, err := h.store.StorageGroups.GetByID(c.Request().Context(), link.StorageGroupID)
		if err != nil {
			continue // Skip if storage group not found
		}

		response := h.buildStorageGroupResponse(c, storageGroup)
		responses = append(responses, response)
	}

	return c.JSON(200, responses)
}

// UnlinkStorage handles DELETE /api/v1/clusters/:id/storage/link/:group_id
//
//	@Summary		Unlink storage from cluster
//	@Description	Removes the storage group link from this cluster
//	@Tags			Storage
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string	true	"Cluster ID"
//	@Param			group_id	path		string	true	"Storage Group ID"
//	@Success		200			{object}	map[string]string
//	@Failure		403			{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404			{object}	map[string]string	"Cluster or storage link not found"
//	@Failure		500			{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/storage/link/{group_id} [delete]
func (h *StorageHandler) UnlinkStorage(c echo.Context) error {
	clusterID := c.Param("id")
	storageGroupID := c.Param("group_id")

	// Get cluster
	cluster, err := h.store.Clusters.GetByID(c.Request().Context(), clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Verify link exists
	_, err = h.store.ClusterStorageLinks.GetByClusterAndGroup(c.Request().Context(), clusterID, storageGroupID)
	if err != nil {
		return ErrorNotFound(c, "Storage link not found")
	}

	// Create unlink job
	jobID := uuid.New().String()
	job := &types.Job{
		ID:          jobID,
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeUnlinkSharedStorage,
		Status:      types.JobStatusPending,
		MaxAttempts: 3,
		Attempt:     0,
		Metadata: types.JobMetadata{
			"storage_group_id": storageGroupID,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.Jobs.Create(c.Request().Context(), job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create unlink job: %w", err))
	}

	// Return 202 Accepted with job ID
	return c.JSON(202, map[string]interface{}{
		"message":        "Storage unlink initiated",
		"job_id":         jobID,
		"cluster":        cluster.Name,
		"storage_group":  storageGroupID,
		"estimated_time": "2-5 minutes",
	})
}

// buildStorageGroupResponse builds a complete storage group response with linked clusters
func (h *StorageHandler) buildStorageGroupResponse(c echo.Context, storageGroup *types.StorageGroup) StorageGroupResponse {
	// Get all linked clusters
	links, err := h.store.ClusterStorageLinks.GetByStorageGroupID(c.Request().Context(), storageGroup.ID)

	linkedClusters := []ClusterStorageLinkResponse{}
	if err == nil {
		for _, link := range links {
			cluster, err := h.store.Clusters.GetByID(c.Request().Context(), link.ClusterID)
			if err == nil {
				linkedClusters = append(linkedClusters, ClusterStorageLinkResponse{
					ClusterID:   link.ClusterID,
					ClusterName: cluster.Name,
					Role:        link.Role,
					LinkedAt:    link.CreatedAt,
				})
			}
		}
	}

	return StorageGroupResponse{
		ID:                 storageGroup.ID,
		Name:               storageGroup.Name,
		EFSID:              storageGroup.EFSID,
		EFSSecurityGroupID: storageGroup.EFSSecurityGroupID,
		S3Bucket:           storageGroup.S3Bucket,
		Region:             storageGroup.Region,
		Status:             storageGroup.Status,
		LinkedClusters:     linkedClusters,
		CreatedAt:          storageGroup.CreatedAt,
		UpdatedAt:          storageGroup.UpdatedAt,
	}
}

// checkClusterAccess verifies the user has access to the cluster
func (h *StorageHandler) checkClusterAccess(c echo.Context, cluster *types.Cluster) error {
	// Admins can access all clusters
	if auth.IsAdmin(c) {
		return nil
	}

	// Check if user owns this cluster
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	if cluster.OwnerID != userID {
		return ErrorForbidden(c, "You do not have access to this cluster")
	}

	return nil
}
