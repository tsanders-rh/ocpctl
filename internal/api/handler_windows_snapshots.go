package api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AWS regions that support OpenShift (subset of all AWS regions)
var supportedAWSRegions = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
	"ca-central-1",
	"eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1", "eu-north-1",
	"ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-northeast-2", "ap-south-1",
	"sa-east-1",
}

const defaultWindowsImageVersion = "1.0"

// WindowsSnapshotHandler handles Windows snapshot management endpoints
type WindowsSnapshotHandler struct {
	store *store.Store
}

// NewWindowsSnapshotHandler creates a new Windows snapshot handler
func NewWindowsSnapshotHandler(st *store.Store) *WindowsSnapshotHandler {
	return &WindowsSnapshotHandler{
		store: st,
	}
}

// ListWindowsSnapshots lists all regional Windows snapshots
// GET /api/v1/windows-snapshots
func (h *WindowsSnapshotHandler) ListWindowsSnapshots(c echo.Context) error {
	ctx := c.Request().Context()

	// Optional filters
	region := c.QueryParam("region")
	statusStr := c.QueryParam("status")

	var regionPtr *string
	var statusPtr *types.WindowsSnapshotStatus

	if region != "" {
		regionPtr = &region
	}

	if statusStr != "" {
		status := types.WindowsSnapshotStatus(statusStr)
		statusPtr = &status
	}

	snapshots, err := h.store.ListWindowsSnapshots(ctx, regionPtr, statusPtr)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to list snapshots: %v", err))
	}

	return c.JSON(http.StatusOK, snapshots)
}

// GetWindowsSnapshot retrieves a single snapshot by ID
// GET /api/v1/windows-snapshots/:id
func (h *WindowsSnapshotHandler) GetWindowsSnapshot(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	snapshot, err := h.store.GetWindowsSnapshot(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Snapshot not found: %v", err))
	}

	return c.JSON(http.StatusOK, snapshot)
}

// GetWindowsSnapshotCoverage returns snapshot coverage summary across regions
// GET /api/v1/windows-snapshots/coverage
func (h *WindowsSnapshotHandler) GetWindowsSnapshotCoverage(c echo.Context) error {
	ctx := c.Request().Context()

	// Get latest version from query param or use default
	latestVersion := c.QueryParam("version")
	if latestVersion == "" {
		latestVersion = defaultWindowsImageVersion
	}

	coverage, err := h.store.GetWindowsSnapshotCoverage(ctx, supportedAWSRegions, latestVersion)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to get coverage: %v", err))
	}

	return c.JSON(http.StatusOK, coverage)
}

// CreateWindowsSnapshot creates a new regional Windows snapshot
// POST /api/v1/windows-snapshots
func (h *WindowsSnapshotHandler) CreateWindowsSnapshot(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse request
	var req types.CreateWindowsSnapshotRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate region
	validRegion := false
	for _, r := range supportedAWSRegions {
		if r == req.Region {
			validRegion = true
			break
		}
	}
	if !validRegion {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unsupported region: %s", req.Region))
	}

	// Check if snapshot already exists for this region/version
	existing, err := h.store.GetWindowsSnapshotByRegionAndVersion(ctx, req.Region, req.Version)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to check existing snapshot: %v", err))
	}
	if existing != nil {
		return echo.NewHTTPError(http.StatusConflict, fmt.Sprintf("Snapshot already exists for region %s version %s (status: %s)", req.Region, req.Version, existing.Status))
	}

	// Get current user for audit
	user, ok := c.Get("user").(*types.User)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not found in context")
	}

	// Create snapshot record
	snapshotID := uuid.New().String()
	jobID := uuid.New().String()

	snapshot := &types.WindowsSnapshot{
		ID:            snapshotID,
		Region:        req.Region,
		Version:       req.Version,
		EBSSnapshotID: "", // Will be populated by worker
		Status:        types.WindowsSnapshotStatusCreating,
		S3SourceURL:   req.S3SourceURL,
		JobID:         &jobID,
	}

	if err := h.store.CreateWindowsSnapshot(ctx, snapshot); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to create snapshot record: %v", err))
	}

	// Create job
	job := &types.Job{
		ID:          jobID,
		ClusterID:   snapshotID, // Use snapshot ID as cluster ID for job locking
		JobType:     types.JobTypeCreateWindowsSnapshot,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 1, // Snapshot creation should not retry (too expensive)
		Metadata: types.JobMetadata{
			"snapshot_id": snapshotID,
			"region":      req.Region,
			"version":     req.Version,
			"s3_source":   req.S3SourceURL,
		},
	}

	if err := h.store.Jobs.Create(ctx, nil, job); err != nil {
		// Clean up snapshot record
		_ = h.store.DeleteWindowsSnapshot(ctx, snapshotID)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to create job: %v", err))
	}

	// Create audit log
	userIDStr := user.ID
	audit := &types.WindowsSnapshotAudit{
		ID:         uuid.New().String(),
		SnapshotID: snapshotID,
		Action:     "create_snapshot",
		UserID:     &userIDStr,
		Details: map[string]interface{}{
			"region":  req.Region,
			"version": req.Version,
		},
	}

	_ = h.store.CreateWindowsSnapshotAudit(ctx, audit) // Non-critical

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"snapshot_id": snapshotID,
		"job_id":      jobID,
		"status":      types.WindowsSnapshotStatusCreating,
		"message":     "Snapshot creation job started",
	})
}

// DeleteWindowsSnapshot deletes a snapshot (and triggers EBS snapshot cleanup)
// DELETE /api/v1/windows-snapshots/:id
func (h *WindowsSnapshotHandler) DeleteWindowsSnapshot(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	// Get snapshot
	snapshot, err := h.store.GetWindowsSnapshot(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Snapshot not found")
	}

	// Check if snapshot is in a deletable state
	if snapshot.Status == types.WindowsSnapshotStatusCreating || snapshot.Status == types.WindowsSnapshotStatusValidating {
		return echo.NewHTTPError(http.StatusConflict, "Cannot delete snapshot while creation/validation is in progress")
	}

	// Get current user for audit
	user, ok := c.Get("user").(*types.User)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not found in context")
	}

	// Update status to deleting
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, id, types.WindowsSnapshotStatusDeleting, nil); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to update status: %v", err))
	}

	// TODO: Trigger async job to delete EBS snapshot and SSM parameter
	// For now, just delete the DB record
	// In future: Create DELETE_WINDOWS_SNAPSHOT job

	if err := h.store.DeleteWindowsSnapshot(ctx, id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to delete snapshot: %v", err))
	}

	// Create audit log
	userIDStr := user.ID
	audit := &types.WindowsSnapshotAudit{
		ID:         uuid.New().String(),
		SnapshotID: id,
		Action:     "delete_snapshot",
		UserID:     &userIDStr,
		Details: map[string]interface{}{
			"region":          snapshot.Region,
			"version":         snapshot.Version,
			"ebs_snapshot_id": snapshot.EBSSnapshotID,
		},
	}

	_ = h.store.CreateWindowsSnapshotAudit(ctx, audit) // Non-critical

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Snapshot deleted successfully",
	})
}
