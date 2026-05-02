package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/postconfig"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/s3"
	"github.com/tsanders-rh/ocpctl/internal/store"
	validation2 "github.com/tsanders-rh/ocpctl/internal/validation"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterHandler handles cluster-related API endpoints
type ClusterHandler struct {
	store    *store.Store
	policy   *policy.Engine
	registry *profile.Registry
}

// NewClusterHandler creates a new cluster handler with dependencies for database access, policy enforcement, and profile management.
// The handler provides endpoints for cluster lifecycle operations (create, list, update, delete, extend TTL).
func NewClusterHandler(s *store.Store, p *policy.Engine, r *profile.Registry) *ClusterHandler {
	return &ClusterHandler{
		store:    s,
		policy:   p,
		registry: r,
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
	Name             string                    `json:"name" validate:"required,min=3,max=63,cluster_name"`
	Platform         string                    `json:"platform" validate:"required,oneof=aws ibmcloud gcp"`
	ClusterType      string                    `json:"cluster_type" validate:"required,oneof=openshift eks iks gke"`
	Version          string                    `json:"version" validate:"required"`
	Profile          string                    `json:"profile" validate:"required"`
	Region           string                    `json:"region" validate:"required"`
	BaseDomain       string                    `json:"base_domain,omitempty"` // Only required for OpenShift
	Owner            string                    `json:"owner" validate:"required,email"`
	Team             string                    `json:"team" validate:"required"`
	CostCenter       string                    `json:"cost_center" validate:"required"`
	TTLHours         *int                      `json:"ttl_hours,omitempty"`
	SSHPublicKey     *string                   `json:"ssh_public_key,omitempty"`
	ExtraTags        map[string]string         `json:"extra_tags,omitempty"`
	OffhoursOptIn      bool                      `json:"offhours_opt_in,omitempty"`
	WorkHoursEnabled   *bool                     `json:"work_hours_enabled,omitempty"`
	WorkHours          *types.WorkHoursSchedule  `json:"work_hours,omitempty"`
	SkipPostDeployment bool                       `json:"skip_post_deployment,omitempty"`
	EnableEFSStorage   bool                       `json:"enable_efs_storage,omitempty"`
	PostConfigAddOns   []types.AddonSelection     `json:"postConfigAddOns,omitempty"` // Pre-approved add-ons with version selection
	CustomPostConfig   *types.CustomPostConfig    `json:"customPostConfig,omitempty"`                                                        // Custom post-deployment operators, scripts, and manifests
	PreserveOnFailure  bool                       `json:"preserve_on_failure,omitempty"`
	CredentialsMode    *string                    `json:"credentials_mode,omitempty" validate:"omitempty,oneof=Manual Passthrough Mint Static"`
	CustomPullSecret   *string                    `json:"custom_pull_secret,omitempty"` // Optional custom pull secret JSON to merge with standard pull secret
	IdempotencyKey     string                     `json:"idempotency_key,omitempty"`
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
//	@Description	Creates a new OpenShift cluster with the specified configuration. Validates against policy engine and initiates async provisioning. Supports post-deployment configuration via pre-approved add-ons (with version selection) or custom operators/manifests.
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
	ctx := c.Request().Context()
	debugLog("Create cluster endpoint called")
	var req CreateClusterRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[ERROR] Failed to bind request: %v", err)
		return ErrorBadRequest(c, "Invalid request body")
	}
	debugLog("Request bound successfully: name=%s, cluster_type=%s, profile=%s", req.Name, req.ClusterType, req.Profile)

	// Validate request body
	if err := c.Validate(req); err != nil {
		log.Printf("[ERROR] Request validation failed: %v", err)
		return ErrorBadRequest(c, err.Error())
	}
	debugLog("Request validation passed")

	// Custom validation: base_domain is required for OpenShift clusters
	if req.ClusterType == "openshift" && req.BaseDomain == "" {
		return ErrorBadRequest(c, "base_domain is required for OpenShift clusters")
	}

	// Check for duplicate cluster creation using idempotency key
	// If idempotency key is provided, check if cluster with same key already exists
	if req.IdempotencyKey != "" {
		// NOTE: Idempotency check temporarily disabled - GetByName method needs implementation
		// TODO: Implement proper idempotency using idempotency_key in database schema
		debugLog("Idempotency key provided: %s", req.IdempotencyKey)
	}

	// Convert empty base_domain to empty string for non-OpenShift clusters
	// (frontend sends empty string, but we want to store empty string in DB for EKS/IKS)
	if req.ClusterType != "openshift" && req.BaseDomain == "" {
		// Keep it as empty string - the database column allows empty strings
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
		Name:              req.Name,
		Platform:          req.Platform,
		ClusterType:       req.ClusterType,
		Version:           req.Version,
		Profile:           req.Profile,
		Region:            req.Region,
		BaseDomain:        req.BaseDomain,
		Owner:             req.Owner,
		Team:              req.Team,
		CostCenter:        req.CostCenter,
		TTLHours:          ttl,
		SSHPublicKey:      req.SSHPublicKey,
		ExtraTags:         req.ExtraTags,
		OffhoursOptIn:     req.OffhoursOptIn,
		CredentialsMode:   req.CredentialsMode,
		PreserveOnFailure: req.PreserveOnFailure,
	}

	// Validate against policy
	debugLog("Starting policy validation for profile: %s", req.Profile)
	validation, err := h.policy.ValidateCreateRequest(policyReq)
	if err != nil {
		log.Printf("[ERROR] Policy validation error: %v", err)
		return LogAndReturnGenericError(c, fmt.Errorf("policy validation failed: %w", err))
	}
	debugLog("Policy validation completed, valid=%v", validation.Valid)

	if !validation.Valid {
		log.Printf("[ERROR] Policy validation failed: %+v", validation)
		return ErrorValidation(c, validation)
	}

	// Validate custom post-config if provided
	if req.CustomPostConfig != nil {
		if errs := validation2.ValidateCustomPostConfig(req.CustomPostConfig); len(errs) > 0 {
			log.Printf("[ERROR] Custom post-config validation failed: %v", errs)
			// Return first validation error
			return ErrorBadRequest(c, errs[0].Error())
		}
	}

	// Validate custom pull secret if provided
	if req.CustomPullSecret != nil && *req.CustomPullSecret != "" {
		var pullSecretData map[string]interface{}
		if err := json.Unmarshal([]byte(*req.CustomPullSecret), &pullSecretData); err != nil {
			log.Printf("[ERROR] Invalid custom pull secret JSON: %v", err)
			return ErrorBadRequest(c, "custom_pull_secret must be valid JSON")
		}

		// Validate that it has the expected structure: {"auths": {...}}
		auths, hasAuths := pullSecretData["auths"]
		if !hasAuths {
			return ErrorBadRequest(c, "custom_pull_secret must have an 'auths' field")
		}

		// Validate auths is an object
		if _, ok := auths.(map[string]interface{}); !ok {
			return ErrorBadRequest(c, "custom_pull_secret 'auths' field must be an object")
		}
	}

	// Check for potential infra ID collisions with recently destroyed clusters
	if err := h.checkInfraIDCollision(c.Request().Context(), req.Name, types.Platform(req.Platform)); err != nil {
		log.Printf("[ERROR] Infra ID collision detected: %v", err)
		return ErrorBadRequest(c, err.Error())
	}

	// Get authenticated user ID
	ownerID, err := auth.GetUserID(c)
	if err != nil {
		log.Printf("[ERROR] Failed to get user ID: %v", err)
		return err
	}
	debugLog("Creating cluster for user ID: %s", ownerID)

	// Parse destroy_at timestamp (empty means infinite TTL)
	var destroyAt *time.Time
	if validation.DestroyAt != "" {
		parsedTime, err := time.Parse(time.RFC3339, validation.DestroyAt)
		if err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("invalid destroy_at timestamp: %w", err))
		}
		destroyAt = &parsedTime
	}

	// Prepare base_domain - use pointer for nullable field
	// EKS/IKS clusters will have nil, OpenShift will have actual value
	var baseDomain *string
	if req.ClusterType == "openshift" && req.BaseDomain != "" {
		baseDomain = &req.BaseDomain
	}

	// Create cluster record
	cluster := &types.Cluster{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Platform:           types.Platform(req.Platform),
		ClusterType:        types.ClusterType(req.ClusterType),
		Version:            req.Version,
		Profile:            req.Profile,
		Region:             req.Region,
		BaseDomain:         baseDomain,
		Status:             types.ClusterStatusPending,
		Owner:              req.Owner,
		OwnerID:            ownerID,
		Team:               req.Team,
		CostCenter:         req.CostCenter,
		TTLHours:           ttl,
		RequestTags:        validation.MergedTags,
		EffectiveTags:      validation.MergedTags,
		SSHPublicKey:       req.SSHPublicKey,
		DestroyAt:          destroyAt,
		OffhoursOptIn:      req.OffhoursOptIn,
		SkipPostDeployment: req.SkipPostDeployment,
		CustomPostConfig:   req.CustomPostConfig,
		PreserveOnFailure:  req.PreserveOnFailure,
		CredentialsMode:    req.CredentialsMode,
		CustomPullSecret:   req.CustomPullSecret,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
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

	// Set initial post_deploy_status based on profile configuration
	// This prevents hibernation from blocking clusters that don't have post-deployment config
	prof, err := h.registry.Get(req.Profile)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to load profile: %w", err))
	}

	hasPostDeployment := prof.PostDeployment != nil && (
		len(prof.PostDeployment.Operators) > 0 ||
		len(prof.PostDeployment.Scripts) > 0 ||
		len(prof.PostDeployment.Manifests) > 0 ||
		len(prof.PostDeployment.HelmCharts) > 0)

	// Load add-ons and merge into custom post-config if specified
	debugLog("PostConfigAddOns received: %+v (count: %d)", req.PostConfigAddOns, len(req.PostConfigAddOns))
	if len(req.PostConfigAddOns) > 0 {
		// Initialize custom post-config if it doesn't exist
		if req.CustomPostConfig == nil {
			req.CustomPostConfig = &types.CustomPostConfig{}
		}

		// Load each add-on with specific version and merge into custom post-config
		for _, selection := range req.PostConfigAddOns {
			// Validate add-on selection has required fields
			if selection.ID == "" {
				return ErrorBadRequest(c, "add-on ID is required")
			}
			if selection.Version == "" {
				return ErrorBadRequest(c, fmt.Sprintf("version is required for add-on '%s'", selection.ID))
			}

			debugLog("Processing add-on: %s version %s", selection.ID, selection.Version)
			addon, err := h.store.PostConfigAddons.GetByAddonIDAndVersion(ctx, selection.ID, selection.Version)
			if err != nil {
				log.Printf("[ERROR] Failed to load add-on %s version %s: %v", selection.ID, selection.Version, err)
				return ErrorBadRequest(c, fmt.Sprintf("add-on '%s' version '%s' not found or disabled", selection.ID, selection.Version))
			}

			debugLog("Loaded add-on %s: %d operators, %d scripts, %d manifests, %d helm charts",
				addon.Name,
				len(addon.Config.Operators),
				len(addon.Config.Scripts),
				len(addon.Config.Manifests),
				len(addon.Config.HelmCharts))

			// Merge add-on config into custom post-config
			req.CustomPostConfig.Operators = append(req.CustomPostConfig.Operators, addon.Config.Operators...)
			req.CustomPostConfig.Scripts = append(req.CustomPostConfig.Scripts, addon.Config.Scripts...)
			req.CustomPostConfig.Manifests = append(req.CustomPostConfig.Manifests, addon.Config.Manifests...)
			req.CustomPostConfig.HelmCharts = append(req.CustomPostConfig.HelmCharts, addon.Config.HelmCharts...)
		}
		debugLog("After merging add-ons: %d total operators, %d scripts, %d manifests, %d helm charts",
			len(req.CustomPostConfig.Operators),
			len(req.CustomPostConfig.Scripts),
			len(req.CustomPostConfig.Manifests),
			len(req.CustomPostConfig.HelmCharts))

		// Re-validate custom post-config after merging add-ons to ensure limits are respected
		if errs := validation2.ValidateCustomPostConfig(req.CustomPostConfig); len(errs) > 0 {
			log.Printf("[ERROR] Custom post-config validation failed after merging add-ons: %v", errs)
			return ErrorBadRequest(c, fmt.Sprintf("validation failed after merging add-ons: %v", errs[0]))
		}

		// Update cluster object with merged configuration
		cluster.CustomPostConfig = req.CustomPostConfig
		debugLog("Updated cluster.CustomPostConfig with merged add-ons")
	}

	// Check if user provided custom post-config (including from add-ons)
	hasCustomPostConfig := req.CustomPostConfig != nil && (
		len(req.CustomPostConfig.Operators) > 0 ||
		len(req.CustomPostConfig.Scripts) > 0 ||
		len(req.CustomPostConfig.Manifests) > 0 ||
		len(req.CustomPostConfig.HelmCharts) > 0)

	if req.SkipPostDeployment || (!hasPostDeployment && !hasCustomPostConfig) {
		// No post-deployment needed - set to 'skipped' so hibernation works immediately
		skipped := "skipped"
		cluster.PostDeployStatus = &skipped
	} else {
		// Post-deployment will run - set to 'pending' to block hibernation until complete
		pending := "pending"
		cluster.PostDeployStatus = &pending
	}

	baseDomainStr := ""
	if cluster.BaseDomain != nil {
		baseDomainStr = *cluster.BaseDomain
	}
	debugLog("About to insert cluster: ID=%s, Name=%s, ClusterType=%s, OwnerID=%s, BaseDomain='%s'",
		cluster.ID, cluster.Name, cluster.ClusterType, cluster.OwnerID, baseDomainStr)

	if err := h.store.Clusters.Create(ctx, cluster); err != nil {
		// Log detailed error server-side for debugging
		requestID := GetRequestID(c)
		log.Printf("[ERROR] Database insert failed: %v (request_id=%s, owner_id=%s, cluster_type=%s, cluster_id=%s)",
			err, requestID, ownerID, cluster.ClusterType, cluster.ID)
		// Return generic error to client to avoid information disclosure
		return ErrorBadRequest(c, fmt.Sprintf("Failed to create cluster. Please contact support with request ID: %s", requestID))
	}

	debugLog("Cluster created successfully: %s", cluster.ID)

	// Create provision job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeCreate,
		Status:      types.JobStatusPending,
		Metadata:    types.JobMetadata{},
		MaxAttempts: 3,
		Attempt:     1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.store.Jobs.Create(ctx, nil, job); err != nil {
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
			Attempt:     1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := h.store.Jobs.Create(ctx, nil, efsJob); err != nil {
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

	// Build enhanced response with execution order if custom post-config exists
	response := map[string]interface{}{
		"cluster": cluster,
	}

	// Add execution order metadata if custom post-config exists
	if cluster.CustomPostConfig != nil {
		executionOrder, err := h.buildExecutionOrderMetadata(cluster.CustomPostConfig)
		if err != nil {
			// Log but don't fail - just return cluster without execution order
			log.Printf("Warning: failed to build execution order metadata for cluster %s: %v", cluster.ID, err)
		} else {
			response["postConfigExecutionOrder"] = executionOrder
		}
	}

	return SuccessOK(c, response)
}

// TaskExecutionInfo represents execution metadata for a single task
type TaskExecutionInfo struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"` // "operator", "script", "manifest", "helmChart"
	Dependencies []string `json:"dependencies"`
	Order        int      `json:"order"` // Execution order (1-based)
}

