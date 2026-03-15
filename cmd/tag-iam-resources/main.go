package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: tag-iam-resources [--dry-run|--execute]")
		fmt.Println("")
		fmt.Println("Tag orphaned IAM/OIDC resources with cluster metadata")
		fmt.Println("")
		fmt.Println("Options:")
		fmt.Println("  --dry-run   Show what would be tagged (safe, read-only)")
		fmt.Println("  --execute   Actually tag the resources (requires confirmation)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  tag-iam-resources --dry-run")
		fmt.Println("  tag-iam-resources --execute")
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

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Query orphaned IAM/OIDC resources
	// Get IAM roles
	activeStatus := types.OrphanedResourceStatusActive
	iamRoleType := types.OrphanedResourceTypeIAMRole
	iamRoles, _, err := st.OrphanedResources.List(ctx, store.OrphanedResourceFilters{
		Status:       &activeStatus,
		ResourceType: &iamRoleType,
	})
	if err != nil {
		log.Fatalf("Failed to query IAM roles: %v", err)
	}

	// Get OIDC providers
	oidcType := types.OrphanedResourceTypeOIDCProvider
	oidcProviders, _, err := st.OrphanedResources.List(ctx, store.OrphanedResourceFilters{
		Status:       &activeStatus,
		ResourceType: &oidcType,
	})
	if err != nil {
		log.Fatalf("Failed to query OIDC providers: %v", err)
	}

	// Combine all resources
	resources := append(iamRoles, oidcProviders...)

	log.Printf("Found %d orphaned IAM/OIDC resources in database", len(resources))

	if len(resources) == 0 {
		log.Printf("No orphaned resources to tag")
		return
	}

	if mode == "--dry-run" {
		log.Printf("\n=== DRY RUN MODE ===")
		runDryRun(ctx, st, resources)
		log.Printf("\nNo changes will be made. Run with --execute to actually tag these resources.")
		return
	}

	// Execute mode - confirm before tagging
	log.Printf("\n=== EXECUTE MODE ===")
	log.Printf("WARNING: This will tag %d AWS resources!", len(resources))
	log.Printf("Type 'TAG' to confirm: ")

	var confirmation string
	fmt.Scanln(&confirmation)

	if confirmation != "TAG" {
		log.Printf("Cancelled (you typed: %s)", confirmation)
		return
	}

	runExecute(ctx, st, iamClient, resources)
}

func runDryRun(ctx context.Context, st *store.Store, resources []*types.OrphanedResource) {
	log.Printf("\nResources to be tagged:")

	// Load all clusters into a map for quick lookup
	allClusters, err := st.Clusters.ListAll(ctx)
	if err != nil {
		log.Fatalf("Failed to load clusters: %v", err)
	}
	clustersByName := make(map[string]*types.Cluster)
	for _, c := range allClusters {
		clustersByName[c.Name] = c
	}

	byCluster := groupByCluster(resources)

	for clusterName, clusterResources := range byCluster {
		cluster, exists := clustersByName[clusterName]
		if !exists {
			log.Printf("\n⚠ Cluster %s: Not found in database", clusterName)
			log.Printf("  Resources will be skipped:")
			for _, r := range clusterResources {
				log.Printf("    - %s: %s", r.ResourceType, r.ResourceName)
			}
			continue
		}

		tags := buildTagsFromCluster(cluster)
		log.Printf("\nCluster %s (infraID: %s)", cluster.Name, extractInfraID(clusterResources))
		log.Printf("  Tags to apply: %d", len(tags))
		for k, v := range tags {
			log.Printf("    %s = %s", k, v)
		}
		log.Printf("  Resources:")
		for _, r := range clusterResources {
			log.Printf("    - %s: %s", r.ResourceType, r.ResourceName)
		}
	}
}

func runExecute(ctx context.Context, st *store.Store, iamClient *iam.Client, resources []*types.OrphanedResource) {
	// Load all clusters into a map for quick lookup
	allClusters, err := st.Clusters.ListAll(ctx)
	if err != nil {
		log.Fatalf("Failed to load clusters: %v", err)
	}
	clustersByName := make(map[string]*types.Cluster)
	for _, c := range allClusters {
		clustersByName[c.Name] = c
	}

	byCluster := groupByCluster(resources)

	tagged := 0
	failed := 0
	skipped := 0

	for clusterName, clusterResources := range byCluster {
		cluster, exists := clustersByName[clusterName]
		if !exists {
			log.Printf("\n⚠ Cluster %s: Not found in database", clusterName)
			log.Printf("  Skipping %d resources", len(clusterResources))
			skipped += len(clusterResources)
			continue
		}

		tags := buildTagsFromCluster(cluster)
		infraID := extractInfraID(clusterResources)

		log.Printf("\nTagging resources for cluster %s (infraID: %s)", cluster.Name, infraID)

		for _, resource := range clusterResources {
			log.Printf("  Tagging %s: %s", resource.ResourceType, resource.ResourceName)

			var tagErr error
			switch resource.ResourceType {
			case types.OrphanedResourceTypeIAMRole:
				tagErr = tagIAMRole(ctx, iamClient, resource.ResourceName, tags)
			case types.OrphanedResourceTypeOIDCProvider:
				// OIDC provider needs ARN, construct it from resource ID
				arn := constructOIDCProviderARN(resource.ResourceID, cluster.Region)
				tagErr = tagOIDCProvider(ctx, iamClient, arn, tags)
			default:
				log.Printf("    ⚠ Unknown resource type: %s", resource.ResourceType)
				skipped++
				continue
			}

			if tagErr != nil {
				log.Printf("    ✗ Failed to tag: %v", tagErr)
				failed++
			} else {
				log.Printf("    ✓ Tagged successfully")

				// Mark as resolved in database
				if err := st.OrphanedResources.MarkResolved(ctx, resource.ID, "tag-iam-resources", "Tagged with cluster metadata"); err != nil {
					log.Printf("    ⚠ Failed to mark as resolved in DB: %v", err)
				} else {
					log.Printf("    ✓ Marked as resolved in database")
					tagged++
				}
			}
		}
	}

	log.Printf("\n=== Results ===")
	log.Printf("Successfully tagged: %d", tagged)
	log.Printf("Failed: %d", failed)
	log.Printf("Skipped: %d", skipped)
	log.Printf("Total: %d", len(resources))
}

