package profile

import (
	"fmt"
)

// IBMCloudInstallConfigData extends InstallConfigData with IBM Cloud-specific fields
type IBMCloudInstallConfigData struct {
	InstallConfigData

	// IBM Cloud platform fields
	ResourceGroupName string
	VPCName           string
	ControlPlaneSubnets []string
	ComputeSubnets      []string

	// Boot volume encryption
	BootVolumeEncryptionKey string

	// Dedicated hosts (for compliance)
	DedicatedHosts []IBMCloudDedicatedHost

	// Zones for worker distribution
	WorkerZones []string

	// Publish strategy (External or Internal)
	Publish string
}

// IBMCloudDedicatedHost represents a dedicated host configuration
type IBMCloudDedicatedHost struct {
	Name    string
	Profile string
}

// ValidateIBMCloudConfig validates IBM Cloud-specific configuration
func ValidateIBMCloudConfig(data *IBMCloudInstallConfigData) error {
	// Validate region
	validRegions := map[string]bool{
		"us-south": true,
		"us-east":  true,
		"eu-de":    true,
		"eu-gb":    true,
		"jp-tok":   true,
		"jp-osa":   true,
		"au-syd":   true,
		"ca-tor":   true,
		"br-sao":   true,
	}

	if !validRegions[data.Region] {
		return fmt.Errorf("invalid IBM Cloud region: %s", data.Region)
	}

	// Validate instance types
	if err := validateIBMCloudInstanceType(data.ControlPlaneType); err != nil {
		return fmt.Errorf("invalid control plane instance type: %w", err)
	}

	if data.WorkerReplicas > 0 {
		if err := validateIBMCloudInstanceType(data.WorkerType); err != nil {
			return fmt.Errorf("invalid worker instance type: %w", err)
		}
	}

	// If using existing VPC, subnets must be specified
	if data.VPCName != "" {
		if len(data.ControlPlaneSubnets) == 0 || len(data.ComputeSubnets) == 0 {
			return fmt.Errorf("when using existing VPC, both controlPlaneSubnets and computeSubnets must be specified")
		}
	}

	// Validate publish strategy
	if data.Publish != "" && data.Publish != "External" && data.Publish != "Internal" {
		return fmt.Errorf("invalid publish strategy: %s (must be External or Internal)", data.Publish)
	}

	return nil
}

// validateIBMCloudInstanceType validates IBM Cloud instance type
func validateIBMCloudInstanceType(instanceType string) error {
	// IBM Cloud instance type format: {family}{version}-{vcpu}x{memory}
	// Examples: bx2-2x8, bx2-4x16, bx2-8x32, cx2-2x4, mx2-2x16

	validFamilies := map[string]bool{
		"bx2": true, // Balanced
		"cx2": true, // Compute
		"mx2": true, // Memory
		"bx3": true, // Balanced Gen 3
		"cx3": true, // Compute Gen 3
		"mx3": true, // Memory Gen 3
		"vx2": true, // Very High Memory
		"ux2": true, // Ultra High Memory
	}

	// Common instance types
	validTypes := map[string]bool{
		// Balanced Gen 2
		"bx2-2x8":    true,
		"bx2-4x16":   true,
		"bx2-8x32":   true,
		"bx2-16x64":  true,
		"bx2-32x128": true,
		"bx2-48x192": true,

		// Compute Optimized Gen 2
		"cx2-2x4":    true,
		"cx2-4x8":    true,
		"cx2-8x16":   true,
		"cx2-16x32":  true,
		"cx2-32x64":  true,

		// Memory Optimized Gen 2
		"mx2-2x16":   true,
		"mx2-4x32":   true,
		"mx2-8x64":   true,
		"mx2-16x128": true,
		"mx2-32x256": true,

		// Balanced Gen 3
		"bx3-2x10":   true,
		"bx3-4x20":   true,
		"bx3-8x40":   true,
		"bx3-16x80":  true,

		// Very High Memory
		"vx2-2x28":   true,
		"vx2-4x56":   true,
		"vx2-8x112":  true,

		// Ultra High Memory
		"ux2-2x56":   true,
		"ux2-4x112":  true,
		"ux2-8x224":  true,
	}

	if !validTypes[instanceType] {
		// Check if family is valid (for newer instance types not in our list)
		family := instanceType[:3]
		if !validFamilies[family] {
			return fmt.Errorf("invalid instance type %q: family %q not recognized", instanceType, family)
		}
		// Family is valid, assume instance type is valid (may be newer)
	}

	return nil
}

// GetIBMCloudRegionDisplayName returns a human-readable region name
func GetIBMCloudRegionDisplayName(region string) string {
	regionNames := map[string]string{
		"us-south": "Dallas (US South)",
		"us-east":  "Washington DC (US East)",
		"eu-de":    "Frankfurt (Europe Germany)",
		"eu-gb":    "London (Europe UK)",
		"jp-tok":   "Tokyo (Japan)",
		"jp-osa":   "Osaka (Japan)",
		"au-syd":   "Sydney (Australia)",
		"ca-tor":   "Toronto (Canada)",
		"br-sao":   "São Paulo (Brazil)",
	}

	if name, ok := regionNames[region]; ok {
		return name
	}
	return region
}

// GetIBMCloudInstanceTypeSpecs returns vCPU and memory specs for an instance type
func GetIBMCloudInstanceTypeSpecs(instanceType string) (vcpu int, memoryGB int, err error) {
	// Parse instance type: {family}-{vcpu}x{memory}
	// Example: bx2-4x16 = 4 vCPU, 16 GB RAM

	var family string
	_, err = fmt.Sscanf(instanceType, "%3s-%dx%d", &family, &vcpu, &memoryGB)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid instance type format: %s", instanceType)
	}

	return vcpu, memoryGB, nil
}
