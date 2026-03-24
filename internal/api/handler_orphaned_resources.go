package api

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
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

	// Delete the resource based on type
	switch resource.ResourceType {
	case types.OrphanedResourceTypeHostedZone:
		err = h.deleteHostedZone(c.Request().Context(), resource.ResourceID, resource.ResourceName)
	case types.OrphanedResourceTypeDNSRecord:
		err = h.deleteDNSRecord(c.Request().Context(), resource.ResourceName)
	case types.OrphanedResourceTypeEBSVolume:
		err = h.deleteEBSVolume(c.Request().Context(), resource.ResourceID, resource.Region)
	case types.OrphanedResourceTypeElasticIP:
		err = h.deleteElasticIP(c.Request().Context(), resource.ResourceID, resource.Region)
	case types.OrphanedResourceTypeIAMRole:
		err = h.deleteIAMRole(c.Request().Context(), resource.ResourceName)
	case types.OrphanedResourceTypeOIDCProvider:
		err = h.deleteOIDCProvider(c.Request().Context(), resource.ResourceID)
	case types.OrphanedResourceTypeCloudWatchLogGroup:
		err = h.deleteCloudWatchLogGroup(c.Request().Context(), resource.ResourceID, resource.Region)
	case types.OrphanedResourceTypeVPC:
		return ErrorBadRequest(c, "VPC deletion not supported - must delete all dependent resources first (subnets, route tables, etc)")
	case types.OrphanedResourceTypeLoadBalancer:
		return ErrorBadRequest(c, "LoadBalancer deletion not supported - delete via AWS Console")
	case types.OrphanedResourceTypeEC2Instance:
		return ErrorBadRequest(c, "EC2Instance deletion not supported - delete via AWS Console")
	default:
		return ErrorBadRequest(c, fmt.Sprintf("Deletion not supported for resource type: %s", resource.ResourceType))
	}

	if err != nil {
		log.Printf("Failed to delete %s %s: %v", resource.ResourceType, resource.ResourceName, err)
		return ErrorInternal(c, fmt.Sprintf("Failed to delete resource: %v", err))
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

// deleteEBSVolume deletes an EBS volume
func (h *OrphanedResourceHandler) deleteEBSVolume(ctx context.Context, volumeID, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Delete the volume
	_, err = ec2Client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{
		VolumeId: aws.String(volumeID),
	})
	if err != nil {
		// Check if volume doesn't exist (already deleted)
		if strings.Contains(err.Error(), "InvalidVolume.NotFound") {
			log.Printf("EBS volume %s not found - assuming already deleted", volumeID)
			return nil
		}
		return fmt.Errorf("delete EBS volume: %w", err)
	}

	log.Printf("Successfully deleted EBS volume %s", volumeID)
	return nil
}

