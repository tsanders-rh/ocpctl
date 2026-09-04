package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/orphan"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// OrphanedResourceHandler handles orphaned resource API endpoints
type OrphanedResourceHandler struct {
	store         *store.Store
	policy        *policy.Engine
	safetyCfg     orphan.Config
	clusterLookup orphan.ClusterLookup
	vpcInspector  orphan.VPCInspector
}

// NewOrphanedResourceHandler creates a new orphaned resource handler
func NewOrphanedResourceHandler(s *store.Store, p *policy.Engine) *OrphanedResourceHandler {
	return &OrphanedResourceHandler{
		store:         s,
		policy:        p,
		safetyCfg:     orphan.ConfigFromEnv(),
		clusterLookup: orphan.NewStoreClusterLookup(s.Clusters),
		vpcInspector:  orphan.NewAWSVPCInspector(),
	}
}

// evaluateSafety runs the orphaned-resource deletion safety gate for one
// resource. It never deletes anything. VPC probes get a bounded timeout so a
// slow AWS call can't hang the request.
func (h *OrphanedResourceHandler) evaluateSafety(ctx context.Context, resource *types.OrphanedResource) orphan.Verdict {
	evalCtx := ctx
	if resource.ResourceType == types.OrphanedResourceTypeVPC {
		var cancel context.CancelFunc
		evalCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	return orphan.Evaluate(evalCtx, resource, h.safetyCfg, time.Now(), h.clusterLookup, h.vpcInspector)
}

// auditDelete records an audit event for a delete attempt (blocked, forced, or
// clean). Fire-and-forget: audit failures never fail the request.
func (h *OrphanedResourceHandler) auditDelete(c echo.Context, resource *types.OrphanedResource, actor string, status types.AuditEventStatus, action string, verdict orphan.Verdict, forced bool) {
	ip := c.RealIP()
	userAgent := c.Request().UserAgent()

	metadata := types.JobMetadata{
		"resource_id":   resource.ID,
		"resource_type": string(resource.ResourceType),
		"aws_resource":  resource.ResourceID,
		"region":        resource.Region,
		"cluster_name":  resource.ClusterName,
		"safe":          verdict.Safe,
		"forced":        forced,
	}
	if len(verdict.BlockReasons) > 0 {
		metadata["block_reasons"] = verdict.BlockReasons
	}
	if verdict.SourceClusterStatus != nil {
		metadata["source_cluster_status"] = *verdict.SourceClusterStatus
	}

	event := &types.AuditEvent{
		ID:              uuid.New().String(),
		Actor:           actor,
		Action:          action,
		TargetClusterID: resource.ClusterID,
		TargetJobID:     resource.JobID,
		Status:          status,
		Metadata:        metadata,
		IPAddress:       &ip,
		UserAgent:       &userAgent,
		CreatedAt:       time.Now(),
	}

	_ = h.store.Audit.Log(c.Request().Context(), event)
}

// MarkResolvedRequest represents the request to mark a resource as resolved
type MarkResolvedRequest struct {
	Notes string `json:"notes"`
}

// MarkIgnoredRequest represents the request to mark a resource as ignored
type MarkIgnoredRequest struct {
	Notes string `json:"notes"`
}

// OrphanedResourceListResponse represents the paginated list response
type OrphanedResourceListResponse struct {
	Resources []*types.OrphanedResource `json:"resources"`
	Total     int                       `json:"total"`
	Limit     int                       `json:"limit"`
	Offset    int                       `json:"offset"`
}

// List handles GET /api/v1/admin/orphaned-resources
//
//	@Summary		List orphaned resources
//	@Description	Lists orphaned AWS resources that were detected by the janitor. Supports filtering by status, type, and region.
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Param			status	query		string	false	"Filter by status (active, resolved, ignored)"
//	@Param			type	query		string	false	"Filter by resource type (VPC, LoadBalancer, HostedZone, DNSRecord, EC2Instance, S3Bucket)"
//	@Param			region	query		string	false	"Filter by AWS region"
//	@Param			limit	query		int		false	"Maximum number of results (default 50, max 100)"
//	@Param			offset	query		int		false	"Number of results to skip (default 0)"
//	@Success		200		{object}	OrphanedResourceListResponse
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources [get]
func (h *OrphanedResourceHandler) List(c echo.Context) error {
	// Parse query parameters
	filters := store.OrphanedResourceFilters{
		Limit:  50, // Default limit
		Offset: 0,
	}

	// Parse status filter
	if statusStr := c.QueryParam("status"); statusStr != "" {
		status := types.OrphanedResourceStatus(statusStr)
		filters.Status = &status
	}

	// Parse resource type filter
	if typeStr := c.QueryParam("type"); typeStr != "" {
		resourceType := types.OrphanedResourceType(typeStr)
		filters.ResourceType = &resourceType
	}

	// Parse region filter
	if region := c.QueryParam("region"); region != "" {
		filters.Region = &region
	}

	// Parse limit
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err == nil && limit > 0 && limit <= 100 {
			filters.Limit = limit
		}
	}

	// Parse offset
	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err == nil && offset >= 0 {
			filters.Offset = offset
		}
	}

	// Get orphaned resources
	resources, total, err := h.store.OrphanedResources.List(c.Request().Context(), filters)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to list orphaned resources: %w", err))
	}

	// Return paginated response
	return c.JSON(200, OrphanedResourceListResponse{
		Resources: resources,
		Total:     total,
		Limit:     filters.Limit,
		Offset:    filters.Offset,
	})
}