// buildExecutionOrderMetadata builds execution order metadata for UI visualization
func (h *ClusterHandler) buildExecutionOrderMetadata(config *types.CustomPostConfig) ([]TaskExecutionInfo, error) {
	// Build DAG
	dag, err := postconfig.BuildExecutionDAG(config)
	if err != nil {
		return nil, fmt.Errorf("build execution DAG: %w", err)
	}

	// Get tasks in execution order
	tasks := dag.GetTasksByExecutionOrder()

	// Build execution info for each task
	executionInfo := make([]TaskExecutionInfo, len(tasks))
	for i, task := range tasks {
		executionInfo[i] = TaskExecutionInfo{
			Name:         task.Name,
			Type:         task.Type,
			Dependencies: task.Dependencies,
			Order:        i + 1, // 1-based ordering for UI
		}
	}

	return executionInfo, nil
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

	// Validate region access for AWS clusters
	// This prevents delete jobs from failing due to missing AWS credentials for the region
	if cluster.Platform == types.PlatformAWS {
		if err := h.validateRegionAccess(ctx, cluster.Region); err != nil {
			LogWarning(c, "region access validation failed",
				"cluster_id", cluster.ID,
				"region", cluster.Region,
				"error", err.Error())
			return ErrorBadRequest(c, fmt.Sprintf(
				"Cannot delete cluster: AWS region '%s' is not accessible. "+
					"Ensure AWS credentials are configured for this region. Error: %v",
				cluster.Region, err))
		}
	}

	// Check if cluster can be deleted
	if cluster.Status == types.ClusterStatusDestroying {
		return ErrorConflict(c, "Cluster is already being deleted")
	}

	// Check for in-progress CREATE jobs and cancel them
	// This prevents race conditions when deleting a cluster that's still being created
	createJobs, err := h.store.Jobs.ListByClusterID(ctx, cluster.ID)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to check for existing jobs: %w", err))
	}

	for _, job := range createJobs {
		// Cancel any PENDING or RUNNING CREATE jobs
		if job.JobType == types.JobTypeCreate && (job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning || job.Status == types.JobStatusRetrying) {
			log.Printf("Cancelling in-progress CREATE job %s for cluster %s (user initiated delete)", job.ID, cluster.ID)
			if err := h.store.Jobs.UpdateStatus(ctx, job.ID, types.JobStatusFailed); err != nil {
				log.Printf("Warning: failed to cancel CREATE job %s: %v", job.ID, err)
			}
		}
	}

	// Atomically update cluster status and create destroy job within a transaction
	// This prevents race conditions where status update succeeds but job creation fails
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeDestroy,
		Status:      types.JobStatusPending,
		Metadata:    types.JobMetadata{},
		MaxAttempts: 3,
		Attempt:     1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Update cluster status to destroying
		if err := h.store.Clusters.UpdateStatus(ctx, tx, cluster.ID, types.ClusterStatusDestroying); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}

		// Create deprovision job
		if err := h.store.Jobs.Create(ctx, tx, job); err != nil {
			return fmt.Errorf("create deprovision job: %w", err)
		}

		return nil
	})

	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to initiate cluster deletion: %w", err))
	}

	cluster.Status = types.ClusterStatusDestroying

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
//
//	@Summary		Refresh cluster outputs
//	@Description	Extracts cluster outputs from the install directory and updates the database. Useful if outputs become stale.
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"Cluster not ready"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/refresh-outputs [post]
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
	// Validate cluster is not nil
	if cluster == nil {
		return nil, fmt.Errorf("cluster cannot be nil")
	}

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

	// Construct API URL and Console URL from cluster name and base domain (OpenShift only)
	if cluster.Name != "" && cluster.BaseDomain != nil && *cluster.BaseDomain != "" {
		apiURL := fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, *cluster.BaseDomain)
		outputs.APIURL = &apiURL

		consoleURL := fmt.Sprintf("https://console-openshift-console.apps.%s.%s", cluster.Name, *cluster.BaseDomain)
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
//
//	@Summary		Hibernate cluster
//	@Description	Hibernates a cluster to reduce costs during off-hours. AWS/GCP: stops instances. EKS/GKE: scales node pools to 0. IKS: scales workers to 0.
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"Cluster not ready or platform not supported"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/hibernate [post]
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

	// Atomically update cluster status and create hibernate job within a transaction
	// This prevents race conditions where status update succeeds but job creation fails
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeHibernate,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(types.JobMetadata),
	}

	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Update cluster status to HIBERNATING
		if err := h.store.Clusters.UpdateStatus(ctx, tx, id, types.ClusterStatusHibernating); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}

		// Create HIBERNATE job
		if err := h.store.Jobs.Create(ctx, tx, job); err != nil {
			return fmt.Errorf("create hibernate job: %w", err)
		}

		return nil
	})

	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to initiate hibernation: %w", err))
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
			// Validate that all required work hours fields are present
			if cluster.WorkHoursStart == nil {
				return time.Time{}, fmt.Errorf("work hours enabled but work_hours_start is missing for cluster %s", cluster.ID)
			}
			if cluster.WorkHoursEnd == nil {
				return time.Time{}, fmt.Errorf("work hours enabled but work_hours_end is missing for cluster %s", cluster.ID)
			}
			if cluster.WorkDays == nil {
				return time.Time{}, fmt.Errorf("work hours enabled but work_days is missing for cluster %s", cluster.ID)
			}

			workHoursEnd = *cluster.WorkHoursEnd
			workDays = *cluster.WorkDays
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

	// Couldn't find a future work hours end time within 14 days (shouldn't happen unless work_days is invalid)
	return time.Time{}, fmt.Errorf("could not calculate next work hours end time for cluster %s: end_time=%02d:%02d, work_days=%d (binary: %014b), timezone=%s, current_time=%s",
		cluster.ID, endHour, endMinute, workDays, workDays, user.Timezone, nowInTZ.Format(time.RFC3339))
}

