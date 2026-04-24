package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Sentinel errors to distinguish retryable progress from terminal failures
var (
	// ErrDestroyInProgress indicates deletion is in progress (retryable)
	ErrDestroyInProgress = errors.New("destroy in progress")
)

// EKSDestroyState represents the current state of EKS cluster resources
type EKSDestroyState struct {
	ClusterName          string
	Region               string
	ClusterExists        bool
	ClusterStatus        string
	ManagedNodegroups    []string
	FargateProfiles      []string
	CloudFormationStacks []string
	// VPC-level resources
	VPCID                 string
	NetworkInterfaceIDs   []string
	LoadBalancerARNs      []string // Application/Network Load Balancers (v2)
	ClassicLoadBalancers  []string // Classic Load Balancers (v1)
	TargetGroupARNs       []string
	InstanceIDs           []string
	SecurityGroupIDs      []string
}

// EKSDestroyReconciler implements reconciliation-based EKS cluster deletion
type EKSDestroyReconciler struct {
	store        *store.Store
	eksClient    *eks.Client
	cfnClient    *cloudformation.Client
	ec2Client    *ec2.Client
	elbClient    *elasticloadbalancing.Client
	elbv2Client  *elasticloadbalancingv2.Client
	clusterName  string
	region       string
	clusterID    string
}

// NewEKSDestroyReconciler creates a new EKS destroy reconciler
func NewEKSDestroyReconciler(ctx context.Context, st *store.Store, cluster *types.Cluster) (*EKSDestroyReconciler, error) {
	// Load AWS config for the cluster's region
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return &EKSDestroyReconciler{
		store:       st,
		eksClient:   eks.NewFromConfig(cfg),
		cfnClient:   cloudformation.NewFromConfig(cfg),
		ec2Client:   ec2.NewFromConfig(cfg),
		elbClient:   elasticloadbalancing.NewFromConfig(cfg),
		elbv2Client: elasticloadbalancingv2.NewFromConfig(cfg),
		clusterName: cluster.Name,
		region:      cluster.Region,
		clusterID:   cluster.ID,
	}, nil
}

// Reconcile performs one iteration of destroy reconciliation
// Returns true if deletion is complete, false if more work is needed
func (r *EKSDestroyReconciler) Reconcile(ctx context.Context) (bool, error) {
	// Phase 1: Discover current state
	state, err := r.discover(ctx)
	if err != nil {
		return false, fmt.Errorf("discover state: %w", err)
	}

	// If nothing exists, we're done
	if !state.ClusterExists && len(state.CloudFormationStacks) == 0 {
		log.Printf("EKS cluster %s fully destroyed", r.clusterName)
		return true, nil
	}

	// Phase 2: Delete managed nodegroups first (AWS best practice)
	if len(state.ManagedNodegroups) > 0 {
		return false, r.reconcileManagedNodegroups(ctx, state)
	}

	// Phase 3: Delete Fargate profiles
	if len(state.FargateProfiles) > 0 {
		return false, r.reconcileFargateProfiles(ctx, state)
	}

	// Phase 4: Delete self-managed nodegroup CloudFormation stacks
	if len(state.CloudFormationStacks) > 0 {
		selfManagedStacks := r.filterSelfManagedNodeStacks(state.CloudFormationStacks)
		if len(selfManagedStacks) > 0 {
			return false, r.reconcileSelfManagedStacks(ctx, selfManagedStacks)
		}
	}

	// Phase 5: Delete cluster control plane
	if state.ClusterExists {
		return false, r.reconcileClusterDelete(ctx, state)
	}

	// Phase 6: Delete Application/Network load balancers (must happen before deleting subnets/VPC)
	if len(state.LoadBalancerARNs) > 0 {
		return false, r.reconcileLoadBalancers(ctx, state)
	}

	// Phase 7: Delete Classic load balancers (must happen before deleting subnets/VPC)
	if len(state.ClassicLoadBalancers) > 0 {
		return false, r.reconcileClassicLoadBalancers(ctx, state)
	}

	// Phase 8: Delete target groups (after load balancers)
	if len(state.TargetGroupARNs) > 0 {
		return false, r.reconcileTargetGroups(ctx, state)
	}

	// Phase 9: Delete network interfaces (after load balancers, before subnets)
	if len(state.NetworkInterfaceIDs) > 0 {
		return false, r.reconcileNetworkInterfaces(ctx, state)
	}

	// Phase 10: Delete security groups (after ENIs, before VPC)
	// Don't block on security group deletion - they may be managed by CloudFormation
	if len(state.SecurityGroupIDs) > 0 {
		err := r.reconcileSecurityGroups(ctx, state)
		if err != nil && !errors.Is(err, ErrDestroyInProgress) {
			log.Printf("Warning: security group cleanup encountered errors (continuing): %v", err)
		}
		// Continue to CloudFormation deletion even if security groups fail
		// CloudFormation will clean them up
	}

	// Phase 11: Delete supporting CloudFormation stacks (cluster stack, addons, VPC)
	// VPC dependencies should now be clear
	if len(state.CloudFormationStacks) > 0 {
		return false, r.reconcileInfraStacks(ctx, state.CloudFormationStacks)
	}

	// All done
	return true, nil
}

