package janitor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/tsanders-rh/ocpctl/internal/metrics"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// OrphanedResource represents an AWS resource without a matching cluster
type OrphanedResource struct {
	Type         string // "VPC", "LoadBalancer", "DNSRecord", "EC2Instance", "HostedZone", "IAMRole", "OIDCProvider", "EBSVolume", "ElasticIP", "CloudWatchLogGroup"
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

	// Check IAM Roles
	iamRoleOrphans, err := j.detectOrphanedIAMRoles(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned IAM roles: %v", err)
	} else {
		orphans = append(orphans, iamRoleOrphans...)
	}

	// Check OIDC Providers
	oidcOrphans, err := j.detectOrphanedOIDCProviders(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned OIDC providers: %v", err)
	} else {
		orphans = append(orphans, oidcOrphans...)
	}

	// Check EBS Volumes
	ebsOrphans, err := j.detectOrphanedEBSVolumes(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned EBS volumes: %v", err)
	} else {
		orphans = append(orphans, ebsOrphans...)
	}

	// Check Elastic IPs
	eipOrphans, err := j.detectOrphanedElasticIPs(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned Elastic IPs: %v", err)
	} else {
		orphans = append(orphans, eipOrphans...)
	}

	// Check CloudWatch Log Groups
	cwlOrphans, err := j.detectOrphanedCloudWatchLogGroups(ctx, cfg, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned CloudWatch log groups: %v", err)
	} else {
		orphans = append(orphans, cwlOrphans...)
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
			} else if orphan.Type == "IAMRole" {
				clusterName = extractClusterNameFromIAMRole(orphan.ResourceName)
			} else if orphan.Type == "OIDCProvider" {
				// OIDC providers should have ClusterName in tags
				if cn, ok := orphan.Tags["ClusterName"]; ok {
					clusterName = cn
				}
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

		// Publish CloudWatch metrics for total orphaned resources
		if j.metricsPublisher != nil {
			if err := j.metricsPublisher.PublishGauge(ctx, metrics.MetricOrphanedResources, float64(len(orphans)), map[string]string{
				"Region": cfg.Region,
			}); err != nil {
				log.Printf("Warning: failed to publish orphaned resources metric: %v", err)
			}

			// Publish metrics by resource type
			orphansByType := make(map[string]int)
			for _, orphan := range orphans {
				orphansByType[orphan.Type]++
			}

			for resourceType, count := range orphansByType {
				if err := j.metricsPublisher.PublishCount(ctx, metrics.MetricOrphanedResourceDetected, float64(count), map[string]string{
					"ResourceType": resourceType,
					"Region":       cfg.Region,
				}); err != nil {
					log.Printf("Warning: failed to publish orphaned resource metric for type %s: %v", resourceType, err)
				}
			}
		}
	} else {
		log.Printf("No orphaned AWS resources detected")

		// Publish zero metric when no orphans found
		if j.metricsPublisher != nil {
			if err := j.metricsPublisher.PublishGauge(ctx, metrics.MetricOrphanedResources, 0, map[string]string{
				"Region": cfg.Region,
			}); err != nil {
				log.Printf("Warning: failed to publish orphaned resources metric: %v", err)
			}
		}
	}

	return nil
}

// detectOrphanedVPCs finds VPCs tagged with cluster info but no matching cluster
func (j *Janitor) detectOrphanedVPCs(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	ec2Client := ec2.NewFromConfig(cfg)

	// List all VPCs (we'll filter by tags)
	result, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, vpc := range result.Vpcs {
		// Step 1: Check for ManagedBy=ocpctl tag (preferred method)
		managedByOcpctl := getTagValue(vpc.Tags, "ManagedBy") == "ocpctl"
		clusterNameFromTag := getTagValue(vpc.Tags, "ClusterName")

		// If ManagedBy tag is present, use it
		if managedByOcpctl {
			if clusterNameFromTag == "" {
				log.Printf("[detectOrphanedVPCs] VPC %s has ManagedBy=ocpctl but no ClusterName tag", aws.ToString(vpc.VpcId))
				continue
			}

			// Check if cluster exists in database
			cluster, exists := clustersByName[clusterNameFromTag]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphans = append(orphans, OrphanedResource{
					Type:         "VPC",
					ResourceID:   aws.ToString(vpc.VpcId),
					ResourceName: getTagValue(vpc.Tags, "Name"),
					Region:       cfg.Region,
					Tags:         tagsToMap(vpc.Tags),
				})
			}
			// Only rely on ManagedBy=ocpctl tag - no pattern matching fallback to avoid false positives
		}

	}

	return orphans, nil
}

