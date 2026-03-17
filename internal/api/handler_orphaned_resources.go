package api

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// OrphanedResourceHandler handles orphaned resource API endpoints
type OrphanedResourceHandler struct {
	store  *store.Store
	policy *policy.Engine
}

// NewOrphanedResourceHandler creates a new orphaned resource handler
func NewOrphanedResourceHandler(s *store.Store, p *policy.Engine) *OrphanedResourceHandler {
	return &OrphanedResourceHandler{
		store:  s,
		policy: p,
	}
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
		return ErrorInternal(c, "Failed to list orphaned resources")
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
		return ErrorInternal(c, "Failed to get statistics")
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
		return ErrorInternal(c, "Failed to mark resource as resolved")
	}

	// Get updated resource
	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return ErrorInternal(c, "Failed to get updated resource")
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
		return ErrorInternal(c, "Failed to mark resource as ignored")
	}

	// Get updated resource
	resource, err := h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return ErrorInternal(c, "Failed to get updated resource")
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

	// Only support HostedZone and DNSRecord deletion
	if resource.ResourceType != types.OrphanedResourceTypeHostedZone &&
	   resource.ResourceType != types.OrphanedResourceTypeDNSRecord {
		return ErrorBadRequest(c, "Only HostedZone and DNSRecord resources can be deleted via API. Other resource types must be deleted manually in AWS Console.")
	}

	// Delete the resource based on type
	if resource.ResourceType == types.OrphanedResourceTypeHostedZone {
		err = h.deleteHostedZone(c.Request().Context(), resource.ResourceID, resource.ResourceName)
		if err != nil {
			log.Printf("Failed to delete hosted zone %s: %v", resource.ResourceName, err)
			return ErrorInternal(c, fmt.Sprintf("Failed to delete hosted zone: %v", err))
		}
	} else if resource.ResourceType == types.OrphanedResourceTypeDNSRecord {
		err = h.deleteDNSRecord(c.Request().Context(), resource.ResourceName)
		if err != nil {
			log.Printf("Failed to delete DNS record %s: %v", resource.ResourceName, err)
			return ErrorInternal(c, fmt.Sprintf("Failed to delete DNS record: %v", err))
		}
	}

	// Mark as resolved
	notes := fmt.Sprintf("Automatically deleted via API by %s", userEmail)
	err = h.store.OrphanedResources.MarkResolved(c.Request().Context(), id, userEmail, notes)
	if err != nil {
		return ErrorInternal(c, "Resource deleted but failed to update database")
	}

	// Get updated resource
	resource, err = h.store.OrphanedResources.GetByID(c.Request().Context(), id)
	if err != nil {
		return ErrorInternal(c, "Resource deleted but failed to get updated status")
	}

	return c.JSON(200, resource)
}