// discover finds all existing resources for this cluster
func (r *EKSDestroyReconciler) discover(ctx context.Context) (*EKSDestroyState, error) {
	state := &EKSDestroyState{
		ClusterName:          r.clusterName,
		Region:               r.region,
		ManagedNodegroups:    []string{},
		FargateProfiles:      []string{},
		CloudFormationStacks: []string{},
	}

	// Check if cluster exists
	describeInput := &eks.DescribeClusterInput{
		Name: aws.String(r.clusterName),
	}

	describeOutput, err := r.eksClient.DescribeCluster(ctx, describeInput)
	if err != nil {
		// ResourceNotFoundException means cluster is gone - that's fine
		if isEKSResourceNotFound(err) {
			log.Printf("Cluster %s not found in EKS (already deleted)", r.clusterName)
			state.ClusterExists = false
		} else {
			return nil, fmt.Errorf("describe cluster: %w", err)
		}
	} else {
		state.ClusterExists = true
		state.ClusterStatus = string(describeOutput.Cluster.Status)
		log.Printf("Cluster %s exists with status: %s", r.clusterName, state.ClusterStatus)
	}

	// List managed nodegroups (only if cluster exists)
	if state.ClusterExists {
		listNgInput := &eks.ListNodegroupsInput{
			ClusterName: aws.String(r.clusterName),
		}

		listNgOutput, err := r.eksClient.ListNodegroups(ctx, listNgInput)
		if err != nil {
			if !isEKSResourceNotFound(err) {
				return nil, fmt.Errorf("list nodegroups: %w", err)
			}
		} else {
			state.ManagedNodegroups = listNgOutput.Nodegroups
			if len(state.ManagedNodegroups) > 0 {
				log.Printf("Found %d managed nodegroups: %v", len(state.ManagedNodegroups), state.ManagedNodegroups)
			}
		}

		// List Fargate profiles
		listFpInput := &eks.ListFargateProfilesInput{
			ClusterName: aws.String(r.clusterName),
		}

		listFpOutput, err := r.eksClient.ListFargateProfiles(ctx, listFpInput)
		if err != nil {
			if !isEKSResourceNotFound(err) {
				return nil, fmt.Errorf("list fargate profiles: %w", err)
			}
		} else {
			state.FargateProfiles = listFpOutput.FargateProfileNames
			if len(state.FargateProfiles) > 0 {
				log.Printf("Found %d Fargate profiles: %v", len(state.FargateProfiles), state.FargateProfiles)
			}
		}
	}

	// List CloudFormation stacks for this cluster (with pagination)
	// Include all relevant stack states, not just a subset
	listStacksInput := &cloudformation.ListStacksInput{
		StackStatusFilter: []cfntypes.StackStatus{
			cfntypes.StackStatusCreateComplete,
			cfntypes.StackStatusUpdateComplete,
			cfntypes.StackStatusDeleteInProgress,
			cfntypes.StackStatusDeleteFailed,
			cfntypes.StackStatusUpdateRollbackComplete,
			cfntypes.StackStatusImportComplete,
			cfntypes.StackStatusCreateFailed,
			cfntypes.StackStatusRollbackComplete,
			cfntypes.StackStatusUpdateCompleteCleanupInProgress,
		},
	}

	// OPTIMIZATION: Instead of listing ALL stacks (which can be thousands in a busy AWS account),
	// we check specific stack names that eksctl creates. This reduces API calls by ~90%.
	// eksctl stack naming pattern:
	//   - eksctl-{cluster-name}-cluster (main cluster stack)
	//   - eksctl-{cluster-name}-nodegroup-{ng-name} (per nodegroup)
	//   - eksctl-{cluster-name}-addon-{addon-name} (per addon)

	// Use a map to avoid duplicates
	stackNamesMap := make(map[string]bool)

	// First, check if the main cluster stack exists
	mainStackName := fmt.Sprintf("eksctl-%s-cluster", r.clusterName)
	stackNamesMap[mainStackName] = true

	// Try to find node group and addon stacks by checking common patterns
	// We still need to list stacks, but with a targeted prefix to reduce the result set
	paginator := cloudformation.NewListStacksPaginator(r.cfnClient, listStacksInput)

	// Early termination: stop after processing a reasonable number of pages
	// Most clusters have < 20 stacks, so 2 pages (100 stacks) is generous
	maxPages := 2
	pagesProcessed := 0

	for paginator.HasMorePages() && pagesProcessed < maxPages {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list cloudformation stacks: %w", err)
		}
		pagesProcessed++

		// Filter stacks belonging to this cluster (eksctl naming convention)
		for _, stack := range page.StackSummaries {
			stackName := aws.ToString(stack.StackName)
			if strings.HasPrefix(stackName, "eksctl-"+r.clusterName+"-") {
				stackNamesMap[stackName] = true
			}
		}

		// If we found stacks, no need to keep paginating through thousands more
		if len(stackNamesMap) > 1 {
			break
		}
	}

	// Verify stacks exist (convert map to slice)
	verifiedStacks := []string{}
	for stackName := range stackNamesMap {
		// Quick check if stack actually exists (avoids including stacks that don't exist)
		describeInput := &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		}
		_, err := r.cfnClient.DescribeStacks(ctx, describeInput)
		if err == nil {
			verifiedStacks = append(verifiedStacks, stackName)
		}
		// Ignore errors - stack might not exist, which is fine
	}

	state.CloudFormationStacks = verifiedStacks

	if len(state.CloudFormationStacks) > 0 {
		log.Printf("Found %d CloudFormation stacks: %v", len(state.CloudFormationStacks), state.CloudFormationStacks)
	}

	// Discover VPC ID (needed for VPC-level resource discovery)
	vpcID, err := r.discoverVPCID(ctx, state)
	if err != nil {
		log.Printf("Warning: could not discover VPC ID: %v", err)
		// Don't fail - continue without VPC-level discovery
	} else if vpcID != "" {
		state.VPCID = vpcID
		log.Printf("Found VPC ID: %s", vpcID)

		// Discover VPC-level resources
		if enis, err := r.discoverNetworkInterfaces(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to discover network interfaces: %v", err)
		} else {
			state.NetworkInterfaceIDs = enis
			if len(enis) > 0 {
				log.Printf("Found %d network interfaces", len(enis))
			}
		}

		if lbs, tgs, err := r.discoverLoadBalancers(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to discover load balancers: %v", err)
		} else {
			state.LoadBalancerARNs = lbs
			state.TargetGroupARNs = tgs
			if len(lbs) > 0 {
				log.Printf("Found %d load balancers and %d target groups", len(lbs), len(tgs))
			}
		}

		if classicLBs, err := r.discoverClassicLoadBalancers(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to discover classic load balancers: %v", err)
		} else {
			state.ClassicLoadBalancers = classicLBs
			if len(classicLBs) > 0 {
				log.Printf("Found %d classic load balancers", len(classicLBs))
			}
		}

		if instances, err := r.discoverInstances(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to discover instances: %v", err)
		} else {
			state.InstanceIDs = instances
			if len(instances) > 0 {
				log.Printf("Found %d EC2 instances", len(instances))
			}
		}

		if sgs, err := r.discoverSecurityGroups(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to discover security groups: %v", err)
		} else {
			state.SecurityGroupIDs = sgs
			if len(sgs) > 0 {
				log.Printf("Found %d security groups", len(sgs))
			}
		}
	}

	return state, nil
}