// Resume handles POST /api/v1/clusters/:id/resume
//
//	@Summary		Resume cluster
//	@Description	Resumes a hibernated cluster. AWS/GCP: starts instances. EKS/GKE: restores node pools. IKS: restores workers.
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"Cluster not hibernating or platform not supported"
//	@Failure		403	{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/resume [post]
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

	// Atomically update cluster status and create resume job within a transaction
	// This prevents race conditions where status update succeeds but job creation fails
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeResume,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Metadata:    make(types.JobMetadata),
	}

	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Update cluster status to RESUMING
		if err := h.store.Clusters.UpdateStatus(ctx, tx, id, types.ClusterStatusResuming); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}

		// Create RESUME job
		if err := h.store.Jobs.Create(ctx, tx, job); err != nil {
			return fmt.Errorf("create resume job: %w", err)
		}

		return nil
	})

	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to initiate resume: %w", err))
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

// ClusterStatistics represents aggregated cluster statistics
type ClusterStatistics struct {
	TotalClusters      int                       `json:"total_clusters"`
	ClustersByStatus   []ClusterStatusCount      `json:"clusters_by_status"`
	ClustersByProfile  []ClusterProfileCount     `json:"clusters_by_profile"`
	ActiveClusters     int                       `json:"active_clusters"`
	TotalHourlyCost    float64                   `json:"total_hourly_cost"`
	TotalDailyCost     float64                   `json:"total_daily_cost"`
	TotalMonthlyCost   float64                   `json:"total_monthly_cost"`
	CostByProfile      []ProfileCostBreakdown    `json:"cost_by_profile"`
	CostByUser         []UserCostBreakdown       `json:"cost_by_user"`
}