// GetStats handles GET /api/v1/admin/orphaned-resources/stats
//
//	@Summary		Get orphaned resources statistics
//	@Description	Returns aggregated statistics about orphaned resources grouped by type and status
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/stats [get]
func (h *OrphanedResourceHandler) GetStats(c echo.Context) error {
	stats, err := h.store.OrphanedResources.GetStats(c.Request().Context())
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get orphaned resources statistics: %w", err))
	}

	return c.JSON(200, stats)
}

// MarkResolved handles PATCH /api/v1/admin/orphaned-resources/:id/resolve
//
//	@Summary		Mark orphaned resource as resolved
//	@Description	Marks an orphaned resource as resolved (e.g., after manual cleanup in AWS Console)
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Resource ID"
//	@Param			body	body		MarkResolvedRequest		true	"Resolution notes"
//	@Success		200		{object}	types.OrphanedResource
//	@Failure		400		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/{id}/resolve [patch]
func (h *OrphanedResourceHandler) MarkResolved(c echo.Context) error {
	id := c.Param("id")

	var req MarkResolvedRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	// Get user email from context (set by auth middleware)
	userEmail := "unknown"
	if user := c.Get("user"); user != nil {
		if u, ok := user.(*types.User); ok {
			userEmail = u.Email
		}
	}

	// Mark as resolved
	err := h.store.OrphanedResources.MarkResolved(c.Request().Context(), id, userEmail, req.Notes)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to mark resource %s as resolved: %w", id, err))
	}

	// Get updated resource
	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get updated resource %s: %w", id, err))
	}

	return c.JSON(200, resource)
}

// MarkIgnored handles PATCH /api/v1/admin/orphaned-resources/:id/ignore
//
//	@Summary		Mark orphaned resource as ignored
//	@Description	Marks an orphaned resource as ignored (e.g., false positive or intentionally kept)
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Resource ID"
//	@Param			body	body		MarkIgnoredRequest		true	"Ignore reason"
//	@Success		200		{object}	types.OrphanedResource
//	@Failure		400		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/{id}/ignore [patch]
func (h *OrphanedResourceHandler) MarkIgnored(c echo.Context) error {
	id := c.Param("id")

	var req MarkIgnoredRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	// Mark as ignored
	err := h.store.OrphanedResources.MarkIgnored(c.Request().Context(), id, req.Notes)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to mark resource %s as ignored: %w", id, err))
	}

	// Get updated resource
	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get updated resource %s: %w", id, err))
	}

	return c.JSON(200, resource)
}