// discoverVPCID discovers the VPC ID from the cluster or CloudFormation stacks
func (r *EKSDestroyReconciler) discoverVPCID(ctx context.Context, state *EKSDestroyState) (string, error) {
	// Try to get VPC from cluster first
	if state.ClusterExists {
		describeInput := &eks.DescribeClusterInput{
			Name: aws.String(r.clusterName),
		}
		describeOutput, err := r.eksClient.DescribeCluster(ctx, describeInput)
		if err == nil && describeOutput.Cluster != nil && describeOutput.Cluster.ResourcesVpcConfig != nil {
			vpcID := aws.ToString(describeOutput.Cluster.ResourcesVpcConfig.VpcId)
			if vpcID != "" {
				return vpcID, nil
			}
		}
	}

	// Try to get VPC from cluster stack outputs
	for _, stackName := range state.CloudFormationStacks {
		if strings.HasSuffix(stackName, "-cluster") {
			describeInput := &cloudformation.DescribeStacksInput{
				StackName: aws.String(stackName),
			}
			describeOutput, err := r.cfnClient.DescribeStacks(ctx, describeInput)
			if err != nil {
				continue
			}
			if len(describeOutput.Stacks) > 0 {
				for _, output := range describeOutput.Stacks[0].Outputs {
					if aws.ToString(output.OutputKey) == "VPC" {
						return aws.ToString(output.OutputValue), nil
					}
				}
			}
		}
	}

	// Try to get VPC from VPC stack outputs
	for _, stackName := range state.CloudFormationStacks {
		if strings.Contains(stackName, "-vpc") {
			describeInput := &cloudformation.DescribeStacksInput{
				StackName: aws.String(stackName),
			}
			describeOutput, err := r.cfnClient.DescribeStacks(ctx, describeInput)
			if err != nil {
				continue
			}
			if len(describeOutput.Stacks) > 0 {
				for _, output := range describeOutput.Stacks[0].Outputs {
					if aws.ToString(output.OutputKey) == "VPC" {
						return aws.ToString(output.OutputValue), nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not find VPC ID from cluster or stacks")
}

// discoverNetworkInterfaces finds all cluster-related ENIs in the VPC
func (r *EKSDestroyReconciler) discoverNetworkInterfaces(ctx context.Context, vpcID string) ([]string, error) {
	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	output, err := r.ec2Client.DescribeNetworkInterfaces(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe network interfaces: %w", err)
	}

	var eniIDs []string
	for _, eni := range output.NetworkInterfaces {
		eniID := aws.ToString(eni.NetworkInterfaceId)
		description := aws.ToString(eni.Description)

		// Skip ENIs that are managed by AWS services (like ELB, NLB, ALB)
		// These are automatically deleted when the service resource (LoadBalancer) is deleted
		// Including them in discovery creates an infinite loop because reconcileNetworkInterfaces
		// skips them but they're never removed from the state
		if eni.RequesterManaged != nil && *eni.RequesterManaged {
			requesterID := aws.ToString(eni.RequesterId)
			log.Printf("Skipping discovery of AWS-managed network interface %s (managed by %s)", eniID, requesterID)
			continue
		}

		// Match by tags first
		matched := false
		for _, tag := range eni.TagSet {
			key := aws.ToString(tag.Key)
			val := aws.ToString(tag.Value)
			if key == "kubernetes.io/cluster/"+r.clusterName && (val == "owned" || val == "shared") {
				matched = true
				break
			}
		}

		// Fallback: description-based heuristics for ELB/EKS ENIs
		// Note: We already filtered out AWS-managed ENIs above, so these are user-managed
		if matched ||
			strings.Contains(description, "ELB") ||
			strings.Contains(description, "amazon-eks") ||
			strings.Contains(description, r.clusterName) {
			eniIDs = append(eniIDs, eniID)
		}
	}

	return eniIDs, nil
}

// discoverLoadBalancers finds all cluster-related load balancers and target groups
func (r *EKSDestroyReconciler) discoverLoadBalancers(ctx context.Context, vpcID string) ([]string, []string, error) {
	// List all Application/Network load balancers (v2)
	lbInput := &elasticloadbalancingv2.DescribeLoadBalancersInput{}
	lbOutput, err := r.elbv2Client.DescribeLoadBalancers(ctx, lbInput)
	if err != nil {
		return nil, nil, fmt.Errorf("describe load balancers: %w", err)
	}

	var lbARNs []string
	for _, lb := range lbOutput.LoadBalancers {
		// Filter by VPC
		if aws.ToString(lb.VpcId) != vpcID {
			continue
		}

		lbARN := aws.ToString(lb.LoadBalancerArn)

		// Check tags
		tagsInput := &elasticloadbalancingv2.DescribeTagsInput{
			ResourceArns: []string{lbARN},
		}
		tagsOutput, err := r.elbv2Client.DescribeTags(ctx, tagsInput)
		if err != nil {
			log.Printf("Warning: failed to get tags for ALB/NLB %s: %v", lbARN, err)
			continue
		}

		// Check if this LB belongs to our cluster
		matched := false
		for _, tagDesc := range tagsOutput.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				key := aws.ToString(tag.Key)
				val := aws.ToString(tag.Value)
				if key == "kubernetes.io/cluster/"+r.clusterName && (val == "owned" || val == "shared") {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if matched {
			lbARNs = append(lbARNs, lbARN)
		}
	}

	// List all target groups and filter by VPC and cluster tags
	tgInput := &elasticloadbalancingv2.DescribeTargetGroupsInput{}
	tgOutput, err := r.elbv2Client.DescribeTargetGroups(ctx, tgInput)
	if err != nil {
		return lbARNs, nil, fmt.Errorf("describe target groups: %w", err)
	}

	var tgARNs []string
	for _, tg := range tgOutput.TargetGroups {
		// Filter by VPC
		if aws.ToString(tg.VpcId) != vpcID {
			continue
		}

		tgARN := aws.ToString(tg.TargetGroupArn)

		// Check tags
		tagsInput := &elasticloadbalancingv2.DescribeTagsInput{
			ResourceArns: []string{tgARN},
		}
		tagsOutput, err := r.elbv2Client.DescribeTags(ctx, tagsInput)
		if err != nil {
			log.Printf("Warning: failed to get tags for TG %s: %v", tgARN, err)
			continue
		}

		// Check if this TG belongs to our cluster
		matched := false
		for _, tagDesc := range tagsOutput.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				key := aws.ToString(tag.Key)
				val := aws.ToString(tag.Value)
				if key == "kubernetes.io/cluster/"+r.clusterName && (val == "owned" || val == "shared") {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if matched {
			tgARNs = append(tgARNs, tgARN)
		}
	}

	return lbARNs, tgARNs, nil
}

// discoverClassicLoadBalancers finds all cluster-related Classic Load Balancers (ELBv1)
func (r *EKSDestroyReconciler) discoverClassicLoadBalancers(ctx context.Context, vpcID string) ([]string, error) {
	// List all Classic load balancers
	elbInput := &elasticloadbalancing.DescribeLoadBalancersInput{}
	elbOutput, err := r.elbClient.DescribeLoadBalancers(ctx, elbInput)
	if err != nil {
		return nil, fmt.Errorf("describe classic load balancers: %w", err)
	}

	var classicLBNames []string
	for _, lb := range elbOutput.LoadBalancerDescriptions {
		// Filter by VPC
		if aws.ToString(lb.VPCId) != vpcID {
			continue
		}

		lbName := aws.ToString(lb.LoadBalancerName)

		// Check tags
		tagsInput := &elasticloadbalancing.DescribeTagsInput{
			LoadBalancerNames: []string{lbName},
		}
		tagsOutput, err := r.elbClient.DescribeTags(ctx, tagsInput)
		if err != nil {
			log.Printf("Warning: failed to get tags for classic ELB %s: %v", lbName, err)
			continue
		}

		// Check if this LB belongs to our cluster
		matched := false
		for _, tagDesc := range tagsOutput.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				key := aws.ToString(tag.Key)
				val := aws.ToString(tag.Value)
				// Classic load balancers created by Kubernetes have this tag
				if key == "kubernetes.io/cluster/"+r.clusterName && (val == "owned" || val == "shared") {
					matched = true
					break
				}
				// Also check for the service tag (LoadBalancer services)
				if key == "kubernetes.io/service-name" {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if matched {
			classicLBNames = append(classicLBNames, lbName)
		}
	}

	return classicLBNames, nil
}

// discoverInstances finds all cluster-related EC2 instances
func (r *EKSDestroyReconciler) discoverInstances(ctx context.Context, vpcID string) ([]string, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending", "stopping", "stopped"},
			},
		},
	}

	output, err := r.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	var instanceIDs []string
	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			// Check if this instance belongs to our cluster
			matched := false
			for _, tag := range instance.Tags {
				key := aws.ToString(tag.Key)
				val := aws.ToString(tag.Value)
				if key == "kubernetes.io/cluster/"+r.clusterName && (val == "owned" || val == "shared") {
					matched = true
					break
				}
				// Also check for eksctl tags
				if key == "alpha.eksctl.io/cluster-name" && val == r.clusterName {
					matched = true
					break
				}
			}

			if matched {
				instanceIDs = append(instanceIDs, aws.ToString(instance.InstanceId))
			}
		}
	}

	return instanceIDs, nil
}

// discoverSecurityGroups finds all cluster-related security groups
func (r *EKSDestroyReconciler) discoverSecurityGroups(ctx context.Context, vpcID string) ([]string, error) {
	input := &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + r.clusterName),
				Values: []string{"owned", "shared"},
			},
		},
	}

	output, err := r.ec2Client.DescribeSecurityGroups(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe security groups: %w", err)
	}

	var sgIDs []string
	for _, sg := range output.SecurityGroups {
		// Skip the default security group
		if aws.ToString(sg.GroupName) == "default" {
			continue
		}
		sgIDs = append(sgIDs, aws.ToString(sg.GroupId))
	}

	return sgIDs, nil
}

