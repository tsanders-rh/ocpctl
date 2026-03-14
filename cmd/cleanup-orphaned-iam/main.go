package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: cleanup-orphaned-iam [--dry-run|--execute]")
		fmt.Println("")
		fmt.Println("This tool deletes orphaned IAM roles for clusters marked as DESTROYED in the database.")
		fmt.Println("")
		fmt.Println("Options:")
		fmt.Println("  --dry-run   Show what would be deleted (safe, read-only)")
		fmt.Println("  --execute   Actually delete the IAM roles (requires confirmation)")
		os.Exit(1)
	}

	mode := os.Args[1]
	if mode != "--dry-run" && mode != "--execute" {
		log.Fatalf("Invalid mode: %s. Use --dry-run or --execute", mode)
	}

	ctx := context.Background()

	// Connect to database
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/ocpctl?sslmode=disable"
	}

	st, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Get all DESTROYED clusters
	allClusters, err := st.Clusters.ListAll(ctx)
	if err != nil {
		log.Fatalf("Failed to list clusters: %v", err)
	}

	destroyedClusters := make(map[string]*types.Cluster)
	for _, cluster := range allClusters {
		if cluster.Status == types.ClusterStatusDestroyed {
			destroyedClusters[cluster.Name] = cluster
		}
	}

	log.Printf("Found %d DESTROYED clusters in database", len(destroyedClusters))

	// Get orphaned IAM roles from database
	activeStatus := types.OrphanedResourceStatusActive
	iamRoleType := types.OrphanedResourceTypeIAMRole
	orphanedResources, _, err := st.OrphanedResources.List(ctx, store.OrphanedResourceFilters{
		Status:       &activeStatus,
		ResourceType: &iamRoleType,
	})
	if err != nil {
		log.Fatalf("Failed to list orphaned resources: %v", err)
	}

	// Filter to only IAM roles that belong to DESTROYED clusters
	type roleToDelete struct {
		ResourceID   string
		ResourceName string
		ClusterName  string
	}
	rolesToDelete := []roleToDelete{}
	rolesByCluster := make(map[string][]string)

	for _, resource := range orphanedResources {
		clusterName := resource.ClusterName
		if clusterName == "" {
			// Extract from resource name if not set
			clusterName = extractClusterNameFromIAMRole(resource.ResourceName)
		}

		// Only delete if we have explicit DESTROYED status in DB
		if cluster, exists := destroyedClusters[clusterName]; exists {
			rolesToDelete = append(rolesToDelete, roleToDelete{
				ResourceID:   resource.ID,
				ResourceName: resource.ResourceName,
				ClusterName:  clusterName,
			})
			rolesByCluster[clusterName] = append(rolesByCluster[clusterName], resource.ResourceName)
			log.Printf("  ✓ %s (cluster: %s, destroyed: %s)", resource.ResourceName, clusterName, cluster.UpdatedAt.Format("2006-01-02"))
		}
	}

	if len(rolesToDelete) == 0 {
		log.Printf("No IAM roles to delete (no orphaned roles for DESTROYED clusters)")
		return
	}

	log.Printf("\n=== Summary ===")
	log.Printf("Total IAM roles to delete: %d", len(rolesToDelete))
	log.Printf("Affected DESTROYED clusters: %d", len(rolesByCluster))
	log.Printf("")

	for clusterName, roles := range rolesByCluster {
		log.Printf("  %s: %d roles", clusterName, len(roles))
	}

	if mode == "--dry-run" {
		log.Printf("\n=== DRY RUN MODE ===")
		log.Printf("No changes will be made. Run with --execute to actually delete these roles.")
		return
	}

	// Execute mode - confirm before deleting
	log.Printf("\n=== EXECUTE MODE ===")
	log.Printf("WARNING: This will permanently delete %d IAM roles!", len(rolesToDelete))
	log.Printf("Type 'DELETE' to confirm: ")

	var confirmation string
	fmt.Scanln(&confirmation)

	if confirmation != "DELETE" {
		log.Printf("Cancelled (you typed: %s)", confirmation)
		return
	}

	// Load AWS config with region defaulting to us-east-1
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	log.Printf("Using AWS region: %s", cfg.Region)

	iamClient := iam.NewFromConfig(cfg)

	// Delete each role
	deleted := 0
	failed := 0

	for _, role := range rolesToDelete {
		log.Printf("Deleting role: %s", role.ResourceName)

		// First, remove role from any instance profiles
		instanceProfilesResult, err := iamClient.ListInstanceProfilesForRole(ctx, &iam.ListInstanceProfilesForRoleInput{
			RoleName: aws.String(role.ResourceName),
		})
		if err != nil {
			// Check if role doesn't exist (already deleted)
			if strings.Contains(err.Error(), "NoSuchEntity") {
				log.Printf("  ℹ Role already deleted: %s", role.ResourceName)
				// Mark as resolved in database since it's already gone
				if err := st.OrphanedResources.MarkResolved(ctx, role.ResourceID, "cleanup-script", fmt.Sprintf("IAM role already deleted for cluster: %s", role.ClusterName)); err != nil {
					log.Printf("  Warning: Failed to mark as resolved in database: %v", err)
				}
				deleted++
				continue
			}
			log.Printf("  ✗ Failed to list instance profiles for %s: %v", role.ResourceName, err)
			failed++
			continue
		}

		for _, profile := range instanceProfilesResult.InstanceProfiles {
			profileName := aws.ToString(profile.InstanceProfileName)
			log.Printf("  - Removing role from instance profile: %s", profileName)

			_, err := iamClient.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
				InstanceProfileName: profile.InstanceProfileName,
				RoleName:            aws.String(role.ResourceName),
			})
			if err != nil {
				log.Printf("  ✗ Failed to remove role from instance profile %s: %v", profileName, err)
			} else {
				log.Printf("  ✓ Removed from instance profile: %s", profileName)

				// Try to delete the instance profile if it's now empty
				_, err = iamClient.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
					InstanceProfileName: profile.InstanceProfileName,
				})
				if err != nil {
					log.Printf("  - Instance profile %s not deleted (may still be in use): %v", profileName, err)
				} else {
					log.Printf("  ✓ Deleted instance profile: %s", profileName)
				}
			}
		}

		// Detach all managed policies
		policiesResult, err := iamClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
			RoleName: aws.String(role.ResourceName),
		})
		if err != nil {
			log.Printf("  ✗ Failed to list policies for %s: %v", role.ResourceName, err)
			failed++
			continue
		}

		for _, policy := range policiesResult.AttachedPolicies {
			_, err := iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  aws.String(role.ResourceName),
				PolicyArn: policy.PolicyArn,
			})
			if err != nil {
				log.Printf("  ✗ Failed to detach policy %s: %v", aws.ToString(policy.PolicyArn), err)
			}
		}

		// Delete inline policies
		inlinePoliciesResult, err := iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
			RoleName: aws.String(role.ResourceName),
		})
		if err != nil {
			log.Printf("  ✗ Failed to list inline policies for %s: %v", role.ResourceName, err)
			failed++
			continue
		}

		for _, policyName := range inlinePoliciesResult.PolicyNames {
			_, err := iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
				RoleName:   aws.String(role.ResourceName),
				PolicyName: aws.String(policyName),
			})
			if err != nil {
				log.Printf("  ✗ Failed to delete inline policy %s: %v", policyName, err)
			}
		}

		// Delete the role
		_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(role.ResourceName),
		})
		if err != nil {
			if strings.Contains(err.Error(), "NoSuchEntity") {
				log.Printf("  ℹ Role already deleted: %s", role.ResourceName)
				deleted++
			} else {
				log.Printf("  ✗ Failed to delete role %s: %v", role.ResourceName, err)
				failed++
				continue
			}
		} else {
			log.Printf("  ✓ Deleted: %s", role.ResourceName)
			deleted++
		}

		// Mark as resolved in orphaned_resources table
		if err := st.OrphanedResources.MarkResolved(ctx, role.ResourceID, "cleanup-script", fmt.Sprintf("Deleted IAM role for DESTROYED cluster: %s", role.ClusterName)); err != nil {
			log.Printf("  Warning: Failed to mark as resolved in database: %v", err)
		}
	}

	log.Printf("\n=== Results ===")
	log.Printf("Successfully deleted: %d", deleted)
	log.Printf("Failed: %d", failed)
	log.Printf("Total: %d", len(rolesToDelete))
}

func extractClusterNameFromIAMRole(roleName string) string {
	var prefix string
	if strings.Contains(roleName, "-openshift-") {
		parts := strings.SplitN(roleName, "-openshift-", 2)
		if len(parts) == 2 {
			prefix = parts[0]
		}
	} else if strings.HasSuffix(roleName, "-master-role") {
		prefix = strings.TrimSuffix(roleName, "-master-role")
	} else if strings.HasSuffix(roleName, "-worker-role") {
		prefix = strings.TrimSuffix(roleName, "-worker-role")
	}

	if prefix == "" {
		return ""
	}

	parts := strings.Split(prefix, "-")
	if len(parts) < 2 {
		return ""
	}

	lastSegment := parts[len(parts)-1]
	if len(lastSegment) == 5 {
		return strings.Join(parts[0:len(parts)-1], "-")
	}

	return ""
}