// ClusterStatusCount represents cluster count per status
type ClusterStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// ClusterProfileCount represents cluster count per profile
type ClusterProfileCount struct {
	Profile string `json:"profile"`
	Count   int    `json:"count"`
}

// ProfileCostBreakdown represents cost breakdown by profile
type ProfileCostBreakdown struct {
	Profile     string  `json:"profile"`
	ClusterCount int     `json:"cluster_count"`
	HourlyCost  float64 `json:"hourly_cost"`
	DailyCost   float64 `json:"daily_cost"`
	MonthlyCost float64 `json:"monthly_cost"`
}

// UserCostBreakdown represents cost breakdown by user
type UserCostBreakdown struct {
	UserID       string  `json:"user_id"`
	Username     string  `json:"username"`
	ClusterCount int     `json:"cluster_count"`
	HourlyCost   float64 `json:"hourly_cost"`
	DailyCost    float64 `json:"daily_cost"`
	MonthlyCost  float64 `json:"monthly_cost"`
}

// GetStatistics handles GET /api/v1/admin/clusters/statistics
//
//	@Summary		Get cluster statistics
//	@Description	Returns aggregated statistics for all clusters (admin only)
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	ClusterStatistics
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/clusters/statistics [get]
func (h *ClusterHandler) GetStatistics(c echo.Context) error {
	ctx := c.Request().Context()

	// Use database aggregation instead of loading all clusters into memory
	// This prevents memory exhaustion with large deployments (>10k clusters)

	// Get total and active cluster counts
	totalClusters, err := h.store.Clusters.GetTotalClusterCount(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get total cluster count: %w", err))
	}

	activeClusters, err := h.store.Clusters.GetActiveClusterCount(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get active cluster count: %w", err))
	}

	// Get aggregated statistics by status
	statusStats, err := h.store.Clusters.GetStatisticsByStatus(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get statistics by status: %w", err))
	}

	// Get aggregated statistics by profile (includes owner_id for cost calculation)
	profileStats, err := h.store.Clusters.GetStatisticsByProfile(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get statistics by profile: %w", err))
	}

	// Collect unique owner IDs for batch user lookup
	ownerIDSet := make(map[string]bool)
	for _, stat := range profileStats {
		ownerIDSet[stat.OwnerID] = true
	}

	ownerIDs := make([]string, 0, len(ownerIDSet))
	for ownerID := range ownerIDSet {
		ownerIDs = append(ownerIDs, ownerID)
	}

	// Batch fetch all users in a single query
	usersByID, err := h.store.Users.GetByIDs(ctx, ownerIDs)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to fetch users: %w", err))
	}

	// Validate that all requested users were fetched
	// A mismatch could indicate database corruption, partial query failure, or missing user records
	if len(usersByID) != len(ownerIDs) {
		// Log warning with details for debugging
		LogWarning(c, "partial user fetch detected in statistics",
			"requested", len(ownerIDs),
			"fetched", len(usersByID))
		return LogAndReturnGenericError(c, fmt.Errorf(
			"partial user fetch failure: requested %d users but got %d (possible database inconsistency)",
			len(ownerIDs), len(usersByID)))
	}

	// Initialize statistics response
	stats := ClusterStatistics{
		TotalClusters:     totalClusters,
		ActiveClusters:    activeClusters,
		ClustersByStatus:  make([]ClusterStatusCount, 0),
		ClustersByProfile: make([]ClusterProfileCount, 0),
		CostByProfile:     make([]ProfileCostBreakdown, 0),
		CostByUser:        make([]UserCostBreakdown, 0),
	}

	// Convert status stats to response format
	for _, stat := range statusStats {
		stats.ClustersByStatus = append(stats.ClustersByStatus, ClusterStatusCount{
			Status: stat.Status,
			Count:  stat.Count,
		})
	}

	// Calculate costs from aggregated profile stats
	profileCounts := make(map[string]int)
	profileCosts := make(map[string]float64)
	userCosts := make(map[string]*UserCostBreakdown)

	for _, stat := range profileStats {
		profileCounts[stat.Profile] += stat.Count

		// Get profile cost (use GetAny to include disabled profiles for existing clusters)
		prof, err := h.registry.GetAny(stat.Profile)
		if err != nil || prof == nil {
			continue // Skip if profile not found
		}

		// Calculate cost per cluster based on status
		// We need to create a minimal cluster object for cost calculation
		cluster := &types.Cluster{
			Status:      types.ClusterStatus(stat.Status),
			ClusterType: types.ClusterTypeOpenShift, // Default, actual value doesn't affect cost much
		}
		hourlyCostPerCluster := h.calculateEffectiveCost(cluster, prof)

		// Multiply by count to get total cost for this group
		totalHourlyCost := hourlyCostPerCluster * float64(stat.Count)
		stats.TotalHourlyCost += totalHourlyCost
		profileCosts[stat.Profile] += totalHourlyCost

		// Track cost by user
		if userCost, exists := userCosts[stat.OwnerID]; exists {
			userCost.ClusterCount += stat.Count
			userCost.HourlyCost += totalHourlyCost
		} else {
			// Get username from batch-fetched users map
			username := stat.OwnerID
			if user, exists := usersByID[stat.OwnerID]; exists {
				username = user.Username
			}
			userCosts[stat.OwnerID] = &UserCostBreakdown{
				UserID:       stat.OwnerID,
				Username:     username,
				ClusterCount: stat.Count,
				HourlyCost:   totalHourlyCost,
			}
		}
	}

	// Calculate daily and monthly costs
	stats.TotalDailyCost = stats.TotalHourlyCost * 24
	stats.TotalMonthlyCost = stats.TotalHourlyCost * 24 * 30

	// Convert profile counts to response format
	for profile, count := range profileCounts {
		stats.ClustersByProfile = append(stats.ClustersByProfile, ClusterProfileCount{
			Profile: profile,
			Count:   count,
		})
	}

	// Convert profile costs to response format
	for profile, hourlyCost := range profileCosts {
		stats.CostByProfile = append(stats.CostByProfile, ProfileCostBreakdown{
			Profile:      profile,
			ClusterCount: profileCounts[profile],
			HourlyCost:   hourlyCost,
			DailyCost:    hourlyCost * 24,
			MonthlyCost:  hourlyCost * 24 * 30,
		})
	}

	// Convert user costs to slice and calculate daily/monthly
	for _, userCost := range userCosts {
		userCost.DailyCost = userCost.HourlyCost * 24
		userCost.MonthlyCost = userCost.HourlyCost * 24 * 30
		stats.CostByUser = append(stats.CostByUser, *userCost)
	}

	return SuccessOK(c, stats)
}