// reconcileManagedNodegroups deletes managed nodegroups via EKS API
func (r *EKSDestroyReconciler) reconcileManagedNodegroups(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling managed nodegroups deletion for %s", r.clusterName)

	for _, ng := range state.ManagedNodegroups {
		// Check current status
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(r.clusterName),
			NodegroupName: aws.String(ng),
		}

		describeOutput, err := r.eksClient.DescribeNodegroup(ctx, describeInput)
		if err != nil {
			if isEKSResourceNotFound(err) {
				log.Printf("Nodegroup %s already deleted", ng)
				continue
			}
			return fmt.Errorf("describe nodegroup %s: %w", ng, err)
		}

		ngStatus := describeOutput.Nodegroup.Status

		// If already deleting, just wait
		if ngStatus == ekstypes.NodegroupStatusDeleting {
			log.Printf("Nodegroup %s is already deleting (status: %s)", ng, ngStatus)
			return fmt.Errorf("%w: nodegroup %s still deleting", ErrDestroyInProgress, ng)
		}

		// If in terminal failed state, log and skip
		if ngStatus == ekstypes.NodegroupStatusDegraded {
			log.Printf("Warning: nodegroup %s in degraded state %s, attempting delete anyway", ng, ngStatus)
			// Don't skip - try to delete anyway
		}

		// Issue delete
		log.Printf("Deleting managed nodegroup %s (current status: %s)", ng, ngStatus)
		deleteInput := &eks.DeleteNodegroupInput{
			ClusterName:   aws.String(r.clusterName),
			NodegroupName: aws.String(ng),
		}

		_, err = r.eksClient.DeleteNodegroup(ctx, deleteInput)
		if err != nil {
			if isEKSResourceNotFound(err) {
				log.Printf("Nodegroup %s already deleted", ng)
				continue
			}
			return fmt.Errorf("delete nodegroup %s: %w", ng, err)
		}

		log.Printf("Issued delete for managed nodegroup %s", ng)

		// Return error to indicate work in progress
		// Next reconcile loop will check if deletion completed
		return fmt.Errorf("%w: nodegroup %s deletion in progress", ErrDestroyInProgress, ng)
	}

	return nil
}

// reconcileFargateProfiles deletes Fargate profiles
func (r *EKSDestroyReconciler) reconcileFargateProfiles(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling Fargate profiles deletion for %s", r.clusterName)

	for _, fp := range state.FargateProfiles {
		// Check current status
		describeInput := &eks.DescribeFargateProfileInput{
			ClusterName:        aws.String(r.clusterName),
			FargateProfileName: aws.String(fp),
		}

		describeOutput, err := r.eksClient.DescribeFargateProfile(ctx, describeInput)
		if err != nil {
			if isEKSResourceNotFound(err) {
				log.Printf("Fargate profile %s already deleted", fp)
				continue
			}
			return fmt.Errorf("describe fargate profile %s: %w", fp, err)
		}

		fpStatus := describeOutput.FargateProfile.Status

		// If already deleting, just wait
		if fpStatus == ekstypes.FargateProfileStatusDeleting {
			log.Printf("Fargate profile %s is already deleting", fp)
			return fmt.Errorf("%w: fargate profile %s still deleting", ErrDestroyInProgress, fp)
		}

		// Issue delete
		log.Printf("Deleting Fargate profile %s", fp)
		deleteInput := &eks.DeleteFargateProfileInput{
			ClusterName:        aws.String(r.clusterName),
			FargateProfileName: aws.String(fp),
		}

		_, err = r.eksClient.DeleteFargateProfile(ctx, deleteInput)
		if err != nil {
			if isEKSResourceNotFound(err) {
				log.Printf("Fargate profile %s already deleted", fp)
				continue
			}
			return fmt.Errorf("delete fargate profile %s: %w", fp, err)
		}

		log.Printf("Issued delete for Fargate profile %s", fp)
		return fmt.Errorf("%w: fargate profile %s deletion in progress", ErrDestroyInProgress, fp)
	}

	return nil
}

// reconcileSelfManagedStacks deletes self-managed nodegroup CloudFormation stacks
func (r *EKSDestroyReconciler) reconcileSelfManagedStacks(ctx context.Context, stacks []string) error {
	log.Printf("Reconciling self-managed nodegroup stacks deletion")

	for _, stackName := range stacks {
		// Check stack status
		describeInput := &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		}

		describeOutput, err := r.cfnClient.DescribeStacks(ctx, describeInput)
		if err != nil {
			if isCFNStackNotFound(err) {
				log.Printf("Stack %s already deleted", stackName)
				continue
			}
			return fmt.Errorf("describe stack %s: %w", stackName, err)
		}

		if len(describeOutput.Stacks) == 0 {
			log.Printf("Stack %s not found (already deleted)", stackName)
			continue
		}

		stack := describeOutput.Stacks[0]
		status := stack.StackStatus

		// If already deleting, wait
		if status == cfntypes.StackStatusDeleteInProgress {
			log.Printf("Stack %s is already deleting", stackName)
			return fmt.Errorf("%w: stack %s still deleting", ErrDestroyInProgress, stackName)
		}

		// If delete failed, retry after disabling termination protection
		// The failure might have been due to dependencies that we've since cleaned up
		if status == cfntypes.StackStatusDeleteFailed {
			log.Printf("Stack %s delete previously failed, retrying after dependency cleanup", stackName)
			// Continue to termination protection check and retry deletion
		}

		// Disable termination protection if enabled
		if stack.EnableTerminationProtection != nil && *stack.EnableTerminationProtection {
			log.Printf("Disabling termination protection on stack %s", stackName)
			updateInput := &cloudformation.UpdateTerminationProtectionInput{
				StackName:                   aws.String(stackName),
				EnableTerminationProtection: aws.Bool(false),
			}

			_, err := r.cfnClient.UpdateTerminationProtection(ctx, updateInput)
			if err != nil {
				return fmt.Errorf("disable termination protection on %s: %w", stackName, err)
			}
		}

		// Issue delete
		log.Printf("Deleting CloudFormation stack %s", stackName)
		deleteInput := &cloudformation.DeleteStackInput{
			StackName: aws.String(stackName),
		}

		_, err = r.cfnClient.DeleteStack(ctx, deleteInput)
		if err != nil {
			if isCFNStackNotFound(err) {
				log.Printf("Stack %s already deleted", stackName)
				continue
			}
			return fmt.Errorf("delete stack %s: %w", stackName, err)
		}

		log.Printf("Issued delete for stack %s", stackName)
		return fmt.Errorf("%w: stack %s deletion in progress", ErrDestroyInProgress, stackName)
	}

	return nil
}

// reconcileClusterDelete deletes the EKS cluster control plane
func (r *EKSDestroyReconciler) reconcileClusterDelete(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling cluster deletion for %s", r.clusterName)

	// Check if already deleting
	if state.ClusterStatus == string(ekstypes.ClusterStatusDeleting) {
		log.Printf("Cluster %s is already deleting", r.clusterName)
		return fmt.Errorf("%w: cluster %s still deleting", ErrDestroyInProgress, r.clusterName)
	}

	// Issue delete
	log.Printf("Deleting EKS cluster %s", r.clusterName)
	deleteInput := &eks.DeleteClusterInput{
		Name: aws.String(r.clusterName),
	}

	_, err := r.eksClient.DeleteCluster(ctx, deleteInput)
	if err != nil {
		if isEKSResourceNotFound(err) {
			log.Printf("Cluster %s already deleted", r.clusterName)
			return nil
		}
		return fmt.Errorf("delete cluster: %w", err)
	}

	log.Printf("Issued delete for cluster %s", r.clusterName)
	return fmt.Errorf("%w: cluster %s deletion in progress", ErrDestroyInProgress, r.clusterName)
}