// detectOrphanedLoadBalancers finds load balancers with cluster names but no matching cluster
// detectOrphanedLoadBalancers finds load balancers with cluster tags but no matching cluster
func (j *Janitor) detectOrphanedLoadBalancers(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	elbClient := elasticloadbalancingv2.NewFromConfig(cfg)

	result, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, lb := range result.LoadBalancers {
		lbArn := aws.ToString(lb.LoadBalancerArn)

		// Get tags for this load balancer
		tagsResult, err := elbClient.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{
			ResourceArns: []string{lbArn},
		})
		if err != nil {
			log.Printf("Warning: failed to get tags for load balancer %s: %v", lbArn, err)
			continue
		}

		if len(tagsResult.TagDescriptions) == 0 {
			continue
		}

		tags := make(map[string]string)
		for _, tag := range tagsResult.TagDescriptions[0].Tags {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}

		// Check for ManagedBy=ocpctl tag
		if tags["ManagedBy"] == "ocpctl" {
			clusterName := tags["ClusterName"]
			if clusterName == "" {
				log.Printf("[detectOrphanedLoadBalancers] LB %s has ManagedBy=ocpctl but no ClusterName tag", lbArn)
				continue
			}

			cluster, exists := clustersByName[clusterName]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphans = append(orphans, OrphanedResource{
					Type:         "LoadBalancer",
					ResourceID:   lbArn,
					ResourceName: aws.ToString(lb.LoadBalancerName),
					Region:       cfg.Region,
					Tags:         tags,
				})
			}
			continue
		}

		// Check for kubernetes.io/cluster/{infraID} tag (created by Kubernetes services)
		for key, value := range tags {
			if strings.HasPrefix(key, "kubernetes.io/cluster/") && (value == "owned" || value == "shared") {
				infraID := strings.TrimPrefix(key, "kubernetes.io/cluster/")
				// Extract cluster name from infraID pattern: {clustername}-{5chars}
				parts := strings.Split(infraID, "-")
				if len(parts) < 2 {
					continue
				}
				// Remove the 5-char suffix to get cluster name
				clusterName := strings.Join(parts[0:len(parts)-1], "-")

				cluster, exists := clustersByName[clusterName]
				if !exists || cluster.Status == types.ClusterStatusDestroyed {
					orphans = append(orphans, OrphanedResource{
						Type:         "LoadBalancer",
						ResourceID:   lbArn,
						ResourceName: aws.ToString(lb.LoadBalancerName),
						Region:       cfg.Region,
						Tags:         tags,
					})
				}
				break
			}
		}
		// Only rely on tags - no pattern matching fallback to avoid false positives
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

// detectOrphanedIAMRoles finds IAM roles created by ccoctl for clusters that don't exist
func (j *Janitor) detectOrphanedIAMRoles(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	iamClient := iam.NewFromConfig(cfg)

	orphans := []OrphanedResource{}

	log.Printf("IAM Detection: Starting scan (clusters in DB: %d)", len(clustersByName))

	// Use paginator to iterate through all IAM roles (account may have 1000+ roles)
	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})

	totalScanned := 0
	openshiftRoles := 0

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Printf("IAM Detection: Error paginating roles: %v", err)
			return nil, err
		}

		for _, role := range page.Roles {
			totalScanned++
			roleName := aws.ToString(role.RoleName)

			// Get role tags to check for ManagedBy tag
			tagsResult, err := iamClient.ListRoleTags(ctx, &iam.ListRoleTagsInput{
				RoleName: role.RoleName,
			})

			tags := make(map[string]string)
			if err == nil {
				for _, tag := range tagsResult.Tags {
					tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			// Step 1: Check for ManagedBy=ocpctl tag (preferred method)
			managedByOcpctl := tags["ManagedBy"] == "ocpctl"
			clusterNameFromTag := tags["ClusterName"]

			if managedByOcpctl {
				openshiftRoles++
				if clusterNameFromTag == "" {
					log.Printf("[detectOrphanedIAMRoles] Role %s has ManagedBy=ocpctl but no ClusterName tag", roleName)
					continue
				}

				// Check if cluster exists in database
				cluster, exists := clustersByName[clusterNameFromTag]
				if !exists || cluster.Status == types.ClusterStatusDestroyed {
					orphans = append(orphans, OrphanedResource{
						Type:         "IAMRole",
						ResourceID:   aws.ToString(role.Arn),
						ResourceName: roleName,
						Region:       "global", // IAM is global
						Tags:         tags,
					})
				}
				// Only rely on ManagedBy=ocpctl tag - no pattern matching fallback to avoid false positives
			}

		}
	}

	log.Printf("IAM Detection: Scanned %d total roles, %d OpenShift-related, %d orphaned", totalScanned, openshiftRoles, len(orphans))

	return orphans, nil
}

