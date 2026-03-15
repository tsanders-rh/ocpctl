package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Name             string                    `json:"name" validate:"required,min=3,max=63"`
	Platform         string                    `json:"platform" validate:"required,oneof=aws ibmcloud"`
	Version          string                    `json:"version" validate:"required"`
	Profile          string                    `json:"profile" validate:"required"`
	Region           string                    `json:"region" validate:"required"`
	BaseDomain       string                    `json:"base_domain" validate:"required"`
	Owner            string                    `json:"owner" validate:"required,email"`
	Team             string                    `json:"team" validate:"required"`
	CostCenter       string                    `json:"cost_center" validate:"required"`
	TTLHours         *int                      `json:"ttl_hours,omitempty"`
	SSHPublicKey     *string                   `json:"ssh_public_key,omitempty"`
	ExtraTags        map[string]string         `json:"extra_tags,omitempty"`
	OffhoursOptIn    bool                      `json:"offhours_opt_in,omitempty"`
	WorkHoursEnabled *bool                     `json:"work_hours_enabled,omitempty"`
	WorkHours        *types.WorkHoursSchedule  `json:"work_hours,omitempty"`
	EnableEFSStorage bool                      `json:"enable_efs_storage,omitempty"`
	IdempotencyKey   string                    `json:"idempotency_key,omitempty"`
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
//
//	@Summary		Create cluster
//	@Description	Creates a new OpenShift cluster with the specified configuration. Validates against policy engine and initiates async provisioning.
//	@Tags			clusters
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateClusterRequest	true	"Cluster configuration"
//	@Success		201		{object}	types.Cluster
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters [post]
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
		return LogAndReturnGenericError(c, fmt.Errorf("policy validation failed: %w", err))
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

	// Parse destroy_at timestamp (empty means infinite TTL)
	var destroyAt *time.Time
	if validation.DestroyAt != "" {
		parsedTime, err := time.Parse(time.RFC3339, validation.DestroyAt)
		if err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("invalid destroy_at timestamp: %w", err))
		}
		destroyAt = &parsedTime
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
		DestroyAt:     destroyAt,
		OffhoursOptIn: req.OffhoursOptIn,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Handle work hours override if provided
	if req.WorkHoursEnabled != nil {
		cluster.WorkHoursEnabled = req.WorkHoursEnabled

		// If work hours are provided, parse and store them
		if req.WorkHours != nil {
			// Parse "09:00" format to time.Time
			startTime, err := time.Parse("15:04", req.WorkHours.StartTime)
			if err != nil {
				return ErrorBadRequest(c, "invalid work hours start time format, use HH:MM")
			}
			endTime, err := time.Parse("15:04", req.WorkHours.EndTime)
			if err != nil {
				return ErrorBadRequest(c, "invalid work hours end time format, use HH:MM")
			}

			// Convert day names to bitmask
			workDaysMask := types.WorkDaysFromStrings(req.WorkHours.WorkDays)
			if workDaysMask == 0 {
				return ErrorBadRequest(c, "at least one work day must be selected")
			}

			cluster.WorkHoursStart = &startTime
			cluster.WorkHoursEnd = &endTime
			cluster.WorkDays = &workDaysMask
		}
	}

	if err := h.store.Clusters.Create(ctx, cluster); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create cluster: %w", err))
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create provision job: %w", err))
	}

	// Create EFS configuration job if requested
	if req.EnableEFSStorage {
		efsJob := &types.Job{
			ID:          uuid.New().String(),
			ClusterID:   cluster.ID,
			JobType:     types.JobTypeConfigureEFS,
			Status:      types.JobStatusPending,
			Metadata:    types.JobMetadata{},
			MaxAttempts: 3,
			Attempt:     0,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := h.store.Jobs.Create(ctx, efsJob); err != nil {
			LogWarning(c, "failed to create EFS configuration job",
				"cluster_id", cluster.ID,
				"error", err.Error())
			// Don't fail cluster creation if EFS job creation fails
		} else {
			LogInfo(c, "EFS configuration job created",
				"cluster_id", cluster.ID,
				"job_id", efsJob.ID)
		}
	}

	// Log successful cluster creation
	LogInfo(c, "cluster created successfully",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"job_id", job.ID,
		"user_id", ownerID)

	return SuccessCreated(c, cluster)
}