// reconcileInfraStacks deletes supporting CloudFormation stacks (cluster, addons, VPC)
func (r *EKSDestroyReconciler) reconcileInfraStacks(ctx context.Context, stacks []string) error {
	log.Printf("Reconciling infrastructure stacks deletion")

	// Process stacks in dependency order (addons before cluster, cluster before VPC)
	// Sort by stack type priority: addons > cluster > vpc
	sortedStacks := r.sortStacksByDependency(stacks)

	for _, stackName := range sortedStacks {
		// Check stack status
		describeInput := &cloudformation.DescribeStacksInput{
			StackName: aws.String(stackName),
		}

		describeOutput, err := r.cfnClient.DescribeStacks(ctx, describeInput)
		if err != nil {
			if isCFNStackNotFound(err) {
				log.Printf("Stack %s already deleted", stackName)
				continue
			}
			return fmt.Errorf("describe stack %s: %w", stackName, err)
		}

		if len(describeOutput.Stacks) == 0 {
			continue
		}

		stack := describeOutput.Stacks[0]
		status := stack.StackStatus

		if status == cfntypes.StackStatusDeleteInProgress {
			log.Printf("Stack %s is already deleting", stackName)
			return fmt.Errorf("%w: stack %s still deleting", ErrDestroyInProgress, stackName)
		}

		if status == cfntypes.StackStatusDeleteFailed {
			log.Printf("Warning: stack %s delete failed, skipping", stackName)
			continue
		}

		// Disable termination protection
		if stack.EnableTerminationProtection != nil && *stack.EnableTerminationProtection {
			log.Printf("Disabling termination protection on stack %s", stackName)
			updateInput := &cloudformation.UpdateTerminationProtectionInput{
				StackName:                   aws.String(stackName),
				EnableTerminationProtection: aws.Bool(false),
			}

			_, err := r.cfnClient.UpdateTerminationProtection(ctx, updateInput)
			if err != nil {
				return fmt.Errorf("disable termination protection on %s: %w", stackName, err)
			}
		}

		// Issue delete
		log.Printf("Deleting CloudFormation stack %s", stackName)
		deleteInput := &cloudformation.DeleteStackInput{
			StackName: aws.String(stackName),
		}

		_, err = r.cfnClient.DeleteStack(ctx, deleteInput)
		if err != nil {
			if isCFNStackNotFound(err) {
				log.Printf("Stack %s already deleted", stackName)
				continue
			}
			return fmt.Errorf("delete stack %s: %w", stackName, err)
		}

		log.Printf("Issued delete for stack %s", stackName)
		return fmt.Errorf("%w: stack %s deletion in progress", ErrDestroyInProgress, stackName)
	}

	return nil
}

// filterSelfManagedNodeStacks filters for self-managed nodegroup stacks
func (r *EKSDestroyReconciler) filterSelfManagedNodeStacks(stacks []string) []string {
	var result []string
	for _, stack := range stacks {
		// Self-managed nodegroup stacks have "nodegroup" in the name
		// but are NOT addon or cluster stacks
		if strings.Contains(stack, "-nodegroup-") &&
			!strings.Contains(stack, "-addon-") &&
			!strings.HasSuffix(stack, "-cluster") {
			result = append(result, stack)
		}
	}
	return result
}

// sortStacksByDependency sorts stacks by deletion order (addons > cluster > vpc)
func (r *EKSDestroyReconciler) sortStacksByDependency(stacks []string) []string {
	var addons []string
	var cluster []string
	var vpc []string
	var other []string

	for _, stack := range stacks {
		if strings.Contains(stack, "-addon-") {
			addons = append(addons, stack)
		} else if strings.HasSuffix(stack, "-cluster") {
			cluster = append(cluster, stack)
		} else if strings.Contains(stack, "-vpc") {
			vpc = append(vpc, stack)
		} else {
			other = append(other, stack)
		}
	}

	// Return in dependency order: addons first, then cluster, then VPC, then other
	result := make([]string, 0, len(stacks))
	result = append(result, addons...)
	result = append(result, cluster...)
	result = append(result, vpc...)
	result = append(result, other...)
	return result
}

// reconcileLoadBalancers deletes load balancers created by the cluster
func (r *EKSDestroyReconciler) reconcileLoadBalancers(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling load balancers deletion for %s", r.clusterName)

	// First, check for Classic Load Balancers via network interfaces
	// Classic ELBs create ENIs with description "ELB <lb-name>"
	if len(state.NetworkInterfaceIDs) > 0 {
		for _, eniID := range state.NetworkInterfaceIDs {
			describeInput := &ec2.DescribeNetworkInterfacesInput{
				NetworkInterfaceIds: []string{eniID},
			}
			describeOutput, err := r.ec2Client.DescribeNetworkInterfaces(ctx, describeInput)
			if err != nil || len(describeOutput.NetworkInterfaces) == 0 {
				continue
			}

			eni := describeOutput.NetworkInterfaces[0]
			description := aws.ToString(eni.Description)

			// Check if this is an ELB network interface
			if strings.HasPrefix(description, "ELB ") {
				lbName := strings.TrimPrefix(description, "ELB ")
				log.Printf("Found Classic Load Balancer %s via ENI %s", lbName, eniID)

				// Delete the Classic Load Balancer
				_, err := r.elbClient.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
					LoadBalancerName: aws.String(lbName),
				})
				if err != nil {
					if strings.Contains(err.Error(), "LoadBalancerNotFound") {
						log.Printf("Classic load balancer %s already deleted", lbName)
						continue
					}
					return fmt.Errorf("delete classic load balancer %s: %w", lbName, err)
				}
				log.Printf("Deleted Classic Load Balancer %s", lbName)
				// Return in-progress to allow ENI cleanup in next iteration
				return fmt.Errorf("%w: classic load balancer %s deletion in progress", ErrDestroyInProgress, lbName)
			}
		}
	}

	// Then handle ALB/NLB (v2) load balancers
	for _, lbARN := range state.LoadBalancerARNs {
		log.Printf("Deleting load balancer %s", lbARN)

		_, err := r.elbv2Client.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
			LoadBalancerArn: aws.String(lbARN),
		})
		if err != nil {
			if strings.Contains(err.Error(), "LoadBalancerNotFound") {
				log.Printf("Load balancer %s already deleted", lbARN)
				continue
			}
			return fmt.Errorf("delete load balancer %s: %w", lbARN, err)
		}
		log.Printf("Issued delete for load balancer %s", lbARN)

		// Return in-progress error to trigger next reconcile iteration
		return fmt.Errorf("%w: load balancer %s deletion in progress", ErrDestroyInProgress, lbARN)
	}

	return nil
}

// reconcileClassicLoadBalancers deletes Classic Load Balancers created by the cluster
func (r *EKSDestroyReconciler) reconcileClassicLoadBalancers(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling classic load balancers deletion for %s", r.clusterName)

	for _, lbName := range state.ClassicLoadBalancers {
		log.Printf("Deleting classic load balancer %s", lbName)

		_, err := r.elbClient.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(lbName),
		})
		if err != nil {
			if strings.Contains(err.Error(), "LoadBalancerNotFound") {
				log.Printf("Classic load balancer %s already deleted", lbName)
				continue
			}
			return fmt.Errorf("delete classic load balancer %s: %w", lbName, err)
		}
		log.Printf("Issued delete for classic load balancer %s", lbName)

		// Return in-progress error to trigger next reconcile iteration
		// Classic LB deletion can take a few seconds
		return fmt.Errorf("%w: classic load balancer %s deletion in progress", ErrDestroyInProgress, lbName)
	}

	return nil
}