// Delete handles DELETE /api/v1/admin/orphaned-resources/:id
//
//	@Summary		Delete orphaned AWS resource
//	@Description	Actually deletes the orphaned resource from AWS (currently supports HostedZone and DNSRecord only). Other resource types must be deleted manually in AWS Console.
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Resource ID"
//	@Success		200	{object}	types.OrphanedResource
//	@Failure		400	{object}	map[string]string	"Resource type not supported for automated deletion"
//	@Failure		404	{object}	map[string]string	"Resource not found"
//	@Failure		500	{object}	map[string]string	"Failed to delete resource from AWS"
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/{id} [delete]
func (h *OrphanedResourceHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	// Get the resource
	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return ErrorNotFound(c, "Resource not found")
	}

	// Get user email from context
	userEmail := "unknown"
	if user := c.Get("user"); user != nil {
		if u, ok := user.(*types.User); ok {
			userEmail = u.Email
		}
	}

	// Safety gate: refuse to delete a resource that fails a safety check unless
	// the caller explicitly overrides with ?force=true. The gate is the primary
	// guard against deleting a live cluster's infrastructure.
	force := strings.EqualFold(c.QueryParam("force"), "true") || c.QueryParam("force") == "1"
	verdict := h.evaluateSafety(c.Request().Context(), resource)
	if !verdict.Safe && !force {
		h.auditDelete(c, resource, userEmail, types.AuditEventStatusDenied,
			"orphaned_resource.delete_blocked", verdict, false)
		return c.JSON(409, map[string]interface{}{
			"error":   "safety_check_failed",
			"message": "Deletion blocked by safety checks. Review block_reasons and retry with ?force=true to override.",
			"verdict": verdict,
		})
	}

	// Delete the resource based on type
	// For VPC deletions, use a longer timeout (5 minutes) due to the complex cleanup process
	ctx := c.Request().Context()
	if resource.ResourceType == types.OrphanedResourceTypeVPC {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
	}

	// Perform the real cloud teardown via the shared executor (same code path the
	// janitor's auto-remediation uses).
	err = orphan.DeleteResource(ctx, resource, orphan.DeleteOptions{GCPProject: os.Getenv("GCP_PROJECT")})
	if err != nil {
		// Unsupported types and missing GCP project are client errors (400).
		if errors.Is(err, orphan.ErrUnsupportedResourceType) {
			return ErrorBadRequest(c, err.Error())
		}
		if errors.Is(err, orphan.ErrGCPProjectRequired) {
			return ErrorBadRequest(c, "GCP_PROJECT environment variable not set")
		}
		// For VPC deletion errors, return the specific error message (contains helpful retry instructions)
		if resource.ResourceType == types.OrphanedResourceTypeVPC {
			log.Printf("[ERROR] request_id=unknown method=%s path=%s error=failed to delete %s resource %s: %v",
				c.Request().Method, c.Request().URL.Path, resource.ResourceType, resource.ResourceName, err)
			return c.JSON(500, map[string]string{
				"error":   "vpc_deletion_failed",
				"message": err.Error(),
			})
		}
		return LogAndReturnGenericError(c, fmt.Errorf("failed to delete %s resource %s: %w", resource.ResourceType, resource.ResourceName, err))
	}

	// Resource deletion succeeded - now update the database
	// Use a fresh context with a reasonable timeout to avoid context deadline exceeded errors
	// after long-running deletions (like VPC deletions that can take 4-5 minutes)
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dbCancel()

	// Mark as resolved
	forcedUnsafe := force && !verdict.Safe
	notes := fmt.Sprintf("Automatically deleted via API by %s", userEmail)
	if forcedUnsafe {
		notes = fmt.Sprintf("Force-deleted via API by %s despite safety checks (%s)",
			userEmail, strings.Join(verdict.BlockReasons, "; "))
	}
	err = h.store.OrphanedResources.MarkResolved(dbCtx, id, userEmail, notes)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("resource deleted but failed to update database for %s: %w", id, err))
	}

	// Audit the successful deletion (distinguish a clean delete from a forced override).
	deleteAction := "orphaned_resource.deleted"
	if forcedUnsafe {
		deleteAction = "orphaned_resource.delete_forced"
	}
	h.auditDelete(c, resource, userEmail, types.AuditEventStatusSuccess, deleteAction, verdict, forcedUnsafe)

	// Get updated resource
	resource, err = h.store.OrphanedResources.GetByID(dbCtx, id)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("resource deleted but failed to get updated status for %s: %w", id, err))
	}

	return c.JSON(200, resource)
}

// Safety previews the deletion safety gate for an orphaned resource without
// deleting anything.
//
//	@Summary		Preview deletion safety checks
//	@Description	Runs the deletion safety gate (live-cluster guard, grace period, status, VPC-alive probe) for an orphaned resource and returns the verdict. Deletes nothing. Admin only.
//	@Tags			Orphaned Resources
//	@Produce		json
//	@Param			id	path		string	true	"Resource ID"
//	@Success		200	{object}	map[string]interface{}
//	@Failure		404	{object}	map[string]string	"Resource not found"
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/{id}/safety [get]
func (h *OrphanedResourceHandler) Safety(c echo.Context) error {
	id := c.Param("id")

	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return ErrorNotFound(c, "Resource not found")
	}

	verdict := h.evaluateSafety(c.Request().Context(), resource)
	return c.JSON(200, map[string]interface{}{
		"resource": resource,
		"verdict":  verdict,
	})
}