// List handles GET /api/v1/clusters
//
//	@Summary		List clusters
//	@Description	Lists all clusters accessible to the authenticated user. Admins see all clusters, regular users see only their own.
//	@Tags			clusters
//	@Produce		json
//	@Param			page		query		int		false	"Page number"	default(1)
//	@Param			per_page	query		int		false	"Items per page"	default(50)
//	@Param			platform	query		string	false	"Filter by platform (aws, ibmcloud)"
//	@Param			profile		query		string	false	"Filter by profile name"
//	@Param			owner		query		string	false	"Filter by owner email (admin only)"
//	@Param			team		query		string	false	"Filter by team"
//	@Param			cost_center	query		string	false	"Filter by cost center"
//	@Param			status		query		string	false	"Filter by status"
//	@Success		200			{object}	PaginatedResponse{data=[]types.Cluster}
//	@Failure		401			{object}	ErrorResponse
//	@Failure		500			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters [get]
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to list clusters: %w", err))
	}

	// Calculate pagination metadata
	paginationMeta := CalculatePagination(pagination.Page, pagination.PerPage, total)

	return SuccessPaginated(c, clusters, paginationMeta, filterMap)
}

// Get handles GET /api/v1/clusters/:id
//
//	@Summary		Get cluster
//	@Description	Retrieves details of a specific cluster by ID
//	@Tags			clusters
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	types.Cluster
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters/{id} [get]
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	return SuccessOK(c, cluster)
}

// Delete handles DELETE /api/v1/clusters/:id
//
//	@Summary		Delete cluster
//	@Description	Initiates cluster destruction. Creates a background job to deprovision all cluster resources.
//	@Tags			clusters
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	types.Cluster
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		409	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters/{id} [delete]
func (h *ClusterHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()

	// Get cluster ID
	id := c.Param("id")

	// Get authenticated user ID
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get cluster to verify it exists
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

	// Check if cluster can be deleted
	if cluster.Status == types.ClusterStatusDestroying {
		return ErrorConflict(c, "Cluster is already being deleted")
	}

	// Update cluster status to destroying (using nil for tx means no transaction)
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroying); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to update cluster status: %w", err))
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create deprovision job: %w", err))
	}

	// Log successful cluster deletion
	LogInfo(c, "cluster deletion initiated",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"job_id", job.ID,
		"user_id", userID)

	return SuccessOK(c, cluster)
}

// Extend handles PATCH /api/v1/clusters/:id/extend
//
//	@Summary		Extend cluster TTL
//	@Description	Extends the time-to-live (TTL) of a cluster, postponing its automatic destruction
//	@Tags			clusters
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Cluster ID"
//	@Param			request	body		ExtendClusterRequest	true	"TTL extension request"
//	@Success		200		{object}	types.Cluster
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters/{id}/extend [patch]
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
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
	}

	// Check access
	if err := h.checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Extend destroy_at timestamp
	if err := h.store.Clusters.UpdateTTL(ctx, id, req.TTLHours); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to extend cluster TTL: %w", err))
	}

	// Refresh cluster data
	cluster, err = h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve updated cluster: %w", err))
	}

	return SuccessOK(c, cluster)
}

// RefreshOutputs handles POST /api/v1/clusters/:id/refresh-outputs
// This endpoint extracts cluster outputs from the install directory and updates the database
func (h *ClusterHandler) RefreshOutputs(c echo.Context) error {
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

	// Only refresh outputs for ready or failed clusters
	if cluster.Status != types.ClusterStatusReady && cluster.Status != types.ClusterStatusFailed {
		return ErrorBadRequest(c, "Can only refresh outputs for clusters in READY or FAILED status")
	}

	// Extract cluster outputs from the install directory
	outputs, err := h.extractClusterOutputs(cluster)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to extract cluster outputs: %w", err))
	}

	// Upsert outputs (create or update)
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to upsert cluster outputs: %w", err))
	}

	LogInfo(c, "cluster outputs refreshed",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"api_url", outputs.APIURL,
		"console_url", outputs.ConsoleURL)

	return SuccessOK(c, outputs)
}