// deleteElasticIP releases an Elastic IP
func (h *OrphanedResourceHandler) deleteElasticIP(ctx context.Context, allocationID, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// First, check if the EIP is associated with anything
	describeResult, err := ec2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		AllocationIds: []string{allocationID},
	})
	if err != nil {
		// Check if EIP doesn't exist (already deleted)
		if strings.Contains(err.Error(), "InvalidAllocationID.NotFound") {
			log.Printf("Elastic IP %s not found - assuming already deleted", allocationID)
			return nil
		}
		return fmt.Errorf("describe Elastic IP: %w", err)
	}

	if len(describeResult.Addresses) == 0 {
		log.Printf("Elastic IP %s not found - assuming already deleted", allocationID)
		return nil
	}

	address := describeResult.Addresses[0]

	// If the EIP is associated, check if it's attached to a NAT Gateway
	if address.AssociationId != nil {
		// Check if the network interface is a NAT Gateway interface
		if address.NetworkInterfaceId != nil {
			log.Printf("Checking if EIP %s is attached to NAT Gateway (interface: %s)", allocationID, *address.NetworkInterfaceId)
			niResult, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
				NetworkInterfaceIds: []string{*address.NetworkInterfaceId},
			})
			if err != nil {
				log.Printf("Warning: failed to describe network interface %s: %v", *address.NetworkInterfaceId, err)
				// Continue anyway - if we can't check, we'll try to disassociate and let AWS return the proper error
			} else if len(niResult.NetworkInterfaces) > 0 {
				ni := niResult.NetworkInterfaces[0]
				log.Printf("Network interface type: '%v' (comparing to '%v')", ni.InterfaceType, ec2types.NetworkInterfaceTypeNatGateway)
				log.Printf("Type comparison result: %v", ni.InterfaceType == ec2types.NetworkInterfaceTypeNatGateway)
				if ni.InterfaceType == ec2types.NetworkInterfaceTypeNatGateway {
					log.Printf("NAT Gateway detected! Extracting ID...")
					// Extract NAT Gateway ID from description
					natGatewayID := ""
					if ni.Description != nil && strings.Contains(*ni.Description, "nat-") {
						parts := strings.Split(*ni.Description, " ")
						if len(parts) > 3 {
							natGatewayID = parts[len(parts)-1]
						}
					}
					log.Printf("Returning error for NAT Gateway %s", natGatewayID)
					return fmt.Errorf("EIP is attached to NAT Gateway %s - cannot disassociate directly. Delete the NAT Gateway first or manually delete via AWS Console", natGatewayID)
				}
				log.Printf("Not a NAT Gateway (interface type didn't match)")
			}
		}

		// Not a NAT Gateway - proceed with normal disassociation
		log.Printf("Disassociating Elastic IP %s (association: %s)", allocationID, *address.AssociationId)
		_, err = ec2Client.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{
			AssociationId: address.AssociationId,
		})
		if err != nil {
			return fmt.Errorf("disassociate Elastic IP: %w", err)
		}
	}

	// Now release the Elastic IP
	_, err = ec2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
		AllocationId: aws.String(allocationID),
	})
	if err != nil {
		// Check if EIP doesn't exist (already deleted)
		if strings.Contains(err.Error(), "InvalidAllocationID.NotFound") {
			log.Printf("Elastic IP %s not found - assuming already deleted", allocationID)
			return nil
		}
		return fmt.Errorf("release Elastic IP: %w", err)
	}

	log.Printf("Successfully released Elastic IP %s", allocationID)
	return nil
}

// deleteIAMRole deletes an IAM role and its attached policies
func (h *OrphanedResourceHandler) deleteIAMRole(ctx context.Context, roleName string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// List and detach all attached policies
	listPoliciesResult, err := iamClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		// Check if role doesn't exist
		if strings.Contains(err.Error(), "NoSuchEntity") {
			log.Printf("IAM role %s not found - assuming already deleted", roleName)
			return nil
		}
		return fmt.Errorf("list attached policies: %w", err)
	}

	for _, policy := range listPoliciesResult.AttachedPolicies {
		_, err = iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: policy.PolicyArn,
		})
		if err != nil {
			return fmt.Errorf("detach policy %s: %w", aws.ToString(policy.PolicyArn), err)
		}
	}

	// List and delete all inline policies
	listInlinePoliciesResult, err := iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("list inline policies: %w", err)
	}

	for _, policyName := range listInlinePoliciesResult.PolicyNames {
		_, err = iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(policyName),
		})
		if err != nil {
			return fmt.Errorf("delete inline policy %s: %w", policyName, err)
		}
	}

	// Delete the role
	_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}

	log.Printf("Successfully deleted IAM role %s", roleName)
	return nil
}

// deleteOIDCProvider deletes an OIDC provider
func (h *OrphanedResourceHandler) deleteOIDCProvider(ctx context.Context, providerArn string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Delete the OIDC provider
	_, err = iamClient.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerArn),
	})
	if err != nil {
		// Check if provider doesn't exist
		if strings.Contains(err.Error(), "NoSuchEntity") {
			log.Printf("OIDC provider %s not found - assuming already deleted", providerArn)
			return nil
		}
		return fmt.Errorf("delete OIDC provider: %w", err)
	}

	log.Printf("Successfully deleted OIDC provider %s", providerArn)
	return nil
}

// deleteCloudWatchLogGroup deletes a CloudWatch log group
func (h *OrphanedResourceHandler) deleteCloudWatchLogGroup(ctx context.Context, logGroupName, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	cwlClient := cloudwatchlogs.NewFromConfig(cfg)

	// Delete the log group
	_, err = cwlClient.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(logGroupName),
	})
	if err != nil {
		// Check if log group doesn't exist
		if strings.Contains(err.Error(), "ResourceNotFoundException") {
			log.Printf("CloudWatch log group %s not found - assuming already deleted", logGroupName)
			return nil
		}
		return fmt.Errorf("delete log group: %w", err)
	}

	log.Printf("Successfully deleted CloudWatch log group %s", logGroupName)
	return nil
}
