package worker

import (
	"context"
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
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
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
}

// EKSDestroyReconciler implements reconciliation-based EKS cluster deletion
type EKSDestroyReconciler struct {
	store       *store.Store
	eksClient   *eks.Client
	cfnClient   *cloudformation.Client
	ec2Client   *ec2.Client
	clusterName string
	region      string
	clusterID   string
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

	// Phase 5.5: Clean up orphaned security groups before VPC deletion
	// This handles edge cases where cluster was deleted but security groups remain
	if len(state.CloudFormationStacks) > 0 {
		if err := r.cleanupOrphanedSecurityGroups(ctx, state.CloudFormationStacks); err != nil {
			log.Printf("Warning: failed to cleanup orphaned security groups: %v", err)
			// Don't fail - continue with stack deletion
		}
	}

	// Phase 6: Delete supporting CloudFormation stacks (cluster stack, addons, VPC)
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

	// List CloudFormation stacks for this cluster
	listStacksInput := &cloudformation.ListStacksInput{
		StackStatusFilter: []cfntypes.StackStatus{
			cfntypes.StackStatusCreateComplete,
			cfntypes.StackStatusUpdateComplete,
			cfntypes.StackStatusDeleteInProgress,
		},
	}

	listStacksOutput, err := r.cfnClient.ListStacks(ctx, listStacksInput)
	if err != nil {
		return nil, fmt.Errorf("list cloudformation stacks: %w", err)
	}

	// Filter stacks belonging to this cluster (eksctl naming convention)
	for _, stack := range listStacksOutput.StackSummaries {
		stackName := aws.ToString(stack.StackName)
		if strings.HasPrefix(stackName, "eksctl-"+r.clusterName+"-") {
			state.CloudFormationStacks = append(state.CloudFormationStacks, stackName)
		}
	}

	if len(state.CloudFormationStacks) > 0 {
		log.Printf("Found %d CloudFormation stacks: %v", len(state.CloudFormationStacks), state.CloudFormationStacks)
	}

	return state, nil
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
			return fmt.Errorf("nodegroup %s still deleting", ng)
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
		return fmt.Errorf("nodegroup %s deletion in progress", ng)
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
			return fmt.Errorf("fargate profile %s still deleting", fp)
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
		return fmt.Errorf("fargate profile %s deletion in progress", fp)
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
			return fmt.Errorf("stack %s still deleting", stackName)
		}

		// If delete failed, log and skip
		if status == cfntypes.StackStatusDeleteFailed {
			log.Printf("Warning: stack %s delete failed, skipping", stackName)
			continue
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
		return fmt.Errorf("stack %s deletion in progress", stackName)
	}

	return nil
}

// reconcileClusterDelete deletes the EKS cluster control plane
func (r *EKSDestroyReconciler) reconcileClusterDelete(ctx context.Context, state *EKSDestroyState) error {
	log.Printf("Reconciling cluster deletion for %s", r.clusterName)

	// Check if already deleting
	if state.ClusterStatus == string(ekstypes.ClusterStatusDeleting) {
		log.Printf("Cluster %s is already deleting", r.clusterName)
		return fmt.Errorf("cluster %s still deleting", r.clusterName)
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
	return fmt.Errorf("cluster %s deletion in progress", r.clusterName)
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
			return fmt.Errorf("stack %s still deleting", stackName)
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
		return fmt.Errorf("stack %s deletion in progress", stackName)
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
			// Log the error but continue - these are "work in progress" errors
			log.Printf("Reconcile iteration: %v (will retry)", err)
		}

		if done {
			log.Printf("Cluster %s destruction complete", r.clusterName)
			return nil
		}

		// Wait before next iteration
		log.Printf("Waiting %v before next reconcile iteration", pollInterval)
		time.Sleep(pollInterval)
	}
}

// VerificationResult contains details of destroy verification
type VerificationResult struct {
	Passed                  bool
	RemainingResources      map[string][]string // resource type -> resource names
	ClusterExists           bool
	ManagedNodegroupsCount  int
	FargateProfilesCount    int
	CloudFormationStacksCount int
	LoadBalancersCount      int
	SecurityGroupsCount     int
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

	// 4. Verify CloudFormation stacks are deleted
	listStacksInput := &cloudformation.ListStacksInput{
		StackStatusFilter: []cfntypes.StackStatus{
			cfntypes.StackStatusCreateComplete,
			cfntypes.StackStatusUpdateComplete,
			cfntypes.StackStatusDeleteInProgress,
			cfntypes.StackStatusDeleteFailed,
		},
	}

	listStacksOutput, err := r.cfnClient.ListStacks(ctx, listStacksInput)
	if err != nil {
		return nil, fmt.Errorf("list cloudformation stacks: %w", err)
	}

	var remainingStacks []string
	for _, stack := range listStacksOutput.StackSummaries {
		stackName := aws.ToString(stack.StackName)
		if strings.HasPrefix(stackName, "eksctl-"+r.clusterName+"-") {
			remainingStacks = append(remainingStacks, stackName)
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

	// 5. Verify LoadBalancers created by cluster are deleted
	// Look for ELBs/NLBs tagged with cluster name
	describeLBsInput := &ec2.DescribeTagsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("resource-type"),
				Values: []string{"elastic-load-balancer"},
			},
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + r.clusterName),
				Values: []string{"owned", "shared"},
			},
		},
	}

	describeLBsOutput, err := r.ec2Client.DescribeTags(ctx, describeLBsInput)
	if err != nil {
		log.Printf("[Destroy Verification] Warning: failed to check for LoadBalancers: %v", err)
	} else if len(describeLBsOutput.Tags) > 0 {
		result.Passed = false
		result.LoadBalancersCount = len(describeLBsOutput.Tags)
		lbNames := make([]string, 0, len(describeLBsOutput.Tags))
		for _, tag := range describeLBsOutput.Tags {
			if tag.ResourceId != nil {
				lbNames = append(lbNames, *tag.ResourceId)
			}
		}
		result.RemainingResources["load_balancers"] = lbNames
		log.Printf("[Destroy Verification] FAILED: %d LoadBalancers remain: %v", len(lbNames), lbNames)
	} else {
		log.Printf("[Destroy Verification] ✓ No LoadBalancers")
	}

	// 6. Verify security groups created by cluster are deleted
	describeSGsInput := &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:kubernetes.io/cluster/" + r.clusterName),
				Values: []string{"owned"},
			},
		},
	}

	describeSGsOutput, err := r.ec2Client.DescribeSecurityGroups(ctx, describeSGsInput)
	if err != nil {
		log.Printf("[Destroy Verification] Warning: failed to check for SecurityGroups: %v", err)
	} else if len(describeSGsOutput.SecurityGroups) > 0 {
		result.Passed = false
		result.SecurityGroupsCount = len(describeSGsOutput.SecurityGroups)
		sgNames := make([]string, 0, len(describeSGsOutput.SecurityGroups))
		for _, sg := range describeSGsOutput.SecurityGroups {
			if sg.GroupId != nil {
				sgNames = append(sgNames, *sg.GroupId)
			}
		}
		result.RemainingResources["security_groups"] = sgNames
		log.Printf("[Destroy Verification] FAILED: %d SecurityGroups remain: %v", len(sgNames), sgNames)
	} else {
		log.Printf("[Destroy Verification] ✓ No SecurityGroups")
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
