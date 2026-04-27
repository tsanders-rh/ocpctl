package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCPPreflightChecker validates GCP resource availability before cluster creation
type GCPPreflightChecker struct {
	project       string
	region        string
	zone          string
	computeClient *compute.MachineTypesClient
}

// NewGCPPreflightChecker creates a new GCP pre-flight checker
func NewGCPPreflightChecker(ctx context.Context, project, region, zone string) (*GCPPreflightChecker, error) {
	// Check for service account credentials
	credsPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	var opts []option.ClientOption

	if credsPath != "" {
		log.Printf("Using GCP service account credentials from: %s", credsPath)
		opts = append(opts, option.WithCredentialsFile(credsPath))
	} else {
		log.Printf("Using GCP default application credentials")
	}

	// Create compute client
	computeClient, err := compute.NewMachineTypesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create GCP compute client: %w", err)
	}

	return &GCPPreflightChecker{
		project:       project,
		region:        region,
		zone:          zone,
		computeClient: computeClient,
	}, nil
}

// Close closes the GCP client connections
func (c *GCPPreflightChecker) Close() error {
	if c.computeClient != nil {
		return c.computeClient.Close()
	}
	return nil
}

// CheckMachineTypeAvailability validates that required machine types are available in sufficient zones
// for high-availability cluster deployment. Returns error if insufficient availability detected.
func (c *GCPPreflightChecker) CheckMachineTypeAvailability(ctx context.Context, prof *profile.Profile) error {
	// Extract machine types from profile
	machineTypes := c.extractMachineTypes(prof)
	if len(machineTypes) == 0 {
		log.Printf("Pre-flight check: no machine types to validate")
		return nil
	}

	log.Printf("Pre-flight check: validating availability for machine types: %v in region %s",
		machineTypes, c.region)

	// Get zones in the region
	zones, err := c.getRegionZones(ctx)
	if err != nil {
		return fmt.Errorf("get zones for region %s: %w", c.region, err)
	}

	log.Printf("Pre-flight check: found %d zones in region %s: %v",
		len(zones), c.region, zones)

	// Get availability for each machine type
	typeAvailability := make(map[string][]string) // machine type -> available zones
	for _, machineType := range machineTypes {
		availableZones, err := c.checkMachineTypeInZones(ctx, machineType, zones)
		if err != nil {
			return fmt.Errorf("check availability for %s: %w", machineType, err)
		}
		typeAvailability[machineType] = availableZones

		log.Printf("Pre-flight check: %s available in %d zones: %v",
			machineType, len(availableZones), availableZones)
	}

	// Calculate minimum required zones (default to 3 for HA)
	minRequiredZones := 3
	if prof.Compute.ControlPlane != nil && prof.Compute.ControlPlane.Replicas > minRequiredZones {
		minRequiredZones = prof.Compute.ControlPlane.Replicas
	}

	// Find zones where ALL machine types are available (co-location)
	colocatedZones := c.findColocatedZones(typeAvailability)

	log.Printf("Pre-flight check: found %d zones with all machine types co-located: %v",
		len(colocatedZones), colocatedZones)

	// Validate sufficient co-located zones
	if len(colocatedZones) < minRequiredZones {
		return c.formatInsufficientAvailabilityError(machineTypes, typeAvailability, colocatedZones, minRequiredZones)
	}

	log.Printf("Pre-flight check: ✓ Sufficient availability (%d zones) for %d-zone HA cluster",
		len(colocatedZones), minRequiredZones)

	// Warn if limited availability (available but not in all zones)
	if len(colocatedZones) < len(zones) {
		c.logLimitedAvailabilityWarning(machineTypes, colocatedZones, len(zones))
	}

	return nil
}

// extractMachineTypes extracts unique machine types from profile compute configuration
func (c *GCPPreflightChecker) extractMachineTypes(prof *profile.Profile) []string {
	typeSet := make(map[string]bool)

	// GCP-specific control plane machine type
	if prof.PlatformConfig.GCP != nil && prof.PlatformConfig.GCP.ControlPlane != nil && prof.PlatformConfig.GCP.ControlPlane.MachineType != "" {
		typeSet[prof.PlatformConfig.GCP.ControlPlane.MachineType] = true
	}

	// GCP-specific compute machine type
	if prof.PlatformConfig.GCP != nil && prof.PlatformConfig.GCP.Compute != nil && prof.PlatformConfig.GCP.Compute.MachineType != "" {
		typeSet[prof.PlatformConfig.GCP.Compute.MachineType] = true
	}

	// GKE node pool machine types
	if prof.PlatformConfig.GKE != nil && len(prof.PlatformConfig.GKE.NodePools) > 0 {
		for _, pool := range prof.PlatformConfig.GKE.NodePools {
			if pool.MachineType != "" {
				typeSet[pool.MachineType] = true
			}
		}
	}

	// Convert to sorted list for consistent ordering
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	return types
}

// getRegionZones retrieves all zones in a GCP region
func (c *GCPPreflightChecker) getRegionZones(ctx context.Context) ([]string, error) {
	// Create zones client
	zonesClient, err := compute.NewZonesRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create zones client: %w", err)
	}
	defer zonesClient.Close()

	// List zones in the project
	req := &computepb.ListZonesRequest{
		Project: c.project,
	}

	it := zonesClient.List(ctx, req)

	zones := []string{}
	for {
		zone, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterate zones: %w", err)
		}

		// Only include zones in the specified region
		if zone.Region != nil && strings.HasSuffix(*zone.Region, c.region) {
			if zone.Name != nil {
				zones = append(zones, *zone.Name)
			}
		}
	}

	sort.Strings(zones)
	return zones, nil
}

