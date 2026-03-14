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
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: delete-iam-roles [--dry-run|--execute] <role-name> [<role-name>...]")
		fmt.Println("")
		fmt.Println("Delete IAM roles by name (handles instance profiles and policies)")
		fmt.Println("")
		fmt.Println("Options:")
		fmt.Println("  --dry-run   Show what would be deleted (safe, read-only)")
		fmt.Println("  --execute   Actually delete the IAM roles (requires confirmation)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  delete-iam-roles --dry-run sanders12-g58bb-master-role sanders12-g58bb-worker-role")
		fmt.Println("  delete-iam-roles --execute sanders12-g58bb-master-role sanders12-g58bb-worker-role")
		os.Exit(1)
	}

	mode := os.Args[1]
	if mode != "--dry-run" && mode != "--execute" {
		log.Fatalf("Invalid mode: %s. Use --dry-run or --execute", mode)
	}

	if len(os.Args) < 3 {
		log.Fatalf("No role names provided")
	}

	roleNames := os.Args[2:]

	ctx := context.Background()

	// Load AWS config with region defaulting to us-east-1
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	log.Printf("Using AWS region: %s", cfg.Region)
	log.Printf("Found %d roles to delete", len(roleNames))

	if mode == "--dry-run" {
		log.Printf("\n=== DRY RUN MODE ===")
		log.Printf("Roles to be deleted:")
		for _, roleName := range roleNames {
			log.Printf("  - %s", roleName)
		}
		log.Printf("\nNo changes will be made. Run with --execute to actually delete these roles.")
		return
	}

	// Execute mode - confirm before deleting
	log.Printf("\n=== EXECUTE MODE ===")
	log.Printf("WARNING: This will permanently delete %d IAM roles!", len(roleNames))
	log.Printf("Type 'DELETE' to confirm: ")

	var confirmation string
	fmt.Scanln(&confirmation)

	if confirmation != "DELETE" {
		log.Printf("Cancelled (you typed: %s)", confirmation)
		return
	}

	iamClient := iam.NewFromConfig(cfg)

	// Delete each role
	deleted := 0
	failed := 0

	for _, roleName := range roleNames {
		log.Printf("Deleting role: %s", roleName)

		// First, remove role from any instance profiles
		instanceProfilesResult, err := iamClient.ListInstanceProfilesForRole(ctx, &iam.ListInstanceProfilesForRoleInput{
			RoleName: aws.String(roleName),
		})
		if err != nil {
			// Check if role doesn't exist (already deleted)
			if strings.Contains(err.Error(), "NoSuchEntity") {
				log.Printf("  ℹ Role does not exist: %s", roleName)
				deleted++
				continue
			}
			log.Printf("  ✗ Failed to list instance profiles for %s: %v", roleName, err)
			failed++
			continue
		}

		for _, profile := range instanceProfilesResult.InstanceProfiles {
			profileName := aws.ToString(profile.InstanceProfileName)
			log.Printf("  - Removing role from instance profile: %s", profileName)

			_, err := iamClient.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
				InstanceProfileName: profile.InstanceProfileName,
				RoleName:            aws.String(roleName),
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
			RoleName: aws.String(roleName),
		})
		if err != nil {
			log.Printf("  ✗ Failed to list policies for %s: %v", roleName, err)
			failed++
			continue
		}

		for _, policy := range policiesResult.AttachedPolicies {
			_, err := iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  aws.String(roleName),
				PolicyArn: policy.PolicyArn,
			})
			if err != nil {
				log.Printf("  ✗ Failed to detach policy %s: %v", aws.ToString(policy.PolicyArn), err)
			} else {
				log.Printf("  ✓ Detached policy: %s", aws.ToString(policy.PolicyName))
			}
		}

		// Delete inline policies
		inlinePoliciesResult, err := iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
			RoleName: aws.String(roleName),
		})
		if err != nil {
			log.Printf("  ✗ Failed to list inline policies for %s: %v", roleName, err)
			failed++
			continue
		}

		for _, policyName := range inlinePoliciesResult.PolicyNames {
			_, err := iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
				RoleName:   aws.String(roleName),
				PolicyName: aws.String(policyName),
			})
			if err != nil {
				log.Printf("  ✗ Failed to delete inline policy %s: %v", policyName, err)
			} else {
				log.Printf("  ✓ Deleted inline policy: %s", policyName)
			}
		}

		// Delete the role
		_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
			RoleName: aws.String(roleName),
		})
		if err != nil {
			if strings.Contains(err.Error(), "NoSuchEntity") {
				log.Printf("  ℹ Role already deleted: %s", roleName)
				deleted++
			} else {
				log.Printf("  ✗ Failed to delete role %s: %v", roleName, err)
				failed++
				continue
			}
		} else {
			log.Printf("  ✓ Deleted: %s", roleName)
			deleted++
		}
	}

	log.Printf("\n=== Results ===")
	log.Printf("Successfully deleted: %d", deleted)
	log.Printf("Failed: %d", failed)
	log.Printf("Total: %d", len(roleNames))
}