// LongRunningClusterResponse represents a long-running cluster with cost information
type LongRunningClusterResponse struct {
	store.LongRunningCluster
	HourlyCost  float64 `json:"hourly_cost"`
	DailyCost   float64 `json:"daily_cost"`
	MonthlyCost float64 `json:"monthly_cost"`
}

// GetLongRunningClusters handles GET /api/v1/admin/clusters/long-running
//
//	@Summary		Get long-running clusters
//	@Description	Returns READY clusters running 24+ hours without hibernation (admin only)
//	@Tags			admin
//	@Produce		json
//	@Param			min_hours	query		int	false	"Minimum running hours (default: 24)"
//	@Success		200			{object}	map[string]interface{}
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Failure		500			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/clusters/long-running [get]
func (h *ClusterHandler) GetLongRunningClusters(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse min_hours parameter (default: 24)
	minHours := 24
	if minHoursStr := c.QueryParam("min_hours"); minHoursStr != "" {
		if parsed, err := strconv.Atoi(minHoursStr); err == nil && parsed > 0 {
			minHours = parsed
		}
	}

	// Fetch long-running clusters from database
	clusters, err := h.store.Clusters.GetLongRunningClusters(ctx, minHours)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get long-running clusters: %w", err))
	}

	// Calculate costs for each cluster and build response array
	responses := make([]LongRunningClusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		// Get profile for cost calculation
		prof, err := h.registry.GetAny(cluster.Cluster.Profile)
		if err != nil || prof == nil {
			// Skip if profile not found (shouldn't happen for valid clusters)
			log.Printf("Warning: profile %s not found for cluster %s", cluster.Cluster.Profile, cluster.Cluster.ID)
			continue
		}

		// Calculate effective hourly cost based on cluster status
		hourlyCost := h.calculateEffectiveCost(cluster.Cluster, prof)
		dailyCost := hourlyCost * 24
		monthlyCost := hourlyCost * 24 * 30

		responses = append(responses, LongRunningClusterResponse{
			LongRunningCluster: *cluster,
			HourlyCost:         hourlyCost,
			DailyCost:          dailyCost,
			MonthlyCost:        monthlyCost,
		})
	}

	// Calculate aggregate totals
	var totalHourlyCost, totalDailyCost, totalMonthlyCost float64
	for _, r := range responses {
		totalHourlyCost += r.HourlyCost
		totalDailyCost += r.DailyCost
		totalMonthlyCost += r.MonthlyCost
	}

	// Return response with cluster array and totals
	return SuccessOK(c, map[string]interface{}{
		"clusters":           responses,
		"total_count":        len(responses),
		"min_hours":          minHours,
		"total_hourly_cost":  totalHourlyCost,
		"total_daily_cost":   totalDailyCost,
		"total_monthly_cost": totalMonthlyCost,
	})
}

// EC2Instance represents an EC2 instance with relevant details
type EC2Instance struct {
	InstanceID       string    `json:"instance_id"`
	InstanceType     string    `json:"instance_type"`
	State            string    `json:"state"`
	PrivateIPAddress *string   `json:"private_ip_address"`
	PublicIPAddress  *string   `json:"public_ip_address"`
	LaunchTime       time.Time `json:"launch_time"`
	Name             string    `json:"name"`
}

// ClusterInstance represents a generic instance across cloud platforms
type ClusterInstance struct {
	InstanceID       string            `json:"instance_id"`
	InstanceType     string            `json:"instance_type"`
	State            string            `json:"state"`
	PrivateIPAddress *string           `json:"private_ip_address,omitempty"`
	PublicIPAddress  *string           `json:"public_ip_address,omitempty"`
	LaunchTime       *time.Time        `json:"launch_time,omitempty"`
	Name             string            `json:"name"`
	Zone             string            `json:"zone,omitempty"`
	Platform         string            `json:"platform"` // aws, gcp, ibmcloud
	Labels           map[string]string `json:"labels,omitempty"`
	MachineType      string            `json:"machine_type,omitempty"` // GCP-specific
}

// GetInstances handles GET /api/v1/clusters/:id/instances
//
//	@Summary		Get cluster EC2 instances
//	@Description	Returns all EC2 instances associated with the cluster (AWS only)
//	@Tags			clusters
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{array}		EC2Instance
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters/{id}/instances [get]
func (h *ClusterHandler) GetInstances(c echo.Context) error {
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

	// Route to platform-specific handler
	var instances []ClusterInstance
	switch cluster.Platform {
	case types.PlatformAWS:
		instances, err = h.getAWSInstances(ctx, cluster)
	case types.PlatformGCP:
		instances, err = h.getGCPInstances(ctx, cluster)
	default:
		return ErrorBadRequest(c, fmt.Sprintf("Instance information not available for platform: %s", cluster.Platform))
	}

	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get instances: %w", err))
	}

	return SuccessOK(c, instances)
}

// getAWSInstances fetches AWS instances and converts to generic ClusterInstance format
func (h *ClusterHandler) getAWSInstances(ctx context.Context, cluster *types.Cluster) ([]ClusterInstance, error) {
	ec2Instances, err := h.getClusterEC2Instances(ctx, cluster)
	if err != nil {
		return nil, err
	}

	// Convert EC2Instance to ClusterInstance
	instances := make([]ClusterInstance, len(ec2Instances))
	for i, ec2 := range ec2Instances {
		instances[i] = ClusterInstance{
			InstanceID:       ec2.InstanceID,
			InstanceType:     ec2.InstanceType,
			State:            ec2.State,
			PrivateIPAddress: ec2.PrivateIPAddress,
			PublicIPAddress:  ec2.PublicIPAddress,
			LaunchTime:       &ec2.LaunchTime,
			Name:             ec2.Name,
			Platform:         "aws",
		}
	}

	return instances, nil
}