// detectOrphanedOIDCProviders finds OIDC providers created for clusters that don't exist
func (j *Janitor) detectOrphanedOIDCProviders(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	iamClient := iam.NewFromConfig(cfg)

	// List all OIDC providers
	result, err := iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}

	for _, provider := range result.OpenIDConnectProviderList {
		providerArn := aws.ToString(provider.Arn)

		// Get provider details including tags
		detailsResult, err := iamClient.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: provider.Arn,
		})
		if err != nil {
			log.Printf("Warning: failed to get OIDC provider details for %s: %v", providerArn, err)
			continue
		}

		providerURL := aws.ToString(detailsResult.Url)

		// OIDC providers have URLs like:
		// - rh-oidc.s3.us-east-1.amazonaws.com/29avu8o05l9g7lq97vbcsgqfgmklqqh3 (cluster infra ID)
		// - oidc.s3.us-east-1.amazonaws.com/29avu8o05l9g7lq97vbcsgqfgmklqqh3

		// Check if this is an OpenShift OIDC provider
		if !strings.Contains(providerURL, "rh-oidc.s3.") && !strings.Contains(providerURL, "oidc.s3.") {
			continue
		}

		// Look for ClusterName in tags
		clusterName := ""
		tags := make(map[string]string)
		for _, tag := range detailsResult.Tags {
			tagKey := aws.ToString(tag.Key)
			tagValue := aws.ToString(tag.Value)
			tags[tagKey] = tagValue

			if tagKey == "ClusterName" {
				clusterName = tagValue
			}
		}

		// If we found a ClusterName tag, check if cluster exists
		if clusterName != "" {
			cluster, exists := clustersByName[clusterName]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphans = append(orphans, OrphanedResource{
					Type:         "OIDCProvider",
					ResourceID:   providerArn,
					ResourceName: providerURL,
					Region:       "global", // IAM is global
					Tags:         tags,
				})
			}
		} else {
			// No ClusterName tag - this provider might be orphaned but we can't be sure
			// Extract the infra ID from the URL and log a warning
			log.Printf("Warning: OIDC provider %s has no ClusterName tag, skipping", providerURL)
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

// extractClusterNameFromIAMRole extracts cluster name from IAM role name
// Examples:
//   "sanders12-9hfvt-openshift-cloud-credential-operator-cloud-creden" -> "sanders12"
//   "sanders12-9hfvt-master-role" -> "sanders12"
//   "d-cluster-lqrc7-openshift-ingress-operator-cloud-credentials" -> "d-cluster"
func extractClusterNameFromIAMRole(roleName string) string {
	// IAM roles follow pattern: <cluster-name>-<5-char-infra-id>-openshift-* or <cluster-name>-<5-char-infra-id>-master-role

	// First, try to split on "-openshift-" or "-master-role" or "-worker-role"
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

	// Now remove the infra ID (last 5-char segment after last hyphen)
	// Pattern: <cluster-name>-<5-char-infra-id>
	parts := strings.Split(prefix, "-")
	if len(parts) < 2 {
		return ""
	}

	// The infra ID is the last segment and should be 5 alphanumeric characters
	lastSegment := parts[len(parts)-1]
	if len(lastSegment) == 5 {
		// Remove the infra ID and return the cluster name
		return strings.Join(parts[0:len(parts)-1], "-")
	}

	// If the last segment isn't exactly 5 chars, this might be an old-style role
	// or a different naming convention - return empty to skip
	return ""
}

// detectOrphanedEBSVolumes finds EBS volumes tagged with cluster info but no matching cluster
func (j *Janitor) detectOrphanedEBSVolumes(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	ec2Client := ec2.NewFromConfig(cfg)

	// List all EBS volumes in the region
	result, err := ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, volume := range result.Volumes {
		volumeID := aws.ToString(volume.VolumeId)

		// Check for ManagedBy=ocpctl tag
		managedByOcpctl := getTagValue(volume.Tags, "ManagedBy") == "ocpctl"
		clusterNameFromTag := getTagValue(volume.Tags, "ClusterName")

		// Also check for kubernetes.io/cluster/{name} tag (created by Kubernetes PVC)
		var kubernetesClusterName string
		for _, tag := range volume.Tags {
			key := aws.ToString(tag.Key)
			if strings.HasPrefix(key, "kubernetes.io/cluster/") &&
			   (aws.ToString(tag.Value) == "owned" || aws.ToString(tag.Value) == "shared") {
				kubernetesClusterName = strings.TrimPrefix(key, "kubernetes.io/cluster/")
				break
			}
		}

		// Prefer kubernetes tag over ManagedBy tag for PVCs
		var clusterName string
		if kubernetesClusterName != "" {
			// Extract cluster name from infraID (pattern: {clustername}-{5chars})
			parts := strings.Split(kubernetesClusterName, "-")
			if len(parts) < 2 {
				continue
			}
			// Remove the 5-char suffix to get cluster name
			clusterName = strings.Join(parts[0:len(parts)-1], "-")
		} else if managedByOcpctl {
			clusterName = clusterNameFromTag
		}

		if clusterName == "" {
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			volumeName := getTagValue(volume.Tags, "Name")
			if volumeName == "" {
				volumeName = volumeID
			}

			orphans = append(orphans, OrphanedResource{
				Type:         "EBSVolume",
				ResourceID:   volumeID,
				ResourceName: volumeName,
				Region:       cfg.Region,
				Tags:         tagsToMap(volume.Tags),
			})
		}
	}

	return orphans, nil
}

// detectOrphanedElasticIPs finds Elastic IPs tagged with cluster info but no matching cluster
func (j *Janitor) detectOrphanedElasticIPs(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	ec2Client := ec2.NewFromConfig(cfg)

	// List all Elastic IPs in the region
	result, err := ec2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, err
	}

	orphans := []OrphanedResource{}
	for _, address := range result.Addresses {
		allocationID := aws.ToString(address.AllocationId)
		if allocationID == "" {
			// Skip classic EIPs without allocation ID
			continue
		}

		// Check for ManagedBy=ocpctl tag
		managedByOcpctl := getTagValue(address.Tags, "ManagedBy") == "ocpctl"
		clusterNameFromTag := getTagValue(address.Tags, "ClusterName")

		// Also check for kubernetes.io/cluster/{name} tag (created by LoadBalancer services)
		var kubernetesClusterName string
		for _, tag := range address.Tags {
			key := aws.ToString(tag.Key)
			if strings.HasPrefix(key, "kubernetes.io/cluster/") &&
			   (aws.ToString(tag.Value) == "owned" || aws.ToString(tag.Value) == "shared") {
				kubernetesClusterName = strings.TrimPrefix(key, "kubernetes.io/cluster/")
				break
			}
		}

		// Prefer kubernetes tag over ManagedBy tag
		var clusterName string
		if kubernetesClusterName != "" {
			// Extract cluster name from infraID (pattern: {clustername}-{5chars})
			parts := strings.Split(kubernetesClusterName, "-")
			if len(parts) < 2 {
				continue
			}
			// Remove the 5-char suffix to get cluster name
			clusterName = strings.Join(parts[0:len(parts)-1], "-")
		} else if managedByOcpctl {
			clusterName = clusterNameFromTag
		}

		if clusterName == "" {
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			eipName := getTagValue(address.Tags, "Name")
			if eipName == "" {
				eipName = aws.ToString(address.PublicIp)
			}

			orphans = append(orphans, OrphanedResource{
				Type:         "ElasticIP",
				ResourceID:   allocationID,
				ResourceName: eipName,
				Region:       cfg.Region,
				Tags:         tagsToMap(address.Tags),
			})
		}
	}

	return orphans, nil
}