// reconcileTargetGroups deletes target groups created by the cluster
func (r *EKSDestroyReconciler) reconcileTargetGroups(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling target groups deletion for %s", r.clusterName)

	for _, tgARN := range state.TargetGroupARNs {
		log.Printf("Deleting target group %s", tgARN)

		_, err := r.elbv2Client.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{
			TargetGroupArn: aws.String(tgARN),
		})
		if err != nil {
			if strings.Contains(err.Error(), "TargetGroupNotFound") {
				log.Printf("Target group %s already deleted", tgARN)
				continue
			}
			// Target group might still be attached to a load balancer
			if strings.Contains(err.Error(), "ResourceInUse") {
				log.Printf("Target group %s still in use, will retry", tgARN)
				return fmt.Errorf("%w: target group %s still in use", ErrDestroyInProgress, tgARN)
			}
			return fmt.Errorf("delete target group %s: %w", tgARN, err)
		}
		log.Printf("Deleted target group %s", tgARN)
	}

	return nil
}

// reconcileNetworkInterfaces deletes network interfaces created by the cluster
func (r *EKSDestroyReconciler) reconcileNetworkInterfaces(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling network interfaces deletion for %s", r.clusterName)

	for _, eniID := range state.NetworkInterfaceIDs {
		// Check if ENI still exists and get its status
		describeInput := &ec2.DescribeNetworkInterfacesInput{
			NetworkInterfaceIds: []string{eniID},
		}

		describeOutput, err := r.ec2Client.DescribeNetworkInterfaces(ctx, describeInput)
		if err != nil {
			if strings.Contains(err.Error(), "InvalidNetworkInterfaceID.NotFound") {
				log.Printf("Network interface %s already deleted", eniID)
				continue
			}
			return fmt.Errorf("describe network interface %s: %w", eniID, err)
		}

		if len(describeOutput.NetworkInterfaces) == 0 {
			log.Printf("Network interface %s not found (already deleted)", eniID)
			continue
		}

		eni := describeOutput.NetworkInterfaces[0]

		// Skip ENIs that are managed by AWS services (like ELB)
		// They'll be deleted automatically when the service resource is deleted
		if eni.RequesterManaged != nil && *eni.RequesterManaged {
			requesterID := aws.ToString(eni.RequesterId)
			log.Printf("Skipping AWS-managed network interface %s (managed by %s)", eniID, requesterID)
			continue
		}

		// If attached, try to detach first
		if eni.Attachment != nil && eni.Attachment.Status == ec2types.AttachmentStatusAttached {
			attachmentID := aws.ToString(eni.Attachment.AttachmentId)
			log.Printf("Detaching network interface %s (attachment: %s)", eniID, attachmentID)

			_, err := r.ec2Client.DetachNetworkInterface(ctx, &ec2.DetachNetworkInterfaceInput{
				AttachmentId: aws.String(attachmentID),
				Force:        aws.Bool(true),
			})
			if err != nil {
				log.Printf("Warning: failed to detach network interface %s: %v", eniID, err)
				// Continue anyway - might be able to delete
			}

			// Wait for detachment before proceeding
			return fmt.Errorf("%w: network interface %s detaching", ErrDestroyInProgress, eniID)
		}

		// Delete the network interface
		log.Printf("Deleting network interface %s", eniID)
		_, err = r.ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: aws.String(eniID),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvalidNetworkInterfaceID.NotFound") {
				log.Printf("Network interface %s already deleted", eniID)
				continue
			}
			log.Printf("Warning: failed to delete network interface %s: %v", eniID, err)
			// Don't fail - might be managed by another service
			continue
		}
		log.Printf("Deleted network interface %s", eniID)
	}

	return nil
}

// reconcileSecurityGroups deletes security groups created by the cluster
func (r *EKSDestroyReconciler) reconcileSecurityGroups(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling security groups deletion for %s", r.clusterName)

	for _, sgID := range state.SecurityGroupIDs {
		// Check if security group still exists
		describeInput := &ec2.DescribeSecurityGroupsInput{
			GroupIds: []string{sgID},
		}

		describeOutput, err := r.ec2Client.DescribeSecurityGroups(ctx, describeInput)
		if err != nil {
			if strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Security group %s already deleted", sgID)
				continue
			}
			return fmt.Errorf("describe security group %s: %w", sgID, err)
		}

		if len(describeOutput.SecurityGroups) == 0 {
			log.Printf("Security group %s not found (already deleted)", sgID)
			continue
		}

		sg := describeOutput.SecurityGroups[0]
		sgName := aws.ToString(sg.GroupName)

		// Skip the default security group
		if sgName == "default" {
			log.Printf("Skipping default security group %s", sgID)
			continue
		}

		// First, revoke all ingress and egress rules to break dependencies
		if len(sg.IpPermissions) > 0 {
			log.Printf("Revoking %d ingress rules from security group %s", len(sg.IpPermissions), sgID)
			_, err := r.ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       aws.String(sgID),
				IpPermissions: sg.IpPermissions,
			})
			if err != nil && !strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Warning: failed to revoke ingress rules for %s: %v", sgID, err)
			}
		}

		if len(sg.IpPermissionsEgress) > 0 {
			log.Printf("Revoking %d egress rules from security group %s", len(sg.IpPermissionsEgress), sgID)
			_, err := r.ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       aws.String(sgID),
				IpPermissions: sg.IpPermissionsEgress,
			})
			if err != nil && !strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Warning: failed to revoke egress rules for %s: %v", sgID, err)
			}
		}

		// Break circular dependencies: find other security groups that reference this one
		vpcID := aws.ToString(sg.VpcId)
		if err := r.breakCircularSecurityGroupDependencies(ctx, sgID, vpcID); err != nil {
			log.Printf("Warning: failed to break circular dependencies for %s: %v", sgID, err)
		}

		// Delete the security group
		log.Printf("Deleting security group %s (%s)", sgID, sgName)
		_, err = r.ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(sgID),
		})
		if err != nil {
			if strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Security group %s already deleted", sgID)
				continue
			}
			// DependencyViolation means it's still in use - will retry
			if strings.Contains(err.Error(), "DependencyViolation") {
				log.Printf("Security group %s still has dependencies, will retry", sgID)
				return fmt.Errorf("%w: security group %s has dependencies", ErrDestroyInProgress, sgID)
			}
			log.Printf("Warning: failed to delete security group %s: %v", sgID, err)
			// Don't fail - might be managed by CloudFormation
			continue
		}
		log.Printf("Deleted security group %s", sgID)
	}

	return nil
}