// getGCPInstances fetches GCP instances and converts to generic ClusterInstance format
func (h *ClusterHandler) getGCPInstances(ctx context.Context, cluster *types.Cluster) ([]ClusterInstance, error) {
	// Handle based on cluster type
	if cluster.ClusterType == types.ClusterTypeGKE {
		return h.getGKEInstances(ctx, cluster)
	}

	// For OpenShift on GCP, get compute instances
	return h.getGCPComputeInstances(ctx, cluster)
}

// getGKEInstances fetches GKE node pool instances
func (h *ClusterHandler) getGKEInstances(ctx context.Context, cluster *types.Cluster) ([]ClusterInstance, error) {
	// Get GCP project from environment or cluster metadata
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		return nil, fmt.Errorf("GCP_PROJECT environment variable not set")
	}

	// Create a new context with longer timeout for gcloud command (independent of HTTP request timeout)
	cmdCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Execute gcloud command to get node pool information
	cmd := exec.CommandContext(cmdCtx, "gcloud", "container", "node-pools", "list",
		"--cluster", cluster.Name,
		"--region", cluster.Region,
		"--project", project,
		"--format", "json")

	// Set environment variables for gcloud authentication
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
		fmt.Sprintf("GCP_PROJECT=%s", project),
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GKE node pools: %w", err)
	}

	// Parse node pool information
	var nodePools []struct {
		Name              string `json:"name"`
		InitialNodeCount  int    `json:"initialNodeCount"`
		Status            string `json:"status"`
		Config            struct {
			MachineType string            `json:"machineType"`
			Labels      map[string]string `json:"labels"`
		} `json:"config"`
		Locations []string `json:"locations"`
	}

	if err := json.Unmarshal(output, &nodePools); err != nil {
		return nil, fmt.Errorf("failed to parse node pool information: %w", err)
	}

	// Get actual VM instances for each node pool
	var instances []ClusterInstance
	for _, pool := range nodePools {
		// Get instances in this node pool using instance group managers
		poolInstances, err := h.getGKENodePoolInstances(ctx, cluster.Name, pool.Name, project, cluster.Region)
		if err != nil {
			log.Printf("Warning: failed to get instances for node pool %s: %v", pool.Name, err)
			continue
		}
		instances = append(instances, poolInstances...)
	}

	return instances, nil
}

// getGKENodePoolInstances fetches actual VM instances for a GKE node pool
func (h *ClusterHandler) getGKENodePoolInstances(ctx context.Context, clusterName, poolName, project, region string) ([]ClusterInstance, error) {
	// Create a new context with longer timeout for gcloud commands (independent of HTTP request timeout)
	// This function makes multiple gcloud calls, so we need a generous timeout
	cmdCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get instance group manager name (GKE naming convention)
	// Format: gke-{cluster-name}-{pool-name}-{hash}
	cmd := exec.CommandContext(cmdCtx, "gcloud", "compute", "instance-groups", "list",
		"--project", project,
		"--filter", fmt.Sprintf("name~gke-%s-%s", clusterName, poolName),
		"--format", "json")

	// Set environment variables for gcloud authentication
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
		fmt.Sprintf("GCP_PROJECT=%s", project),
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list instance groups: %w", err)
	}

	var instanceGroups []struct {
		Name string `json:"name"`
		Zone string `json:"zone"`
	}

	if err := json.Unmarshal(output, &instanceGroups); err != nil {
		return nil, fmt.Errorf("failed to parse instance groups: %w", err)
	}

	var instances []ClusterInstance
	for _, group := range instanceGroups {
		// Get instances in this group
		cmd := exec.CommandContext(cmdCtx, "gcloud", "compute", "instance-groups", "list-instances",
			group.Name,
			"--zone", filepath.Base(group.Zone),
			"--project", project,
			"--format", "json")

		// Set environment variables for gcloud authentication
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
			fmt.Sprintf("GCP_PROJECT=%s", project),
		)

		output, err := cmd.Output()
		if err != nil {
			log.Printf("Warning: failed to list instances in group %s: %v", group.Name, err)
			continue
		}

		var groupInstances []struct {
			Instance string `json:"instance"`
			Status   string `json:"status"`
		}

		if err := json.Unmarshal(output, &groupInstances); err != nil {
			log.Printf("Warning: failed to parse instances in group %s: %v", group.Name, err)
			continue
		}

		// Get detailed instance information
		for _, inst := range groupInstances {
			instanceName := filepath.Base(inst.Instance)
			zone := filepath.Base(group.Zone)

			cmd := exec.CommandContext(cmdCtx, "gcloud", "compute", "instances", "describe",
				instanceName,
				"--zone", zone,
				"--project", project,
				"--format", "json")

			// Set environment variables for gcloud authentication
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
				fmt.Sprintf("GCP_PROJECT=%s", project),
			)

			output, err := cmd.Output()
			if err != nil {
				log.Printf("Warning: failed to describe instance %s: %v", instanceName, err)
				continue
			}

			var instanceDetail struct {
				Name         string `json:"name"`
				MachineType  string `json:"machineType"`
				Status       string `json:"status"`
				Zone         string `json:"zone"`
				CreationTimestamp string `json:"creationTimestamp"`
				Labels       map[string]string `json:"labels"`
				NetworkInterfaces []struct {
					NetworkIP string `json:"networkIP"`
					AccessConfigs []struct {
						NatIP string `json:"natIP"`
					} `json:"accessConfigs"`
				} `json:"networkInterfaces"`
			}

			if err := json.Unmarshal(output, &instanceDetail); err != nil {
				log.Printf("Warning: failed to parse instance detail for %s: %v", instanceName, err)
				continue
			}

			// Parse creation timestamp
			var launchTime *time.Time
			if instanceDetail.CreationTimestamp != "" {
				if t, err := time.Parse(time.RFC3339, instanceDetail.CreationTimestamp); err == nil {
					launchTime = &t
				}
			}

			// Extract machine type name (remove zone prefix)
			machineType := filepath.Base(instanceDetail.MachineType)

			// Get IP addresses
			var privateIP, publicIP *string
			if len(instanceDetail.NetworkInterfaces) > 0 {
				if instanceDetail.NetworkInterfaces[0].NetworkIP != "" {
					privateIP = &instanceDetail.NetworkInterfaces[0].NetworkIP
				}
				if len(instanceDetail.NetworkInterfaces[0].AccessConfigs) > 0 {
					if instanceDetail.NetworkInterfaces[0].AccessConfigs[0].NatIP != "" {
						publicIP = &instanceDetail.NetworkInterfaces[0].AccessConfigs[0].NatIP
					}
				}
			}

			instances = append(instances, ClusterInstance{
				InstanceID:       instanceName,
				InstanceType:     machineType,
				MachineType:      machineType,
				State:            instanceDetail.Status,
				PrivateIPAddress: privateIP,
				PublicIPAddress:  publicIP,
				LaunchTime:       launchTime,
				Name:             instanceDetail.Name,
				Zone:             filepath.Base(instanceDetail.Zone),
				Platform:         "gcp",
				Labels:           instanceDetail.Labels,
			})
		}
	}

	return instances, nil
}

