package worker

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// AWSPreflightChecker validates AWS resource availability before cluster creation
type AWSPreflightChecker struct {
	region string
	ec2Client *ec2.Client
}

// NewAWSPreflightChecker creates a new AWS pre-flight checker
func NewAWSPreflightChecker(ctx context.Context, region string) (*AWSPreflightChecker, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return &AWSPreflightChecker{
		region:    region,
		ec2Client: ec2.NewFromConfig(cfg),
	}, nil
}

// CheckInstanceTypeAvailability validates that required instance types are available in sufficient zones
// for high-availability cluster deployment. Returns error if insufficient availability detected.
func (c *AWSPreflightChecker) CheckInstanceTypeAvailability(ctx context.Context, prof *profile.Profile) error {
	// Extract instance types from profile
	instanceTypes := c.extractInstanceTypes(prof)
	if len(instanceTypes) == 0 {
		log.Printf("Pre-flight check: no instance types to validate")
		return nil
	}

	log.Printf("Pre-flight check: validating availability for instance types: %v in region %s",
		instanceTypes, c.region)

	// Get availability for each instance type
	typeAvailability := make(map[string][]string) // instance type -> available zones
	for _, instanceType := range instanceTypes {
		zones, err := c.getAvailableZones(ctx, instanceType)
		if err != nil {
			return fmt.Errorf("check availability for %s: %w", instanceType, err)
		}
		typeAvailability[instanceType] = zones

		log.Printf("Pre-flight check: %s available in %d zones: %v",
			instanceType, len(zones), zones)
	}

	// Calculate minimum required zones (default to 3 for HA)
	minRequiredZones := 3
	if prof.Compute.ControlPlane != nil && prof.Compute.ControlPlane.Replicas > minRequiredZones {
		minRequiredZones = prof.Compute.ControlPlane.Replicas
	}

	// Find zones where ALL instance types are available (co-location)
	colocatedZones := c.findColocatedZones(typeAvailability)

	log.Printf("Pre-flight check: found %d zones with all instance types co-located: %v",
		len(colocatedZones), colocatedZones)

	// Validate sufficient co-located zones
	if len(colocatedZones) < minRequiredZones {
		return c.formatInsufficientAvailabilityError(instanceTypes, typeAvailability, colocatedZones, minRequiredZones)
	}

	log.Printf("Pre-flight check: ✓ Sufficient availability (%d zones) for %d-zone HA cluster",
		len(colocatedZones), minRequiredZones)

	// Warn if limited availability (available but not in all zones)
	if len(colocatedZones) < 6 {
		c.logLimitedAvailabilityWarning(instanceTypes, colocatedZones)
	}

	return nil
}

// extractInstanceTypes extracts unique instance types from profile compute configuration
func (c *AWSPreflightChecker) extractInstanceTypes(prof *profile.Profile) []string {
	typeSet := make(map[string]bool)

	// Control plane instance type
	if prof.Compute.ControlPlane != nil && prof.Compute.ControlPlane.InstanceType != "" {
		typeSet[prof.Compute.ControlPlane.InstanceType] = true
	}

	// Worker instance type
	if prof.Compute.Workers != nil && prof.Compute.Workers.InstanceType != "" {
		typeSet[prof.Compute.Workers.InstanceType] = true
	}

	// Convert to sorted list for consistent ordering
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)

	return types
}

// getAvailableZones queries AWS EC2 API for zones where the instance type is available
func (c *AWSPreflightChecker) getAvailableZones(ctx context.Context, instanceType string) ([]string, error) {
	input := &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: ec2types.LocationTypeAvailabilityZone,
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("instance-type"),
				Values: []string{instanceType},
			},
		},
	}

	paginator := ec2.NewDescribeInstanceTypeOfferingsPaginator(c.ec2Client, input)

	zones := []string{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe instance type offerings: %w", err)
		}

		for _, offering := range page.InstanceTypeOfferings {
			if offering.Location != nil {
				zones = append(zones, *offering.Location)
			}
		}
	}

	sort.Strings(zones)
	return zones, nil
}

// findColocatedZones finds zones where all instance types are available
func (c *AWSPreflightChecker) findColocatedZones(typeAvailability map[string][]string) []string {
	if len(typeAvailability) == 0 {
		return []string{}
	}

	// Start with zones from first instance type
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

	// For each instance type, remove zones where it's NOT available
	for _, zones := range typeAvailability {
		zoneSet := make(map[string]bool)
		for _, zone := range zones {
			zoneSet[zone] = true
		}

		// Remove zones not in this instance type's availability
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
func (c *AWSPreflightChecker) formatInsufficientAvailabilityError(
	instanceTypes []string,
	typeAvailability map[string][]string,
	colocatedZones []string,
	minRequiredZones int,
) error {
	var msg strings.Builder

	msg.WriteString(fmt.Sprintf("❌ AWS Capacity Pre-flight Check Failed\n\n"))
	msg.WriteString(fmt.Sprintf("This cluster requires %d availability zones for high-availability deployment,\n", minRequiredZones))
	msg.WriteString(fmt.Sprintf("but the required instance types are only co-located in %d zone(s).\n\n", len(colocatedZones)))

	msg.WriteString("Instance Type Availability:\n")
	for _, instanceType := range instanceTypes {
		zones := typeAvailability[instanceType]
		msg.WriteString(fmt.Sprintf("  • %s: %d zones (%s)\n",
			instanceType, len(zones), strings.Join(zones, ", ")))
	}

	msg.WriteString(fmt.Sprintf("\nZones where ALL instance types are available: "))
	if len(colocatedZones) == 0 {
		msg.WriteString("NONE\n")
	} else {
		msg.WriteString(fmt.Sprintf("%s\n", strings.Join(colocatedZones, ", ")))
	}

	msg.WriteString("\nRecommendations:\n")
	msg.WriteString("  1. Choose a different AWS region with better availability\n")
	msg.WriteString("  2. Select a different cluster profile with more common instance types\n")
	msg.WriteString("  3. Contact AWS Support to request capacity in your preferred region\n")

	return fmt.Errorf("%s", msg.String())
}

// logLimitedAvailabilityWarning logs a warning when instance types have limited (but sufficient) availability
func (c *AWSPreflightChecker) logLimitedAvailabilityWarning(instanceTypes []string, colocatedZones []string) {
	log.Printf("⚠️  Limited Availability Warning")
	log.Printf("")
	log.Printf("The required instance types (%s) are only available in %d zones:",
		strings.Join(instanceTypes, ", "), len(colocatedZones))
	log.Printf("  Available zones: %s", strings.Join(colocatedZones, ", "))
	log.Printf("")
	log.Printf("The OpenShift installer will automatically select from these zones.")
	log.Printf("This is sufficient for HA deployment, but provides less flexibility for zone selection.")
}
