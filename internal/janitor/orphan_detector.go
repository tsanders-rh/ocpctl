package janitor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// OrphanedResource represents an AWS resource without a matching cluster
type OrphanedResource struct {
	Type         string // "VPC", "LoadBalancer", "DNSRecord", "EC2Instance"
	ResourceID   string
	ResourceName string
	Region       string
	Tags         map[string]string
}

// DetectOrphanedResources finds AWS resources that don't match any cluster in the database
func (j *Janitor) detectOrphanedResources(ctx context.Context) error {
	// Get all cluster IDs and names from database
	clusters, err := j.store.Clusters.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}

	// Build lookup maps
	clustersByID := make(map[string]*types.Cluster)
	clustersByName := make(map[string]*types.Cluster)
	for _, cluster := range clusters {
		clustersByID[cluster.ID] = cluster
		clustersByName[cluster.Name] = cluster
	}

	// Initialize AWS SDK with default config
	// This will use AWS_REGION env var, EC2 metadata, or shared config file
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("Failed to load AWS config: %v (skipping orphan detection)", err)
		return nil // Don't fail janitor if AWS SDK can't be loaded
	}

	// If region is still empty, use a default
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
		log.Printf("No AWS region configured, defaulting to us-east-1")
	}

	log.Printf("Checking for orphaned resources in region: %s", cfg.Region)

	orphans := []OrphanedResource{}

	// Check VPCs
	vpcOrphans, err := j.detectOrphanedVPCs(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned VPCs: %v", err)
	} else {
		orphans = append(orphans, vpcOrphans...)
	}

	// Check Load Balancers
	lbOrphans, err := j.detectOrphanedLoadBalancers(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned load balancers: %v", err)
	} else {
		orphans = append(orphans, lbOrphans...)
	}

	// Check DNS Records
	dnsOrphans, err := j.detectOrphanedDNSRecords(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned DNS records: %v", err)
	} else {
		orphans = append(orphans, dnsOrphans...)
	}

	// Check EC2 Instances
	ec2Orphans, err := j.detectOrphanedEC2Instances(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned EC2 instances: %v", err)
	} else {
		orphans = append(orphans, ec2Orphans...)
	}

	// Check Hosted Zones
	hostedZoneOrphans, err := j.detectOrphanedHostedZones(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned hosted zones: %v", err)
	} else {
		orphans = append(orphans, hostedZoneOrphans...)
	}

	// Report findings
	if len(orphans) > 0 {
		log.Printf("WARNING: Found %d orphaned AWS resources:", len(orphans))
		for _, orphan := range orphans {
			log.Printf("  - %s: %s (%s) in %s", orphan.Type, orphan.ResourceName, orphan.ResourceID, orphan.Region)

			// Persist to database
			clusterName := extractClusterName(orphan.ResourceName)
			if orphan.Type == "DNSRecord" || orphan.Type == "HostedZone" {
				clusterName = extractClusterNameFromDNS(orphan.ResourceName)
			}

			dbOrphan := &types.OrphanedResource{
				ResourceType: types.OrphanedResourceType(orphan.Type),
				ResourceID:   orphan.ResourceID,
				ResourceName: orphan.ResourceName,
				Region:       orphan.Region,
				ClusterName:  clusterName,
				Tags:         types.OrphanedResourceTags(orphan.Tags),
			}

			if err := j.store.OrphanedResources.Upsert(ctx, dbOrphan); err != nil {
				log.Printf("  WARNING: Failed to persist orphaned resource to database: %v", err)
			}
		}
		log.Printf("These resources may incur costs and should be manually cleaned up.")
		log.Printf("Consider running AWS cleanup scripts or using openshift-install destroy with saved metadata.")
		log.Printf("View orphaned resources in the admin console: /admin/orphaned-resources")
	} else {
		log.Printf("No orphaned AWS resources detected")
	}

	return nil
}

// detectOrphanedVPCs finds VPCs tagged with cluster info but no matching cluster
func (j *Janitor) detectOrphanedVPCs(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	ec2Client := ec2.NewFromConfig(cfg)

	// List VPCs with cluster tags
	result, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag-key"),
				Values: []string{"Name"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, vpc := range result.Vpcs {
		vpcName := getTagValue(vpc.Tags, "Name")

		// Check if VPC name contains "-cluster-" pattern
		if !strings.Contains(vpcName, "-cluster-") {
			continue
		}

		// Extract cluster name (e.g., "d-cluster-lqrc7-vpc" -> "d-cluster")
		clusterName := extractClusterName(vpcName)
		if clusterName == "" {
			continue
		}

		// Check if cluster exists and is not destroyed
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "VPC",
				ResourceID:   aws.ToString(vpc.VpcId),
				ResourceName: vpcName,
				Region:       cfg.Region,
				Tags:         tagsToMap(vpc.Tags),
			})
		}
	}

	return orphans, nil
}