// checkMachineTypeInZones checks if a machine type is available in the specified zones
func (c *GCPPreflightChecker) checkMachineTypeInZones(ctx context.Context, machineType string, zones []string) ([]string, error) {
	availableZones := []string{}

	for _, zone := range zones {
		// Get machine type details for this zone
		req := &computepb.GetMachineTypeRequest{
			Project:     c.project,
			Zone:        zone,
			MachineType: machineType,
		}

		_, err := c.computeClient.Get(ctx, req)
		if err != nil {
			// Machine type not available in this zone (404 or other error)
			log.Printf("Machine type %s not available in zone %s: %v", machineType, zone, err)
			continue
		}

		// Machine type is available in this zone
		availableZones = append(availableZones, zone)
	}

	return availableZones, nil
}

// findColocatedZones finds zones where all machine types are available
func (c *GCPPreflightChecker) findColocatedZones(typeAvailability map[string][]string) []string {
	if len(typeAvailability) == 0 {
		return []string{}
	}

	// Start with zones from first machine type
	var firstZones []string
	for _, zones := range typeAvailability {
		firstZones = zones
		break
	}

	// Find intersection of all zone lists
	colocated := make(map[string]bool)
	for _, zone := range firstZones {
		colocated[zone] = true
	}

	// For each machine type, remove zones where it's NOT available
	for _, zones := range typeAvailability {
		zoneSet := make(map[string]bool)
		for _, zone := range zones {
			zoneSet[zone] = true
		}

		// Remove zones not in this machine type's availability
		for zone := range colocated {
			if !zoneSet[zone] {
				delete(colocated, zone)
			}
		}
	}

	// Convert back to sorted list
	result := make([]string, 0, len(colocated))
	for zone := range colocated {
		result = append(result, zone)
	}
	sort.Strings(result)

	return result
}

// formatInsufficientAvailabilityError creates a detailed error message for capacity failures
func (c *GCPPreflightChecker) formatInsufficientAvailabilityError(
	machineTypes []string,
	typeAvailability map[string][]string,
	colocatedZones []string,
	minRequiredZones int,
) error {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("❌ GCP Capacity Pre-flight Check Failed\n\n"))
	msg.WriteString(fmt.Sprintf("This cluster requires %d zones for high-availability deployment,\n", minRequiredZones))
	msg.WriteString(fmt.Sprintf("but the required machine types are only co-located in %d zone(s).\n\n", len(colocatedZones)))

	msg.WriteString("Machine Type Availability:\n")
	for _, machineType := range machineTypes {
		zones := typeAvailability[machineType]
		msg.WriteString(fmt.Sprintf("  • %s: %d zones (%s)\n",
			machineType, len(zones), strings.Join(zones, ", ")))
	}

	msg.WriteString(fmt.Sprintf("\nZones where ALL machine types are available: "))
	if len(colocatedZones) == 0 {
		msg.WriteString("NONE\n")
	} else {
		msg.WriteString(fmt.Sprintf("%s\n", strings.Join(colocatedZones, ", ")))
	}

	msg.WriteString("\nRecommendations:\n")
	msg.WriteString("  1. Choose a different GCP region with better availability\n")
	msg.WriteString("  2. Select a different cluster profile with more common machine types\n")
	msg.WriteString("  3. Contact Google Cloud Support to request quota increase in your preferred region\n")

	return fmt.Errorf("%s", msg.String())
}

// logLimitedAvailabilityWarning logs a warning when machine types have limited (but sufficient) availability
func (c *GCPPreflightChecker) logLimitedAvailabilityWarning(machineTypes []string, colocatedZones []string, totalZones int) {
	log.Printf("⚠️  Limited Availability Warning")
	log.Printf("")
	log.Printf("The required machine types (%s) are only available in %d out of %d zones:",
		strings.Join(machineTypes, ", "), len(colocatedZones), totalZones)
	log.Printf("  Available zones: %s", strings.Join(colocatedZones, ", "))
	log.Printf("")
	log.Printf("The installer will automatically select from these zones.")
	log.Printf("This is sufficient for HA deployment, but provides less flexibility for zone selection.")
}

// VerifyAuthentication checks if GCP credentials are properly configured
func VerifyGCPAuthentication(ctx context.Context, project string) error {
	// Try to create a compute client to verify credentials
	client, err := compute.NewMachineTypesRESTClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create GCP client (check GOOGLE_APPLICATION_CREDENTIALS): %w", err)
	}
	defer client.Close()

	// Try to list machine types as a simple API call to verify auth
	req := &computepb.AggregatedListMachineTypesRequest{
		Project:    project,
		MaxResults: func() *uint32 { v := uint32(1); return &v }(), // Just get 1 result to verify auth
	}

	it := client.AggregatedList(ctx, req)
	_, err = it.Next()
	if err != nil && err != iterator.Done {
		return fmt.Errorf("GCP authentication failed (verify project %s access): %w", project, err)
	}

	log.Printf("✓ GCP authentication verified for project: %s", project)
	return nil
}

// ActivateServiceAccount activates a GCP service account using a JSON key file
// This is called when GOOGLE_APPLICATION_CREDENTIALS is set
func ActivateServiceAccount(ctx context.Context, keyFilePath string) error {
	if keyFilePath == "" {
		return fmt.Errorf("service account key file path is empty")
	}

	// Verify the file exists
	if _, err := os.Stat(keyFilePath); os.IsNotExist(err) {
		return fmt.Errorf("service account key file not found: %s", keyFilePath)
	}

	// Set environment variable for GCP SDK
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", keyFilePath)

	log.Printf("✓ GCP service account activated: %s", keyFilePath)
	return nil
}