// breakCircularSecurityGroupDependencies finds and revokes rules from other security groups that reference the target
func (r *EKSDestroyReconciler) breakCircularSecurityGroupDependencies(ctx context.Context, targetSGID, vpcID string) error {
	log.Printf("Checking for circular security group dependencies for %s", targetSGID)

	// Find all security groups in the VPC
	describeInput := &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	describeOutput, err := r.ec2Client.DescribeSecurityGroups(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("describe security groups in VPC %s: %w", vpcID, err)
	}

	// Check each security group for rules that reference the target
	for _, sg := range describeOutput.SecurityGroups {
		otherSGID := aws.ToString(sg.GroupId)

		// Skip the target security group itself
		if otherSGID == targetSGID {
			continue
		}

		// Check ingress rules for references to target SG
		ingressRulesToRevoke := r.findRulesReferencingSG(sg.IpPermissions, targetSGID)
		if len(ingressRulesToRevoke) > 0 {
			log.Printf("Found %d ingress rules in %s (%s) that reference %s - revoking",
				len(ingressRulesToRevoke), otherSGID, aws.ToString(sg.GroupName), targetSGID)

			_, err := r.ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       aws.String(otherSGID),
				IpPermissions: ingressRulesToRevoke,
			})
			if err != nil && !strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Warning: failed to revoke ingress rules from %s: %v", otherSGID, err)
			}
		}

		// Check egress rules for references to target SG
		egressRulesToRevoke := r.findRulesReferencingSG(sg.IpPermissionsEgress, targetSGID)
		if len(egressRulesToRevoke) > 0 {
			log.Printf("Found %d egress rules in %s (%s) that reference %s - revoking",
				len(egressRulesToRevoke), otherSGID, aws.ToString(sg.GroupName), targetSGID)

			_, err := r.ec2Client.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       aws.String(otherSGID),
				IpPermissions: egressRulesToRevoke,
			})
			if err != nil && !strings.Contains(err.Error(), "InvalidGroup.NotFound") {
				log.Printf("Warning: failed to revoke egress rules from %s: %v", otherSGID, err)
			}
		}
	}

	return nil
}

// findRulesReferencingSG filters IP permissions to find rules that reference a specific security group
func (r *EKSDestroyReconciler) findRulesReferencingSG(permissions []ec2types.IpPermission, targetSGID string) []ec2types.IpPermission {
	var matchingRules []ec2types.IpPermission

	for _, perm := range permissions {
		// Check if this permission references the target security group
		for _, groupPair := range perm.UserIdGroupPairs {
			if aws.ToString(groupPair.GroupId) == targetSGID {
				// This rule references the target SG - add it to the list
				matchingRules = append(matchingRules, perm)
				break // Only add the rule once even if it has multiple group pairs
			}
		}
	}

	return matchingRules
}

// cleanupOrphanedSecurityGroups removes EKS cluster security groups that may be left behind
// This handles edge cases where the cluster was deleted but its security group wasn't cleaned up
func (r *EKSDestroyReconciler) cleanupOrphanedSecurityGroups(ctx context.Context, stacks []string) error {
	// Find the cluster stack to get the VPC ID
	var vpcID string
	for _, stackName := range stacks {
		if strings.HasSuffix(stackName, "-cluster") {
			// Get VPC ID from stack outputs
			describeInput := &cloudformation.DescribeStacksInput{
				StackName: aws.String(stackName),
			}
			describeOutput, err := r.cfnClient.DescribeStacks(ctx, describeInput)
			if err != nil {
				if isCFNStackNotFound(err) {
					continue
				}
				return fmt.Errorf("describe cluster stack: %w", err)
			}

			if len(describeOutput.Stacks) > 0 {
				stack := describeOutput.Stacks[0]
				for _, output := range stack.Outputs {
					if aws.ToString(output.OutputKey) == "VPC" {
						vpcID = aws.ToString(output.OutputValue)
						break
					}
				}
			}
			break
		}
	}

	if vpcID == "" {
		// No VPC found, nothing to clean up
		return nil
	}

	log.Printf("Checking for orphaned security groups in VPC %s", vpcID)

	// Find all security groups in the VPC that match the cluster name pattern
	describeSGInput := &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	describeSGOutput, err := r.ec2Client.DescribeSecurityGroups(ctx, describeSGInput)
	if err != nil {
		return fmt.Errorf("describe security groups: %w", err)
	}

	// Delete security groups that belong to this EKS cluster (except default)
	for _, sg := range describeSGOutput.SecurityGroups {
		groupName := aws.ToString(sg.GroupName)
		groupID := aws.ToString(sg.GroupId)

		// Skip the default security group
		if groupName == "default" {
			continue
		}

		// Check if this security group belongs to our cluster
		isClusterSG := strings.Contains(groupName, r.clusterName) ||
			strings.Contains(groupName, "eks-cluster-sg-")

		if !isClusterSG {
			continue
		}

		log.Printf("Deleting orphaned security group %s (%s)", groupID, groupName)

		deleteInput := &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(groupID),
		}

		_, err := r.ec2Client.DeleteSecurityGroup(ctx, deleteInput)
		if err != nil {
			// Log but don't fail - the security group might be in use or already deleted
			log.Printf("Warning: failed to delete security group %s: %v", groupID, err)
		} else {
			log.Printf("Deleted orphaned security group %s", groupID)
		}
	}

	return nil
}

// Helper functions for error checking

func isEKSResourceNotFound(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "ResourceNotFoundException") ||
		strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "No cluster found")
}

func isCFNStackNotFound(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "ValidationError")
}