// extractClusterOutputs extracts cluster access information from the install directory
func (h *ClusterHandler) extractClusterOutputs(cluster *types.Cluster) (*types.ClusterOutputs, error) {
	// Get work directory from environment
	workDir := os.Getenv("WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/ocpctl"
	}
	clusterWorkDir := filepath.Join(workDir, cluster.ID)

	outputs := &types.ClusterOutputs{
		ID:        uuid.New().String(),
		ClusterID: cluster.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Construct API URL and Console URL from cluster name and base domain
	if cluster.Name != "" && cluster.BaseDomain != "" {
		apiURL := fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, cluster.BaseDomain)
		outputs.APIURL = &apiURL

		consoleURL := fmt.Sprintf("https://console-openshift-console.apps.%s.%s", cluster.Name, cluster.BaseDomain)
		outputs.ConsoleURL = &consoleURL
	}

	// Set metadata S3 URI
	metadataPath := filepath.Join(clusterWorkDir, "metadata.json")
	metadataURI := fmt.Sprintf("file://%s", metadataPath)
	outputs.MetadataS3URI = &metadataURI

	// Set kubeconfig S3 URI
	kubeconfigPath := filepath.Join(clusterWorkDir, "auth", "kubeconfig")
	kubeconfigURI := fmt.Sprintf("file://%s", kubeconfigPath)
	outputs.KubeconfigS3URI = &kubeconfigURI

	// Set kubeadmin secret reference
	kubeadminPasswordPath := filepath.Join(clusterWorkDir, "auth", "kubeadmin-password")
	kubeadminRef := fmt.Sprintf("file://%s", kubeadminPasswordPath)
	outputs.KubeadminSecretRef = &kubeadminRef

	return outputs, nil
}

// Hibernate handles POST /api/v1/clusters/:id/hibernate
// Hibernates a cluster by stopping its instances (platform-dependent)
func (h *ClusterHandler) Hibernate(c echo.Context) error {
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

	// Can only hibernate READY clusters
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, "Can only hibernate clusters in READY status")
	}

	// Check for existing HIBERNATE job
	existingJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, id, types.JobTypeHibernate)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to check for existing hibernate jobs: %w", err))
	}

	// Check if there's already a pending or running hibernate job
	for _, job := range existingJobs {
		if job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning {
			return ErrorBadRequest(c, "A hibernate job is already in progress for this cluster")
		}
	}

	// Get user ID for logging
	userID, _ := auth.GetUserID(c)

	// Update cluster status to HIBERNATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, id, types.ClusterStatusHibernating); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to update cluster status: %w", err))
	}

	// Create HIBERNATE job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeHibernate,
		Status:      types.JobStatusPending,
		Attempt:     0,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(types.JobMetadata),
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create hibernate job: %w", err))
	}

	// Log successful hibernation initiation
	LogInfo(c, "cluster hibernation initiated",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"job_id", job.ID,
		"user_id", userID)

	// Refresh cluster data
	cluster, err = h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve updated cluster: %w", err))
	}

	return SuccessOK(c, cluster)
}