// detectOrphanedCloudWatchLogGroups finds CloudWatch log groups for clusters that no longer exist
func (j *Janitor) detectOrphanedCloudWatchLogGroups(ctx context.Context, cfg aws.Config, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	cwlClient := cloudwatchlogs.NewFromConfig(cfg)

	orphans := []OrphanedResource{}

	// List all log groups
	var nextToken *string
	for {
		result, err := cwlClient.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
			NextToken: nextToken,
		})
		if err != nil {
			return nil, err
		}

		for _, logGroup := range result.LogGroups {
			logGroupName := aws.ToString(logGroup.LogGroupName)

			// Check for EKS-related log groups: /aws/eks/{clustername}/*
			if strings.HasPrefix(logGroupName, "/aws/eks/") {
				parts := strings.Split(logGroupName, "/")
				if len(parts) >= 4 {
					clusterName := parts[3]

					// Check if cluster exists in database
					cluster, exists := clustersByName[clusterName]
					if !exists || cluster.Status == types.ClusterStatusDestroyed {
						orphans = append(orphans, OrphanedResource{
							Type:         "CloudWatchLogGroup",
							ResourceID:   logGroupName,
							ResourceName: logGroupName,
							Region:       cfg.Region,
							Tags:         map[string]string{}, // CWL doesn't support tags on log groups
						})
					}
				}
			}
			// Only check EKS log groups - CloudWatch Log Groups dont support tags
			// OpenShift clusters dont create log groups in CloudWatch by default

		}

		if result.NextToken == nil {
			break
		}
		nextToken = result.NextToken
	}

	return orphans, nil
}