// deleteHostedZone deletes a Route53 hosted zone and all its records
func (h *OrphanedResourceHandler) deleteHostedZone(ctx context.Context, hostedZoneID, hostedZoneName string) error {
	// Load AWS config with region (Route53 is global but SDK requires a region)
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	route53Client := route53.NewFromConfig(cfg)

	// List all record sets
	listResult, err := route53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZoneID),
	})
	if err != nil {
		// Check if the hosted zone doesn't exist (already deleted)
		if strings.Contains(err.Error(), "NoSuchHostedZone") {
			log.Printf("Hosted zone %s (%s) not found - assuming already deleted", hostedZoneName, hostedZoneID)
			return nil // Treat as success since the end result (zone deleted) is achieved
		}
		return fmt.Errorf("list record sets: %w", err)
	}

	// Delete all records except NS and SOA (which are required and will be deleted with the zone)
	var changes []route53types.Change
	for _, record := range listResult.ResourceRecordSets {
		recordType := record.Type
		recordName := aws.ToString(record.Name)

		// Skip NS and SOA records at the zone apex - these will be deleted automatically
		if (recordType == route53types.RRTypeNs || recordType == route53types.RRTypeSoa) &&
		   strings.TrimSuffix(recordName, ".") == strings.TrimSuffix(hostedZoneName, ".") {
			continue
		}

		// Add delete change for this record
		changes = append(changes, route53types.Change{
			Action:            route53types.ChangeActionDelete,
			ResourceRecordSet: &record,
		})
	}

	// Execute the changes if there are any records to delete
	if len(changes) > 0 {
		_, err = route53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(hostedZoneID),
			ChangeBatch: &route53types.ChangeBatch{
				Changes: changes,
				Comment: aws.String("Deleting all records before zone deletion via ocpctl"),
			},
		})
		if err != nil {
			return fmt.Errorf("delete record sets: %w", err)
		}
		log.Printf("Deleted %d record sets from hosted zone %s", len(changes), hostedZoneName)
	}

	// Now delete the hosted zone itself
	_, err = route53Client.DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{
		Id: aws.String(hostedZoneID),
	})
	if err != nil {
		// Check if the hosted zone doesn't exist (already deleted)
		if strings.Contains(err.Error(), "NoSuchHostedZone") {
			log.Printf("Hosted zone %s (%s) not found during deletion - assuming already deleted", hostedZoneName, hostedZoneID)
			return nil // Treat as success since the end result (zone deleted) is achieved
		}
		return fmt.Errorf("delete hosted zone: %w", err)
	}

	log.Printf("Successfully deleted hosted zone %s (%s)", hostedZoneName, hostedZoneID)
	return nil
}

// deleteDNSRecord deletes a specific DNS record from Route53
func (h *OrphanedResourceHandler) deleteDNSRecord(ctx context.Context, recordName string) error {
	// Load AWS config with region (Route53 is global but SDK requires a region)
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	route53Client := route53.NewFromConfig(cfg)

	// Normalize record name (ensure it ends with a dot)
	if !strings.HasSuffix(recordName, ".") {
		recordName = recordName + "."
	}

	// Find which hosted zone contains this record
	// List all hosted zones
	zonesResult, err := route53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{})
	if err != nil {
		return fmt.Errorf("list hosted zones: %w", err)
	}

	var targetZoneID string
	var targetZoneName string

	// Find the zone that this record belongs to
	// Check if the record name ends with the zone name
	for _, zone := range zonesResult.HostedZones {
		zoneName := aws.ToString(zone.Name)
		if strings.HasSuffix(recordName, zoneName) {
			// Found a potential match - use the most specific (longest) zone name
			if targetZoneName == "" || len(zoneName) > len(targetZoneName) {
				targetZoneID = aws.ToString(zone.Id)
				targetZoneName = zoneName
			}
		}
	}

	if targetZoneID == "" {
		return fmt.Errorf("could not find hosted zone for record %s", recordName)
	}

	log.Printf("Found record %s in hosted zone %s (%s)", recordName, targetZoneName, targetZoneID)

	// List records in the zone to find the exact match
	listResult, err := route53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(targetZoneID),
	})
	if err != nil {
		return fmt.Errorf("list record sets: %w", err)
	}

	// Find the exact record to delete
	var recordToDelete *route53types.ResourceRecordSet
	for _, record := range listResult.ResourceRecordSets {
		if aws.ToString(record.Name) == recordName {
			recordToDelete = &record
			break
		}
	}

	if recordToDelete == nil {
		// Record doesn't exist - it was probably already deleted manually
		log.Printf("DNS record %s not found in zone %s - assuming already deleted", recordName, targetZoneName)
		return nil // Treat as success since the end result (record deleted) is achieved
	}

	// Delete the record
	_, err = route53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(targetZoneID),
		ChangeBatch: &route53types.ChangeBatch{
			Changes: []route53types.Change{
				{
					Action:            route53types.ChangeActionDelete,
					ResourceRecordSet: recordToDelete,
				},
			},
			Comment: aws.String(fmt.Sprintf("Deleting orphaned DNS record via ocpctl: %s", recordName)),
		},
	})
	if err != nil {
		return fmt.Errorf("delete DNS record: %w", err)
	}

	log.Printf("Successfully deleted DNS record %s from zone %s", recordName, targetZoneName)
	return nil
}
