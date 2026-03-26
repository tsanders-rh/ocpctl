package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func main() {
	var (
		clusterID   string
		clusterName string
		dryRun      bool
		region      string
	)

	flag.StringVar(&clusterID, "id", "", "Cluster ID to tag resources for")
	flag.StringVar(&clusterName, "name", "", "Cluster name to tag resources for")
	flag.BoolVar(&dryRun, "dry-run", false, "Dry run mode (don't actually tag)")
	flag.StringVar(&region, "region", "", "AWS region (auto-detected if not specified)")
	flag.Parse()

	if clusterID == "" && clusterName == "" {
		log.Fatal("Must specify either -id or -name")
	}

	ctx := context.Background()

	// Get environment for configuration validation
	environment := os.Getenv("ENVIRONMENT")

	// Connect to database
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		if environment == "production" {
			log.Fatalf("CRITICAL: DATABASE_URL must be set in production environment")
		}
		// Development fallback with warning
		log.Println("WARNING: DATABASE_URL not set, using localhost (development only)")
		dbURL = "postgres://localhost:5432/ocpctl?sslmode=disable"
	}

	// Validate SSL mode in production
	if environment == "production" {
		if strings.Contains(dbURL, "sslmode=disable") || !strings.Contains(dbURL, "sslmode=") {
			log.Fatalf("CRITICAL: Database connections must use SSL in production (sslmode=require or sslmode=verify-full)")
		}
	}

	st, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Get cluster
	var cluster *types.Cluster
	if clusterID != "" {
		cluster, err = st.Clusters.GetByID(ctx, clusterID)
		if err != nil {
			log.Fatalf("Failed to get cluster: %v", err)
		}
	} else {
		// Get by name - need to list all and filter
		clusters, err := st.Clusters.ListAll(ctx)
		if err != nil {
			log.Fatalf("Failed to list clusters: %v", err)
		}
		for _, c := range clusters {
			if c.Name == clusterName {
				cluster = c
				break
			}
		}
		if cluster == nil {
			log.Fatalf("Cluster not found: %s", clusterName)
		}
	}

	log.Printf("Tagging resources for cluster: %s (ID: %s)", cluster.Name, cluster.ID)

	// Use cluster region if not specified
	if region == "" {
		region = cluster.Region
		if region == "" {
			region = "us-east-1"
			log.Printf("No region specified, using default: %s", region)
		} else {
			log.Printf("Using cluster region: %s", region)
		}
	}

	// Discover infraID from AWS resources
	log.Printf("Discovering infraID from AWS resources...")
	infraID, err := discoverInfraID(ctx, region, cluster.Name)
	if err != nil {
		log.Fatalf("Failed to discover infraID: %v", err)
	}
	log.Printf("Found infraID: %s", infraID)

	// Build metadata for tagging
	metadata := installer.ClusterMetadata{
		ClusterName: cluster.Name,
		ProfileName: cluster.Profile,
		InfraID:     infraID,
		CreatedAt:   cluster.CreatedAt,
		Region:      region,
	}

	if dryRun {
		log.Printf("\n[DRY RUN] Would tag resources with:")
		log.Printf("  ManagedBy: ocpctl")
		log.Printf("  ClusterName: %s", cluster.Name)
		log.Printf("  Profile: %s", cluster.Profile)
		log.Printf("  InfraID: %s", infraID)
		log.Printf("  CreatedAt: %s", cluster.CreatedAt.Format(time.RFC3339))
		log.Printf("  Region: %s", region)
		log.Printf("  kubernetes.io/cluster/%s: owned", infraID)
		return
	}

	// Create installer for the cluster's version
	log.Printf("Creating installer for OpenShift version: %s", cluster.Version)
	inst, err := installer.NewInstallerForVersion(cluster.Version)
	if err != nil {
		log.Fatalf("Failed to create installer for version %s: %v", cluster.Version, err)
	}

	// Tag resources
	log.Printf("Starting resource tagging...")
	if err := inst.TagAWSResources(ctx, "", metadata); err != nil {
		log.Fatalf("Failed to tag resources: %v", err)
	}

	log.Printf("\n✓ Successfully tagged all AWS resources for cluster %s", cluster.Name)
	log.Printf("\nTagged resources:")
	log.Printf("  - EC2 resources (VPCs, subnets, instances, volumes, security groups, elastic IPs)")
	log.Printf("  - Load balancers (NLB, ALB)")
	log.Printf("  - Route53 hosted zones")
	log.Printf("  - S3 buckets (bootstrap, OIDC)")
	log.Printf("  - IAM roles and OIDC provider")
}

// discoverInfraID discovers the cluster's infrastructure ID by querying AWS resources
func discoverInfraID(ctx context.Context, region, clusterName string) (string, error) {
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", err
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// List all VPCs and look for ones with kubernetes.io/cluster/<infraID> tag
	vpcsResp, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return "", err
	}

	// Try to find VPC by Name tag containing cluster name
	for _, vpc := range vpcsResp.Vpcs {
		vpcName := getTagValue(vpc.Tags, "Name")

		// Check if VPC name matches cluster (e.g., sanders12-9hfvt-vpc)
		if !strings.Contains(vpcName, clusterName) {
			continue
		}

		// Extract infraID from kubernetes.io/cluster/<infraID> tag
		for _, tag := range vpc.Tags {
			if aws.ToString(tag.Key) == "kubernetes.io/cluster" {
				continue
			}

			// Check if tag key starts with "kubernetes.io/cluster/"
			tagKey := aws.ToString(tag.Key)
			if strings.HasPrefix(tagKey, "kubernetes.io/cluster/") {
				infraID := strings.TrimPrefix(tagKey, "kubernetes.io/cluster/")
				log.Printf("Discovered infraID from VPC %s: %s", vpcName, infraID)
				return infraID, nil
			}
		}
	}

	return "", fmt.Errorf("no VPC found with kubernetes.io/cluster/<infraID> tag for cluster %s", clusterName)
}

// getTagValue returns the value of a tag by key
func getTagValue(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}