// calculateNextHibernateTime calculates the next time work hours will end (next hibernate time)
// Returns zero time if work hours are not enabled for this cluster
func (h *ClusterHandler) calculateNextHibernateTime(ctx context.Context, cluster *types.Cluster) (time.Time, error) {
	// Get user to access timezone and default work hours
	user, err := h.store.Users.GetByID(ctx, cluster.OwnerID)
	if err != nil {
		return time.Time{}, fmt.Errorf("get user: %w", err)
	}

	// Determine effective work hours (cluster override or user default)
	var workHoursEnabled bool
	var workHoursEnd time.Time
	var workDays int16

	if cluster.WorkHoursEnabled != nil {
		workHoursEnabled = *cluster.WorkHoursEnabled
		if workHoursEnabled {
			if cluster.WorkHoursStart != nil && cluster.WorkHoursEnd != nil && cluster.WorkDays != nil {
				workHoursEnd = *cluster.WorkHoursEnd
				workDays = *cluster.WorkDays
			} else {
				return time.Time{}, fmt.Errorf("work hours enabled but config missing")
			}
		} else {
			// Work hours explicitly disabled
			return time.Time{}, nil
		}
	} else {
		// Use user's default work hours
		workHoursEnabled = user.WorkHoursEnabled
		if !workHoursEnabled {
			return time.Time{}, nil
		}
		workHoursEnd = user.WorkHoursEnd
		workDays = user.WorkDays
	}

	// Load user's timezone
	location, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %s: %w", user.Timezone, err)
	}

	// Get current time in user's timezone
	nowInTZ := time.Now().In(location)

	// Extract work hours end time (just the time component)
	endHour := workHoursEnd.Hour()
	endMinute := workHoursEnd.Minute()

	// Find the next work hours end time
	// Start by checking today
	candidateDate := nowInTZ

	for i := 0; i < 14; i++ { // Check up to 2 weeks ahead to handle weekends
		// Check if this day is a work day
		if types.IsWorkDay(workDays, candidateDate.Weekday()) {
			// Build the end time for this work day
			endTime := time.Date(
				candidateDate.Year(),
				candidateDate.Month(),
				candidateDate.Day(),
				endHour,
				endMinute,
				0, 0,
				location,
			)

			// If this end time is in the future, use it
			if endTime.After(nowInTZ) {
				return endTime, nil
			}
		}

		// Move to next day
		candidateDate = candidateDate.Add(24 * time.Hour)
	}

	// Couldn't find a future work hours end time (shouldn't happen)
	return time.Time{}, fmt.Errorf("could not calculate next work hours end time")
}

// Resume handles POST /api/v1/clusters/:id/resume
// Resumes a hibernated cluster by starting its instances (platform-dependent)
func (h *ClusterHandler) Resume(c echo.Context) error {
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

	// Can only resume HIBERNATED clusters
	if cluster.Status != types.ClusterStatusHibernated {
		return ErrorBadRequest(c, "Can only resume clusters in HIBERNATED status")
	}

	// Check for existing RESUME job
	existingJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, id, types.JobTypeResume)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to check for existing resume jobs: %w", err))
	}

	// Check if there's already a pending or running resume job
	for _, job := range existingJobs {
		if job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning {
			return ErrorBadRequest(c, "A resume job is already in progress for this cluster")
		}
	}

	// Get user ID for logging
	userID, _ := auth.GetUserID(c)

	// Update cluster status to RESUMING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, id, types.ClusterStatusResuming); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to update cluster status: %w", err))
	}

	// Create RESUME job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeResume,
		Status:      types.JobStatusPending,
		Attempt:     0,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(types.JobMetadata),
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to create resume job: %w", err))
	}

	// Set grace period to prevent auto-hibernation until next scheduled hibernate time
	// When user manually resumes, cluster should stay resumed until work hours end
	gracePeriodEnd, err := h.calculateNextHibernateTime(ctx, cluster)
	if err == nil && !gracePeriodEnd.IsZero() {
		if err := h.store.Clusters.SetLastWorkHoursCheck(ctx, cluster.ID, gracePeriodEnd); err != nil {
			// Log warning but don't fail the resume operation
			LogWarning(c, "failed to set work hours grace period", "error", err)
		} else {
			LogInfo(c, "set work hours grace period", "grace_period_until", gracePeriodEnd)
		}
	}

	// Log successful resume initiation
	LogInfo(c, "cluster resume initiated",
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"job_id", job.ID,
		"user_id", userID)

	// Refresh cluster data
	cluster, err = h.store.Clusters.GetByID(ctx, id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve updated cluster: %w", err))
	}

	return SuccessOK(c, cluster)
}
