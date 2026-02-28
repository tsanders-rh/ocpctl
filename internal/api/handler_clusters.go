package api

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterHandler handles cluster-related API endpoints
type ClusterHandler struct {
	store  *store.Store
	policy *policy.Engine
}

// NewClusterHandler creates a new cluster handler
func NewClusterHandler(s *store.Store, p *policy.Engine) *ClusterHandler {
	return &ClusterHandler{
		store:  s,
		policy: p,
	}
}

// checkClusterAccess verifies the user has access to the cluster
// Returns true if user is owner or admin
func (h *ClusterHandler) checkClusterAccess(c echo.Context, cluster *types.Cluster) error {
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

// CreateClusterRequest represents the API request to create a cluster
type CreateClusterRequest struct {
	Name          string            `json:"name" validate:"required,min=3,max=63"`
	Platform      string            `json:"platform" validate:"required,oneof=aws ibmcloud"`
	Version       string            `json:"version" validate:"required"`
	Profile       string            `json:"profile" validate:"required"`
	Region        string            `json:"region" validate:"required"`
	BaseDomain    string            `json:"base_domain" validate:"required"`
	Owner         string            `json:"owner" validate:"required,email"`
	Team          string            `json:"team" validate:"required"`
	CostCenter    string            `json:"cost_center" validate:"required"`
	TTLHours      *int              `json:"ttl_hours,omitempty"`
	SSHPublicKey  *string           `json:"ssh_public_key,omitempty"`
	ExtraTags     map[string]string `json:"extra_tags,omitempty"`
	OffhoursOptIn bool              `json:"offhours_opt_in,omitempty"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
}

// ExtendClusterRequest represents the API request to extend cluster TTL
type ExtendClusterRequest struct {
	TTLHours int `json:"ttl_hours" validate:"required,min=1"`
}

// ListClustersFilters holds filter parameters for listing clusters
type ListClustersFilters struct {
	Platform   string
	Profile    string
	Owner      string
	Team       string
	CostCenter string
	Status     string
}

// Create handles POST /api/v1/clusters
func (h *ClusterHandler) Create(c echo.Context) error {
	var req CreateClusterRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	// Validate request body
	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get default TTL if not provided
	ttl := 0
	if req.TTLHours != nil {
		ttl = *req.TTLHours
	} else {
		defaultTTL, err := h.policy.GetDefaultTTL(req.Profile)
		if err != nil {
			return ErrorBadRequest(c, "Invalid profile: "+err.Error())
		}
		ttl = defaultTTL
	}

	// Build policy validation request
	policyReq := &types.CreateClusterRequest{
		Name:          req.Name,
		Platform:      req.Platform,
		Version:       req.Version,
		Profile:       req.Profile,
		Region:        req.Region,
		BaseDomain:    req.BaseDomain,
		Owner:         req.Owner,
		Team:          req.Team,
		CostCenter:    req.CostCenter,
		TTLHours:      ttl,
		SSHPublicKey:  req.SSHPublicKey,
		ExtraTags:     req.ExtraTags,
		OffhoursOptIn: req.OffhoursOptIn,
	}

	// Validate against policy
	validation, err := h.policy.ValidateCreateRequest(policyReq)
	if err != nil {
		return ErrorInternal(c, "Policy validation failed: "+err.Error())
	}

	if !validation.Valid {
		return ErrorValidation(c, validation)
	}

	// Get authenticated user ID
	ownerID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()

	// Parse destroy_at timestamp
	destroyAt, err := time.Parse(time.RFC3339, validation.DestroyAt)
	if err != nil {
		return ErrorInternal(c, "Invalid destroy_at timestamp")
	}

	// Create cluster record
	cluster := &types.Cluster{
		ID:            uuid.New().String(),
		Name:          req.Name,
		Platform:      types.Platform(req.Platform),
		Version:       req.Version,
		Profile:       req.Profile,
		Region:        req.Region,
		BaseDomain:    req.BaseDomain,
		Status:        types.ClusterStatusPending,
		Owner:         req.Owner,
		OwnerID:       ownerID,
		Team:          req.Team,
		CostCenter:    req.CostCenter,
		TTLHours:      ttl,
		RequestTags:   validation.MergedTags,
		EffectiveTags: validation.MergedTags,
		DestroyAt:     &destroyAt,
		OffhoursOptIn: req.OffhoursOptIn,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := h.store.Clusters.Create(ctx, cluster); err != nil {
		return ErrorInternal(c, "Failed to create cluster: "+err.Error())
	}

	// Create provision job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeCreate,
		Status:      types.JobStatusPending,
		Metadata:    types.JobMetadata{},
		MaxAttempts: 3,
		Attempt:     0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return ErrorInternal(c, "Failed to create provision job: "+err.Error())
	}

	return SuccessCreated(c, cluster)
}

// List handles GET /api/v1/clusters
func (h *ClusterHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Check if user is admin
	isAdmin := auth.IsAdmin(c)

	// Parse pagination
	pagination := ParsePaginationParams(c)

	// Parse filters
	filters := &ListClustersFilters{
		Platform:   c.QueryParam("platform"),
		Profile:    c.QueryParam("profile"),
		Owner:      c.QueryParam("owner"),
		Team:       c.QueryParam("team"),
		CostCenter: c.QueryParam("cost_center"),
		Status:     c.QueryParam("status"),
	}

	// Non-admin users can only see their own clusters
	if !isAdmin {
		filters.Owner = "" // Clear any owner filter for non-admins
	}

	// Build filter map for response
	filterMap := make(map[string]interface{})
	if filters.Platform != "" {
		filterMap["platform"] = filters.Platform
	}
	if filters.Profile != "" {
		filterMap["profile"] = filters.Profile
	}
	if filters.Owner != "" {
		filterMap["owner"] = filters.Owner
	}
	if filters.Team != "" {
		filterMap["team"] = filters.Team
	}
	if filters.CostCenter != "" {
		filterMap["cost_center"] = filters.CostCenter
	}
	if filters.Status != "" {
		filterMap["status"] = filters.Status
	}

	// Build list filters
	listFilters := store.ListFilters{
		Limit:  pagination.PerPage,
		Offset: pagination.Offset,
	}

	// Non-admin users can only see their own clusters
	if !isAdmin {
		listFilters.OwnerID = &userID
	}

	if filters.Platform != "" {
		platform := types.Platform(filters.Platform)
		listFilters.Platform = &platform
	}
	// Only admins can filter by owner email
	if filters.Owner != "" && isAdmin {
		listFilters.Owner = &filters.Owner
	}
	if filters.Team != "" {
		listFilters.Team = &filters.Team
	}
	if filters.Profile != "" {
		listFilters.Profile = &filters.Profile
	}
	if filters.Status != "" {
		status := types.ClusterStatus(filters.Status)
		listFilters.Status = &status
	}

	// Get clusters with total count
	clusters, total, err := h.store.Clusters.List(ctx, listFilters)
	if err != nil {
		return ErrorInternal(c, "Failed to list clusters: "+err.Error())
	}

	// Calculate pagination metadata
	paginationMeta := CalculatePagination(pagination.Page, pagination.PerPage, total)

	return SuccessPaginated(c, clusters, paginationMeta, filterMap)
}

// Get handles GET /api/v1/clusters/:id
func (h *ClusterHandler) Get(c echo.Context) error {
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

	return SuccessOK(c, cluster)
}

// Delete handles DELETE /api/v1/clusters/:id
func (h *ClusterHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Get cluster to verify it exists
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

	// Check if cluster can be deleted
	if cluster.Status == types.ClusterStatusDestroying {
		return ErrorConflict(c, "Cluster is already being deleted")
	}

	// Update cluster status to destroying (using nil for tx means no transaction)
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroying); err != nil {
		return ErrorInternal(c, "Failed to update cluster status: "+err.Error())
	}

	cluster.Status = types.ClusterStatusDestroying

	// Create deprovision job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeDestroy,
		Status:      types.JobStatusPending,
		Metadata:    types.JobMetadata{},
		MaxAttempts: 3,
		Attempt:     0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return ErrorInternal(c, "Failed to create deprovision job: "+err.Error())
	}

	return SuccessOK(c, cluster)
}

// Extend handles PATCH /api/v1/clusters/:id/extend
func (h *ClusterHandler) Extend(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Parse request body
	var req ExtendClusterRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

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

	// Extend destroy_at timestamp
	if err := h.store.Clusters.UpdateTTL(ctx, id, req.TTLHours); err != nil {
		return ErrorInternal(c, "Failed to extend cluster TTL: "+err.Error())
	}

	// Refresh cluster data
	cluster, err = h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		return ErrorInternal(c, "Failed to retrieve updated cluster: "+err.Error())
	}

	return SuccessOK(c, cluster)
}
