package main

import (
	"fmt"
	"log"

	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// This example demonstrates complete profile and policy usage
func main() {
	fmt.Println("=== OpenShift Cluster Profile & Policy Example ===\n")

	// Step 1: Load profiles from YAML files
	fmt.Println("1. Loading profiles...")
	loader := profile.NewLoader("../internal/profile/definitions")

	profiles, err := loader.LoadAll()
	if err != nil {
		log.Fatal("Failed to load profiles:", err)
	}

	fmt.Printf("   Loaded %d profiles\n", len(profiles))
	for _, prof := range profiles {
		fmt.Printf("   - %s (%s, enabled=%v)\n", prof.Name, prof.Platform, prof.Enabled)
	}
	fmt.Println()

	// Step 2: Create profile registry for fast lookups
	fmt.Println("2. Creating profile registry...")
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatal("Failed to create registry:", err)
	}

	fmt.Printf("   Registry contains %d profiles (%d enabled)\n",
		registry.Count(), registry.CountEnabled())
	fmt.Println()

	// Step 3: Get a specific profile
	fmt.Println("3. Getting aws-minimal-test profile...")
	prof, err := registry.Get("aws-minimal-test")
	if err != nil {
		log.Fatal("Failed to get profile:", err)
	}

	fmt.Printf("   Profile: %s\n", prof.DisplayName)
	fmt.Printf("   Platform: %s\n", prof.Platform)
	fmt.Printf("   Control Plane: %d x %s (schedulable=%v)\n",
		prof.Compute.ControlPlane.Replicas,
		prof.Compute.ControlPlane.InstanceType,
		prof.Compute.ControlPlane.Schedulable)
	fmt.Printf("   Workers: %d x %s\n",
		prof.Compute.Workers.Replicas,
		prof.Compute.Workers.InstanceType)
	fmt.Printf("   Max TTL: %d hours\n", prof.Lifecycle.MaxTTLHours)
	fmt.Printf("   Versions: %v\n", prof.OpenshiftVersions.Allowlist)
	fmt.Println()

	// Step 4: Create policy engine
	fmt.Println("4. Creating policy engine...")
	engine := policy.NewEngine(registry)
	fmt.Println("   Policy engine ready")
	fmt.Println()

	// Step 5: Validate a cluster creation request
	fmt.Println("5. Validating cluster creation request...")
	request := &policy.CreateClusterRequest{
		Name:       "demo-cluster-01",
		Platform:   "aws",
		Version:    "4.20.3",
		Profile:    "aws-minimal-test",
		Region:     "us-east-1",
		BaseDomain: "labs.example.com",
		Owner:      "alice",
		Team:       "platform-team",
		CostCenter: "engineering",
		TTLHours:   24,
		ExtraTags: map[string]string{
			"Purpose":     "demonstration",
			"Environment": "test",
		},
	}

	result, err := engine.ValidateCreateRequest(request)
	if err != nil {
		log.Fatal("Validation engine error:", err)
	}

	if !result.Valid {
		fmt.Println("   ❌ Validation FAILED:")
		for _, validationErr := range result.Errors {
			fmt.Printf("      - %s: %s\n", validationErr.Field, validationErr.Message)
		}
		return
	}

	fmt.Println("   ✅ Validation PASSED")
	fmt.Printf("   Destroy at: %s\n", result.DestroyAt)
	fmt.Printf("   Merged tags (%d):\n", len(result.MergedTags))
	for k, v := range result.MergedTags {
		fmt.Printf("      %s: %s\n", k, v)
	}
	fmt.Println()

	// Step 6: Render install-config.yaml
	fmt.Println("6. Rendering install-config.yaml...")
	renderer := profile.NewRenderer(registry)

	pullSecret := `{"auths":{"cloud.openshift.com":{"auth":"base64encodedtoken"}}}`
	sshKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample alice@example.com"
	request.SSHPublicKey = &sshKey

	installConfig, err := renderer.RenderInstallConfig(request, pullSecret, result.MergedTags)
	if err != nil {
		log.Fatal("Failed to render install-config:", err)
	}

	fmt.Printf("   Generated install-config.yaml (%d bytes)\n", len(installConfig))
	fmt.Println("   Preview:")
	fmt.Println("   " + string(installConfig[:200]) + "...")
	fmt.Println()

	// Step 7: Test invalid request
	fmt.Println("7. Testing validation with invalid request...")
	invalidRequest := &policy.CreateClusterRequest{
		Name:       "INVALID-NAME", // Uppercase not allowed!
		Platform:   "aws",
		Version:    "4.19.0", // Not in allowlist!
		Profile:    "aws-minimal-test",
		Region:     "ap-south-1", // Not in allowlist!
		BaseDomain: "labs.example.com",
		Owner:      "bob",
		Team:       "test-team",
		CostCenter: "testing",
		TTLHours:   1000, // Exceeds max!
		ExtraTags: map[string]string{
			"ManagedBy": "hacker", // Reserved key!
		},
	}

	invalidResult, err := engine.ValidateCreateRequest(invalidRequest)
	if err != nil {
		log.Fatal("Validation engine error:", err)
	}

	if !invalidResult.Valid {
		fmt.Println("   ❌ Validation FAILED (as expected):")
		for _, validationErr := range invalidResult.Errors {
			fmt.Printf("      - %s: %s\n", validationErr.Field, validationErr.Message)
		}
	} else {
		fmt.Println("   ⚠️  Validation should have failed but didn't!")
	}
	fmt.Println()

	// Step 8: Get profile defaults
	fmt.Println("8. Getting profile defaults...")
	defaultVersion, _ := engine.GetDefaultVersion("aws-standard")
	defaultRegion, _ := engine.GetDefaultRegion("aws-standard")
	defaultTTL, _ := engine.GetDefaultTTL("aws-standard")

	fmt.Printf("   aws-standard defaults:\n")
	fmt.Printf("      Version: %s\n", defaultVersion)
	fmt.Printf("      Region: %s\n", defaultRegion)
	fmt.Printf("      TTL: %d hours\n", defaultTTL)
	fmt.Println()

	fmt.Println("=== Example Complete ===")
}