// detectOrphanedLoadBalancers finds load balancers with cluster names but no matching cluster
func (j *Janitor) detectOrphanedLoadBalancers(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	elbClient := elasticloadbalancingv2.NewFromConfig(cfg)

	result, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, lb := range result.LoadBalancers {
		lbName := aws.ToString(lb.LoadBalancerName)

		// Check if LB name contains cluster pattern
		if !strings.Contains(lbName, "-cluster-") {
			continue
		}

		// Extract cluster name
		clusterName := extractClusterName(lbName)
		if clusterName == "" {
			continue
		}

		// Check if cluster exists and is not destroyed
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "LoadBalancer",
				ResourceID:   aws.ToString(lb.LoadBalancerArn),
				ResourceName: lbName,
				Region:       cfg.Region,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedDNSRecords finds Route53 records for clusters that don't exist
func (j *Janitor) detectOrphanedDNSRecords(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	route53Client := route53.NewFromConfig(cfg)

	// Get hosted zones
	zonesResult, err := route53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}

	for _, zone := range zonesResult.HostedZones {
		// Get tags for this hosted zone to verify it was created by OpenShift/Kubernetes
		zoneID := aws.ToString(zone.Id)
		// Strip the /hostedzone/ prefix - the API wants just the bare zone ID
		zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")
		tagsResult, err := route53Client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
			ResourceType: route53types.TagResourceTypeHostedzone,
			ResourceId:   aws.String(zoneID),
		})
		if err != nil {
			log.Printf("Warning: failed to get tags for hosted zone %s: %v", aws.ToString(zone.Name), err)
			continue // Skip zones we can't get tags for
		}

		// Check if this zone was created by ocpctl specifically
		// Look for tags like "kubernetes.io/cluster/<infraID>: owned" AND "ClusterName" or "Profile"
		hasK8sTag := false
		hasOcpctlTag := false
		for _, tag := range tagsResult.ResourceTagSet.Tags {
			if strings.HasPrefix(aws.ToString(tag.Key), "kubernetes.io/cluster/") &&
				aws.ToString(tag.Value) == "owned" {
				hasK8sTag = true
			}
			// ocpctl adds ClusterName and Profile tags to all resources
			if aws.ToString(tag.Key) == "ClusterName" || aws.ToString(tag.Key) == "Profile" {
				hasOcpctlTag = true
			}
		}

		// Only check DNS records in zones created by ocpctl
		// This filters out zones created by openshift-install directly (not through ocpctl)
		if !hasK8sTag || !hasOcpctlTag {
			continue
		}

		// List record sets in this zone
		recordsResult, err := route53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId: zone.Id,
		})
		if err != nil {
			log.Printf("Error listing records for zone %s: %v", aws.ToString(zone.Name), err)
			continue
		}

		for _, record := range recordsResult.ResourceRecordSets {
			recordName := aws.ToString(record.Name)

			// Check for api.<cluster-name>.* pattern
			if !strings.Contains(recordName, "api.") || !strings.Contains(recordName, "-cluster.") {
				continue
			}

			// Extract cluster name from "api.d-cluster.mg.dog8code.com."
			clusterName := extractClusterNameFromDNS(recordName)
			if clusterName == "" {
				continue
			}

			// Check if cluster exists and is not destroyed
			cluster, exists := clustersByName[clusterName]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphans = append(orphans, OrphanedResource{
					Type:         "DNSRecord",
					ResourceID:   recordName,
					ResourceName: recordName,
					Region:       "global",
				})
			}
		}
	}

	return orphans, nil
}

// detectOrphanedEC2Instances finds EC2 instances with cluster tags but no matching cluster
func (j *Janitor) detectOrphanedEC2Instances(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	ec2Client := ec2.NewFromConfig(cfg)

	// List instances with kubernetes.io/cluster/* tags
	result, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag-key"),
				Values: []string{"kubernetes.io/cluster/*"},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending", "stopping", "stopped"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instanceName := getTagValue(instance.Tags, "Name")

			// Find cluster name from tags
			clusterName := ""
			for _, tag := range instance.Tags {
				if strings.HasPrefix(aws.ToString(tag.Key), "kubernetes.io/cluster/") {
					clusterName = strings.TrimPrefix(aws.ToString(tag.Key), "kubernetes.io/cluster/")
					break
				}
			}

			if clusterName == "" {
				continue
			}

			// Extract base cluster name (e.g., "d-cluster-lqrc7" -> "d-cluster")
			baseClusterName := extractClusterName(clusterName)
			if baseClusterName == "" {
				continue
			}

			// Check if cluster exists and is not destroyed
			cluster, exists := clustersByName[baseClusterName]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphans = append(orphans, OrphanedResource{
					Type:         "EC2Instance",
					ResourceID:   aws.ToString(instance.InstanceId),
					ResourceName: instanceName,
					Region:       cfg.Region,
					Tags:         tagsToMap(instance.Tags),
				})
			}
		}
	}

	return orphans, nil
}