// getGCPComputeInstances fetches GCP Compute instances for OpenShift on GCP
func (h *ClusterHandler) getGCPComputeInstances(ctx context.Context, cluster *types.Cluster) ([]ClusterInstance, error) {
	// Get infraID from metadata.json (similar to AWS)
	infraID, err := h.getInfraIDFromMetadata(cluster)
	if err != nil {
		// If we can't get infraID, return empty list
		return []ClusterInstance{}, nil
	}

	// Get GCP project from environment
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		return nil, fmt.Errorf("GCP_PROJECT environment variable not set")
	}

	// Find all instances with label kubernetes-io-cluster-{infraID}=owned
	labelFilter := fmt.Sprintf("labels.kubernetes-io-cluster-%s=owned", infraID)
	log.Printf("[DEBUG] getGCPComputeInstances: infraID=%s, labelFilter=%s, project=%s", infraID, labelFilter, project)

	// Create a new context with longer timeout for gcloud command (independent of HTTP request timeout)
	cmdCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "gcloud", "compute", "instances", "list",
		"--project", project,
		"--filter", labelFilter,
		"--format", "json")

	// Set environment variables for gcloud authentication
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS=%s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
		fmt.Sprintf("GCP_PROJECT=%s", project),
	)

	// Capture both stdout and stderr
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		log.Printf("[ERROR] gcloud command failed: %v, stderr: %s", err, stderr.String())
		return nil, fmt.Errorf("failed to list GCP instances: %w (stderr: %s)", err, stderr.String())
	}

	output := []byte(stdout.String())

	var gcpInstances []struct {
		Name         string `json:"name"`
		MachineType  string `json:"machineType"`
		Status       string `json:"status"`
		Zone         string `json:"zone"`
		CreationTimestamp string `json:"creationTimestamp"`
		Labels       map[string]string `json:"labels"`
		NetworkInterfaces []struct {
			NetworkIP string `json:"networkIP"`
			AccessConfigs []struct {
				NatIP string `json:"natIP"`
			} `json:"accessConfigs"`
		} `json:"networkInterfaces"`
	}

	if err := json.Unmarshal(output, &gcpInstances); err != nil {
		return nil, fmt.Errorf("failed to parse GCP instances: %w", err)
	}

	// Convert to ClusterInstance format
	instances := make([]ClusterInstance, 0, len(gcpInstances))
	for _, gcp := range gcpInstances {
		// Parse creation timestamp
		var launchTime *time.Time
		if gcp.CreationTimestamp != "" {
			if t, err := time.Parse(time.RFC3339, gcp.CreationTimestamp); err == nil {
				launchTime = &t
			}
		}

		// Extract machine type name (remove zone prefix)
		machineType := filepath.Base(gcp.MachineType)

		// Get IP addresses
		var privateIP, publicIP *string
		if len(gcp.NetworkInterfaces) > 0 {
			if gcp.NetworkInterfaces[0].NetworkIP != "" {
				privateIP = &gcp.NetworkInterfaces[0].NetworkIP
			}
			if len(gcp.NetworkInterfaces[0].AccessConfigs) > 0 {
				if gcp.NetworkInterfaces[0].AccessConfigs[0].NatIP != "" {
					publicIP = &gcp.NetworkInterfaces[0].AccessConfigs[0].NatIP
				}
			}
		}

		instances = append(instances, ClusterInstance{
			InstanceID:       gcp.Name,
			InstanceType:     machineType,
			MachineType:      machineType,
			State:            gcp.Status,
			PrivateIPAddress: privateIP,
			PublicIPAddress:  publicIP,
			LaunchTime:       launchTime,
			Name:             gcp.Name,
			Zone:             filepath.Base(gcp.Zone),
			Platform:         "gcp",
			Labels:           gcp.Labels,
		})
	}

	return instances, nil
}

// getClusterEC2Instances fetches EC2 instances for a cluster from AWS
func (h *ClusterHandler) getClusterEC2Instances(ctx context.Context, cluster *types.Cluster) ([]EC2Instance, error) {
	// Get infraID from metadata.json
	infraID, err := h.getInfraIDFromMetadata(cluster)
	if err != nil {
		// If we can't get infraID, try using cluster name as fallback
		// (for clusters that haven't completed provisioning)
		return []EC2Instance{}, nil
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Find all instances with tag kubernetes.io/cluster/{infraID}=owned
	tagKey := fmt.Sprintf("kubernetes.io/cluster/%s", infraID)
	describeInput := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   strPtr("tag-key"),
				Values: []string{tagKey},
			},
			{
				Name: strPtr("instance-state-name"),
				Values: []string{
					"pending",
					"running",
					"stopping",
					"stopped",
				},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(ctx, describeInput)
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	// Collect instance information
	var instances []EC2Instance
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId == nil {
				continue
			}

			// Extract name from tags
			name := ""
			for _, tag := range instance.Tags {
				if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
					name = *tag.Value
					break
				}
			}

			ec2Instance := EC2Instance{
				InstanceID:       *instance.InstanceId,
				InstanceType:     string(instance.InstanceType),
				State:            string(instance.State.Name),
				PrivateIPAddress: instance.PrivateIpAddress,
				PublicIPAddress:  instance.PublicIpAddress,
				Name:             name,
			}

			if instance.LaunchTime != nil {
				ec2Instance.LaunchTime = *instance.LaunchTime
			}

			instances = append(instances, ec2Instance)
		}
	}

	return instances, nil
}

// getInfraIDFromMetadata extracts the infrastructure ID from cluster metadata
func (h *ClusterHandler) getInfraIDFromMetadata(cluster *types.Cluster) (string, error) {
	// First try reading from local filesystem
	workDir := os.Getenv("WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/ocpctl"
	}
	clusterWorkDir := filepath.Join(workDir, cluster.ID)
	metadataPath := filepath.Join(clusterWorkDir, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		// If local file not found, try downloading from S3
		data, err = h.getMetadataFromS3(cluster)
		if err != nil {
			return "", fmt.Errorf("metadata not available locally or in S3: %w", err)
		}
	}

	var metadata struct {
		InfraID string `json:"infraID"`
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse metadata.json: %w", err)
	}

	if metadata.InfraID == "" {
		return "", fmt.Errorf("infraID not found in metadata.json")
	}

	return metadata.InfraID, nil
}