func groupByCluster(resources []*types.OrphanedResource) map[string][]*types.OrphanedResource {
	byCluster := make(map[string][]*types.OrphanedResource)
	for _, r := range resources {
		if r.ClusterName != "" {
			byCluster[r.ClusterName] = append(byCluster[r.ClusterName], r)
		}
	}
	return byCluster
}

func buildTagsFromCluster(cluster *types.Cluster) map[string]string {
	// Extract infraID from cluster metadata or resource names
	// For now, we'll need to determine infraID from the cluster
	// This is a simplified version - in reality we'd need to parse metadata.json
	// or extract from resource names

	// Note: We can't determine the exact infraID from the cluster record alone
	// since it's generated during installation. However, we can use the cluster
	// name as a fallback for the ClusterName tag.

	tags := map[string]string{
		"ManagedBy":   "ocpctl",
		"ClusterName": cluster.Name,
		"Profile":     cluster.Profile,
		"CreatedAt":   cluster.CreatedAt.Format(time.RFC3339),
	}

	// Add optional tags if available
	if cluster.Owner != "" {
		tags["Owner"] = cluster.Owner
	}
	if cluster.Team != "" {
		tags["Team"] = cluster.Team
	}
	if cluster.CostCenter != "" {
		tags["CostCenter"] = cluster.CostCenter
	}

	return tags
}

func extractInfraID(resources []*types.OrphanedResource) string {
	// Try to extract infraID from resource names
	// IAM roles have pattern: <cluster>-<infraID>-openshift-*
	// OIDC provider has: <infraID>-oidc.s3.<region>.amazonaws.com

	for _, r := range resources {
		if r.ResourceType == types.OrphanedResourceTypeOIDCProvider {
			// OIDC provider URL is <infraID>-oidc.s3.<region>.amazonaws.com
			// Extract from resource ID
			parts := strings.Split(r.ResourceID, "-oidc.")
			if len(parts) > 0 {
				return parts[0]
			}
		}
		if r.ResourceType == types.OrphanedResourceTypeIAMRole {
			// Try to extract from role name pattern
			// Pattern: <cluster>-<infraID>-openshift-*
			if strings.Contains(r.ResourceName, "-openshift-") {
				parts := strings.Split(r.ResourceName, "-openshift-")
				if len(parts) > 0 {
					nameParts := strings.Split(parts[0], "-")
					if len(nameParts) >= 2 {
						// infraID is the last part before "-openshift-"
						infraID := nameParts[len(nameParts)-1]
						if len(infraID) == 5 {
							return infraID
						}
					}
				}
			}
			// Pattern: <cluster>-<infraID>-master-role or <cluster>-<infraID>-worker-role
			if strings.HasSuffix(r.ResourceName, "-master-role") || strings.HasSuffix(r.ResourceName, "-worker-role") {
				parts := strings.Split(r.ResourceName, "-")
				if len(parts) >= 3 {
					infraID := parts[len(parts)-3]
					if len(infraID) == 5 {
						return infraID
					}
				}
			}
		}
	}
	return "unknown"
}

// constructOIDCProviderARN builds the ARN for an OIDC provider from its resource ID
func constructOIDCProviderARN(resourceID, region string) string {
	// OIDC provider resource ID is the host part: <infraID>-oidc.s3.<region>.amazonaws.com
	// We need to get the account ID using AWS CLI
	cmd := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("arn:aws:iam::UNKNOWN:oidc-provider/%s", resourceID)
	}

	accountID := strings.TrimSpace(string(output))
	return fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", accountID, resourceID)
}

func tagIAMRole(ctx context.Context, iamClient *iam.Client, roleName string, tags map[string]string) error {
	// Convert tags to IAM SDK format
	iamTags := []iamtypes.Tag{}
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := iamClient.TagRole(ctx, &iam.TagRoleInput{
		RoleName: aws.String(roleName),
		Tags:     iamTags,
	})
	if err != nil {
		return fmt.Errorf("tag IAM role: %w", err)
	}

	return nil
}

func tagOIDCProvider(ctx context.Context, iamClient *iam.Client, providerARN string, tags map[string]string) error {
	// Convert tags to IAM SDK format
	iamTags := []iamtypes.Tag{}
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	_, err := iamClient.TagOpenIDConnectProvider(ctx, &iam.TagOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
		Tags:                     iamTags,
	})
	if err != nil {
		return fmt.Errorf("tag OIDC provider: %w", err)
	}

	return nil
}