// detectOrphanedHostedZones finds Route53 hosted zones for clusters that don't exist
func (j *Janitor) detectOrphanedHostedZones(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	route53Client := route53.NewFromConfig(cfg)

	// Get hosted zones
	zonesResult, err := route53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}

	for _, zone := range zonesResult.HostedZones {
		zoneName := aws.ToString(zone.Name)

		// Look for private hosted zones matching cluster pattern: <cluster-name>.domain.com
		// Skip public zones
		if !zone.Config.PrivateZone {
			continue
		}

		// Skip the base domain zones (we only want cluster-specific zones)
		// This is a simple heuristic: if the zone has exactly 3 parts (e.g., mg.dog8code.com), skip it
		// Cluster zones will have 4+ parts (e.g., sanders12.mg.dog8code.com)
		zoneParts := strings.Split(strings.TrimSuffix(zoneName, "."), ".")
		if len(zoneParts) < 4 {
			// This is likely a base domain zone, not a cluster zone
			continue
		}

		// Get tags for this hosted zone to verify it was created by OpenShift/Kubernetes
		zoneID := aws.ToString(zone.Id)
		// Strip the /hostedzone/ prefix - the API wants just the bare zone ID
		zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")
		tagsResult, err := route53Client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
			ResourceType: route53types.TagResourceTypeHostedzone,
			ResourceId:   aws.String(zoneID),
		})
		if err != nil {
			log.Printf("Warning: failed to get tags for hosted zone %s: %v", zoneName, err)
			continue // Skip zones we can't get tags for
		}

		// Check if this zone was created by ocpctl specifically
		// Look for tags like "kubernetes.io/cluster/<infraID>: owned" AND "ClusterName" or "Profile"
		hasK8sTag := false
		hasOcpctlTag := false
		for _, tag := range tagsResult.ResourceTagSet.Tags {
			if strings.HasPrefix(aws.ToString(tag.Key), "kubernetes.io/cluster/") &&
				aws.ToString(tag.Value) == "owned" {
				hasK8sTag = true
			}
			// ocpctl adds ClusterName and Profile tags to all resources
			if aws.ToString(tag.Key) == "ClusterName" || aws.ToString(tag.Key) == "Profile" {
				hasOcpctlTag = true
			}
		}

		// Only consider it an orphan if it has BOTH the Kubernetes tag AND ocpctl tags
		// This filters out zones created by openshift-install directly (not through ocpctl)
		if !hasK8sTag || !hasOcpctlTag {
			continue // Skip zones not created by ocpctl
		}

		// Extract cluster name from the first part of the zone name
		// e.g., sanders12.mg.dog8code.com -> sanders12
		//       d-cluster.mg.dog8code.com -> d-cluster
		clusterName := zoneParts[0]

		// Check if cluster exists and is not destroyed
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "HostedZone",
				ResourceID:   zoneID,
				ResourceName: zoneName,
				Region:       "global", // Route53 is global
			})
		}
	}

	return orphans, nil
}

// Helper functions

func getTagValue(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

func tagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string)
	for _, tag := range tags {
		m[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return m
}

// extractClusterName extracts the cluster name from a resource name
// Examples:
//   "d-cluster-lqrc7-vpc" -> "d-cluster"
//   "d-cluster-lqrc7-ext" -> "d-cluster"
//   "c-cluster-dhbrh-bootstrap" -> "c-cluster"
func extractClusterName(resourceName string) string {
	parts := strings.Split(resourceName, "-")
	if len(parts) < 3 {
		return ""
	}

	// Look for pattern: <name>-cluster-<random>-<suffix>
	for i := 0; i < len(parts)-2; i++ {
		if parts[i+1] == "cluster" {
			// Return "<name>-cluster"
			return strings.Join(parts[0:i+2], "-")
		}
	}

	return ""
}

// extractClusterNameFromDNS extracts cluster name from DNS record or hosted zone
// Examples:
//   "api.d-cluster.mg.dog8code.com." -> "d-cluster"
//   "d-cluster.mg.dog8code.com." -> "d-cluster"
func extractClusterNameFromDNS(dnsName string) string {
	// Remove trailing dot
	dnsName = strings.TrimSuffix(dnsName, ".")

	// Split by dots
	parts := strings.Split(dnsName, ".")
	if len(parts) < 2 {
		return ""
	}

	// For DNS records: api.<cluster-name>.domain.com
	if parts[0] == "api" && strings.Contains(parts[1], "-cluster") {
		return parts[1]
	}

	// For hosted zones: <cluster-name>.domain.com
	if strings.Contains(parts[0], "-cluster") {
		return parts[0]
	}

	return ""
}