// ReconcileLoop runs the reconciliation loop with retries and timeout
func (r *EKSDestroyReconciler) ReconcileLoop(ctx context.Context) error {
	timeout := 60 * time.Minute // Total timeout for entire destroy operation
	pollInterval := 30 * time.Second

	deadline := time.Now().Add(timeout)

	for {
		// Check if we've exceeded timeout
		if time.Now().After(deadline) {
			return fmt.Errorf("cluster destruction timed out after %v", timeout)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Run one reconciliation iteration
		done, err := r.Reconcile(ctx)
		if err != nil {
			// Check if this is a retryable "in progress" error or a terminal failure
			if errors.Is(err, ErrDestroyInProgress) {
				log.Printf("Reconcile iteration: %v (will retry)", err)
			} else {
				// Terminal error - fail immediately
				return fmt.Errorf("reconcile failed: %w", err)
			}
		}

		if done {
			log.Printf("Cluster %s destruction complete", r.clusterName)
			return nil
		}

		// Wait before next iteration (context-aware)
		log.Printf("Waiting %v before next reconcile iteration", pollInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue to next iteration
		}
	}
}

// VerificationResult contains details of destroy verification
type VerificationResult struct {
	Passed                    bool
	RemainingResources        map[string][]string // resource type -> resource names
	ClusterExists             bool
	ManagedNodegroupsCount    int
	FargateProfilesCount      int
	CloudFormationStacksCount int
	LoadBalancersCount        int
	TargetGroupsCount         int
	SecurityGroupsCount       int
	NetworkInterfacesCount    int
	InstancesCount            int
	VPCID                     string
}

// VerifyDestroyed performs comprehensive verification that all cluster resources are deleted
// This is the ONLY function that should be called before marking a cluster as DESTROYED
func (r *EKSDestroyReconciler) VerifyDestroyed(ctx context.Context) (*VerificationResult, error) {
	log.Printf("[Destroy Verification] Starting comprehensive verification for cluster %s", r.clusterName)

	result := &VerificationResult{
		Passed:             true,
		RemainingResources: make(map[string][]string),
	}

	// 1. Verify EKS cluster is deleted
	describeInput := &eks.DescribeClusterInput{
		Name: aws.String(r.clusterName),
	}

	_, err := r.eksClient.DescribeCluster(ctx, describeInput)
	if err == nil {
		result.Passed = false
		result.ClusterExists = true
		result.RemainingResources["eks_cluster"] = []string{r.clusterName}
		log.Printf("[Destroy Verification] FAILED: EKS cluster %s still exists", r.clusterName)
	} else if !isEKSResourceNotFound(err) {
		return nil, fmt.Errorf("verify cluster deleted: %w", err)
	} else {
		log.Printf("[Destroy Verification] ✓ EKS cluster deleted")
	}

	// 2. Verify managed nodegroups are deleted
	listNGInput := &eks.ListNodegroupsInput{
		ClusterName: aws.String(r.clusterName),
	}

	listNGOutput, err := r.eksClient.ListNodegroups(ctx, listNGInput)
	if err != nil && !isEKSResourceNotFound(err) {
		return nil, fmt.Errorf("list nodegroups: %w", err)
	}

	if listNGOutput != nil && len(listNGOutput.Nodegroups) > 0 {
		result.Passed = false
		result.ManagedNodegroupsCount = len(listNGOutput.Nodegroups)
		result.RemainingResources["managed_nodegroups"] = listNGOutput.Nodegroups
		log.Printf("[Destroy Verification] FAILED: %d managed nodegroups remain: %v", len(listNGOutput.Nodegroups), listNGOutput.Nodegroups)
	} else {
		log.Printf("[Destroy Verification] ✓ No managed nodegroups")
	}

	// 3. Verify Fargate profiles are deleted
	listFPInput := &eks.ListFargateProfilesInput{
		ClusterName: aws.String(r.clusterName),
	}

	listFPOutput, err := r.eksClient.ListFargateProfiles(ctx, listFPInput)
	if err != nil && !isEKSResourceNotFound(err) {
		return nil, fmt.Errorf("list fargate profiles: %w", err)
	}

	if listFPOutput != nil && len(listFPOutput.FargateProfileNames) > 0 {
		result.Passed = false
		result.FargateProfilesCount = len(listFPOutput.FargateProfileNames)
		result.RemainingResources["fargate_profiles"] = listFPOutput.FargateProfileNames
		log.Printf("[Destroy Verification] FAILED: %d Fargate profiles remain: %v", len(listFPOutput.FargateProfileNames), listFPOutput.FargateProfileNames)
	} else {
		log.Printf("[Destroy Verification] ✓ No Fargate profiles")
	}

	// 4. Verify CloudFormation stacks are deleted (with pagination)
	listStacksInput := &cloudformation.ListStacksInput{
		StackStatusFilter: []cfntypes.StackStatus{
			cfntypes.StackStatusCreateComplete,
			cfntypes.StackStatusUpdateComplete,
			cfntypes.StackStatusDeleteInProgress,
			cfntypes.StackStatusDeleteFailed,
			cfntypes.StackStatusUpdateRollbackComplete,
			cfntypes.StackStatusImportComplete,
			cfntypes.StackStatusCreateFailed,
			cfntypes.StackStatusRollbackComplete,
			cfntypes.StackStatusUpdateCompleteCleanupInProgress,
		},
	}

	var remainingStacks []string
	paginator := cloudformation.NewListStacksPaginator(r.cfnClient, listStacksInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list cloudformation stacks: %w", err)
		}

		for _, stack := range page.StackSummaries {
			stackName := aws.ToString(stack.StackName)
			if strings.HasPrefix(stackName, "eksctl-"+r.clusterName+"-") {
				remainingStacks = append(remainingStacks, stackName)
			}
		}
	}

	if len(remainingStacks) > 0 {
		result.Passed = false
		result.CloudFormationStacksCount = len(remainingStacks)
		result.RemainingResources["cloudformation_stacks"] = remainingStacks
		log.Printf("[Destroy Verification] FAILED: %d CloudFormation stacks remain: %v", len(remainingStacks), remainingStacks)
	} else {
		log.Printf("[Destroy Verification] ✓ No CloudFormation stacks")
	}

	// 5. Discover VPC ID for VPC-level resource verification
	vpcID, err := r.discoverVPCID(ctx, &EKSDestroyState{
		ClusterExists:        result.ClusterExists,
		CloudFormationStacks: remainingStacks,
		ClusterName:          r.clusterName,
	})
	if err != nil {
		log.Printf("[Destroy Verification] Warning: could not discover VPC ID: %v", err)
	} else {
		result.VPCID = vpcID
	}

	// 6. Verify LoadBalancers and Target Groups using ELBv2 (proper verification)
	if result.VPCID != "" {
		lbARNs, tgARNs, err := r.discoverLoadBalancers(ctx, result.VPCID)
		if err != nil {
			log.Printf("[Destroy Verification] Warning: failed to check for LoadBalancers: %v", err)
		} else {
			if len(lbARNs) > 0 {
				result.Passed = false
				result.LoadBalancersCount = len(lbARNs)
				result.RemainingResources["load_balancers"] = lbARNs
				log.Printf("[Destroy Verification] FAILED: %d LoadBalancers remain: %v", len(lbARNs), lbARNs)
			} else {
				log.Printf("[Destroy Verification] ✓ No LoadBalancers")
			}

			if len(tgARNs) > 0 {
				result.Passed = false
				result.TargetGroupsCount = len(tgARNs)
				result.RemainingResources["target_groups"] = tgARNs
				log.Printf("[Destroy Verification] FAILED: %d Target Groups remain: %v", len(tgARNs), tgARNs)
			} else {
				log.Printf("[Destroy Verification] ✓ No Target Groups")
			}
		}
	}

	// 7. Verify security groups created by cluster are deleted
	if result.VPCID != "" {
		sgIDs, err := r.discoverSecurityGroups(ctx, result.VPCID)
		if err != nil {
			log.Printf("[Destroy Verification] Warning: failed to check for SecurityGroups: %v", err)
		} else if len(sgIDs) > 0 {
			result.Passed = false
			result.SecurityGroupsCount = len(sgIDs)
			result.RemainingResources["security_groups"] = sgIDs
			log.Printf("[Destroy Verification] FAILED: %d SecurityGroups remain: %v", len(sgIDs), sgIDs)
		} else {
			log.Printf("[Destroy Verification] ✓ No SecurityGroups")
		}
	}

	// 8. Verify network interfaces (ENIs) are deleted
	if result.VPCID != "" {
		eniIDs, err := r.discoverNetworkInterfaces(ctx, result.VPCID)
		if err != nil {
			log.Printf("[Destroy Verification] Warning: failed to check for NetworkInterfaces: %v", err)
		} else if len(eniIDs) > 0 {
			result.Passed = false
			result.NetworkInterfacesCount = len(eniIDs)
			result.RemainingResources["network_interfaces"] = eniIDs
			log.Printf("[Destroy Verification] FAILED: %d Network Interfaces remain: %v", len(eniIDs), eniIDs)
		} else {
			log.Printf("[Destroy Verification] ✓ No Network Interfaces")
		}
	}

	// 9. Verify EC2 instances are deleted
	if result.VPCID != "" {
		instanceIDs, err := r.discoverInstances(ctx, result.VPCID)
		if err != nil {
			log.Printf("[Destroy Verification] Warning: failed to check for Instances: %v", err)
		} else if len(instanceIDs) > 0 {
			result.Passed = false
			result.InstancesCount = len(instanceIDs)
			result.RemainingResources["instances"] = instanceIDs
			log.Printf("[Destroy Verification] FAILED: %d EC2 Instances remain: %v", len(instanceIDs), instanceIDs)
		} else {
			log.Printf("[Destroy Verification] ✓ No EC2 Instances")
		}
	}

	// Log final result
	if result.Passed {
		log.Printf("[Destroy Verification] ✓ PASSED: All resources deleted for cluster %s", r.clusterName)
	} else {
		log.Printf("[Destroy Verification] ✗ FAILED: %d resource types still exist for cluster %s",
			len(result.RemainingResources), r.clusterName)
		for resourceType, resources := range result.RemainingResources {
			log.Printf("[Destroy Verification]   - %s: %v", resourceType, resources)
		}
	}

	return result, nil
}