// getMetadataFromS3 downloads cluster metadata from S3
func (h *ClusterHandler) getMetadataFromS3(cluster *types.Cluster) ([]byte, error) {
	ctx := context.Background()

	// Get metadata S3 URI from cluster outputs
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, cluster.ID)
	if err != nil {
		return nil, fmt.Errorf("cluster outputs not found: %w", err)
	}

	if outputs.MetadataS3URI == nil || *outputs.MetadataS3URI == "" {
		return nil, fmt.Errorf("metadata S3 URI not available")
	}

	// Parse S3 URI to extract bucket and key
	s3URI := *outputs.MetadataS3URI
	// Format: s3://bucket/key or file:///path
	if strings.HasPrefix(s3URI, "file://") {
		// Local file reference, try reading it
		filePath := strings.TrimPrefix(s3URI, "file://")
		return os.ReadFile(filePath)
	}

	if !strings.HasPrefix(s3URI, "s3://") {
		return nil, fmt.Errorf("invalid S3 URI format: %s", s3URI)
	}

	// Remove s3:// prefix and split bucket/key
	s3Path := strings.TrimPrefix(s3URI, "s3://")
	parts := strings.SplitN(s3Path, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid S3 URI format: %s", s3URI)
	}

	bucket := parts[0]
	key := parts[1]

	// Download from S3
	return s3.DownloadFile(ctx, bucket, key)
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

// validateRegionAccess validates that AWS credentials have access to the specified region
// Makes a lightweight API call to verify credentials work in the region
func (h *ClusterHandler) validateRegionAccess(ctx context.Context, region string) error {
	// Load AWS config for the specified region
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	// Create EC2 client
	ec2Client := ec2.NewFromConfig(cfg)

	// Make a lightweight API call to verify credentials work in this region
	// DescribeAvailabilityZones is a read-only call that requires minimal permissions
	_, err = ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		// Empty input - just list all AZs in the region
	})
	if err != nil {
		return fmt.Errorf("test AWS API access in region %s: %w", region, err)
	}

	return nil
}

// calculateEffectiveCost calculates the effective hourly cost based on cluster state
// Hibernated clusters cost significantly less than running clusters
func (h *ClusterHandler) calculateEffectiveCost(cluster *types.Cluster, prof *profile.Profile) float64 {
	baseCost := prof.CostControls.EstimatedHourlyCost

	// If cluster is hibernated, calculate reduced cost based on cluster type
	if cluster.Status == types.ClusterStatusHibernated {
		switch cluster.ClusterType {
		case types.ClusterTypeOpenShift:
			// OpenShift (any platform): All instances stopped, only persistent storage remains
			// AWS: EBS volumes, GCP: Persistent Disks
			// Estimate ~10% of running cost (storage only)
			return baseCost * 0.10
		case types.ClusterTypeEKS:
			// EKS: Node groups scaled to 0, but control plane still runs at $0.10/hr
			return 0.10
		case types.ClusterTypeIKS:
			// IKS: Workers scaled to 0, minimal cost
			// Estimate ~5% of running cost
			return baseCost * 0.05
		case types.ClusterTypeGKE:
			// GKE Standard: Node pools scaled to 0, NO control plane cost
			// GKE Standard tier has no control plane charges
			// Only persistent disks remain when hibernated (~2-5% of running cost)
			return baseCost * 0.03
		default:
			// Unknown cluster type, use conservative estimate
			return baseCost * 0.10
		}
	}

	// For all other states (READY, PENDING, etc.), use full cost
	return baseCost
}

// StorageClass represents a Kubernetes storage class
type StorageClass struct {
	Name              string `json:"name"`
	Provisioner       string `json:"provisioner"`
	ReclaimPolicy     string `json:"reclaim_policy,omitempty"`
	VolumeBindingMode string `json:"volume_binding_mode,omitempty"`
	IsDefault         bool   `json:"is_default"`
}

// GetStorageClasses handles GET /api/v1/clusters/:id/storage-classes
//
//	@Summary		Get cluster storage classes
//	@Description	Returns all storage classes available in the cluster
//	@Tags			clusters
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{array}		StorageClass
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/clusters/{id}/storage-classes [get]
func (h *ClusterHandler) GetStorageClasses(c echo.Context) error {
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

	// Only get storage classes if cluster is READY or HIBERNATED
	if cluster.Status != types.ClusterStatusReady && cluster.Status != types.ClusterStatusHibernated {
		return ErrorBadRequest(c, "Storage class information is only available for READY or HIBERNATED clusters")
	}

	// Get storage classes from Kubernetes
	storageClasses, err := h.getClusterStorageClasses(ctx, cluster)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get storage classes: %w", err))
	}

	return SuccessOK(c, storageClasses)
}

// getClusterStorageClasses fetches storage classes from a Kubernetes cluster
func (h *ClusterHandler) getClusterStorageClasses(ctx context.Context, cluster *types.Cluster) ([]StorageClass, error) {
	// Construct kubeconfig path
	workDir := os.Getenv("WORK_DIR")
	if workDir == "" {
		workDir = "/opt/ocpctl/clusters"
	}
	kubeconfigPath := filepath.Join(workDir, cluster.ID, "auth", "kubeconfig")

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return []StorageClass{}, nil // Return empty list if kubeconfig doesn't exist yet
	}

	// Get storage classes using kubectl with JSON output
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "storageclass", "-o", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl get storageclass: %w", err)
	}

	// Parse kubectl output
	var result struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Provisioner       string `json:"provisioner"`
			ReclaimPolicy     string `json:"reclaimPolicy"`
			VolumeBindingMode string `json:"volumeBindingMode"`
		} `json:"items"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse kubectl output: %w", err)
	}

	// Convert to StorageClass objects
	var storageClasses []StorageClass
	for _, item := range result.Items {
		isDefault := false
		// Check for default storage class annotation
		if val, ok := item.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"]; ok && val == "true" {
			isDefault = true
		}

		storageClasses = append(storageClasses, StorageClass{
			Name:              item.Metadata.Name,
			Provisioner:       item.Provisioner,
			ReclaimPolicy:     item.ReclaimPolicy,
			VolumeBindingMode: item.VolumeBindingMode,
			IsDefault:         isDefault,
		})
	}

	return storageClasses, nil
}
// checkInfraIDCollision checks if the cluster name could collide with recently destroyed clusters
// OpenShift generates infra IDs by truncating cluster names to ~27 chars + 5-char random suffix
// Similar names can generate the same infra ID if created close together
func (h *ClusterHandler) checkInfraIDCollision(ctx context.Context, clusterName string, platform types.Platform) error {
	// Only check for OpenShift clusters on AWS (where we've seen this issue)
	if platform != types.PlatformAWS {
		return nil
	}

	// Query for recently destroyed clusters with similar name prefixes
	recentClusters, err := h.store.Clusters.CheckInfraIDCollision(ctx, clusterName, platform)
	if err != nil {
		// Don't fail cluster creation if collision check fails - just log
		log.Printf("[WARN] Failed to check infra ID collision: %v", err)
		return nil
	}

	// If we found similar recently-destroyed clusters, return an error
	if len(recentClusters) > 0 {
		return fmt.Errorf("potential infra ID collision: cluster name '%s' is similar to recently destroyed cluster(s): %v. OpenShift may reuse the same infrastructure ID, causing creation to fail. Please choose a more distinct cluster name", clusterName, recentClusters)
	}

	return nil
}
