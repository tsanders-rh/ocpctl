package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ResumeHandler handles cluster resume jobs
type ResumeHandler struct {
	config   *Config
	store    *store.Store
	registry *profile.Registry
}

// NewResumeHandler creates a new resume handler
func NewResumeHandler(cfg *Config, st *store.Store) *ResumeHandler {
	// Load profile registry
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "internal/profile/definitions"
	}

	loader := profile.NewLoader(profilesDir)
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatalf("Failed to load profile registry: %v", err)
	}

	return &ResumeHandler{
		config:   cfg,
		store:    st,
		registry: registry,
	}
}

// Handle handles a cluster resume job by starting stopped instances or scaling node groups back up.
// Routes to the appropriate platform-specific resume handler based on cluster type.
// Supports OpenShift (AWS), EKS, and IKS cluster types.
func (h *ResumeHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Resuming cluster %s (platform=%s, cluster_type=%s)", cluster.Name, cluster.Platform, cluster.ClusterType)

	// Route by cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		return h.resumeOpenShift(ctx, cluster, job)
	case types.ClusterTypeROSA:
		return h.resumeROSA(ctx, cluster, job)
	case types.ClusterTypeEKS:
		return h.resumeEKS(ctx, cluster, job)
	case types.ClusterTypeIKS:
		return h.resumeIKS(ctx, cluster, job)
	case types.ClusterTypeGKE:
		return h.resumeGKE(ctx, cluster, job)
	case types.ClusterTypeARO:
		return h.resumeARO(ctx, cluster, job)
	case types.ClusterTypeAKS:
		return h.resumeAKS(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported cluster type for resume: %s", cluster.ClusterType)
	}
}

// resumeOpenShift resumes an OpenShift cluster (AWS, IBMCloud, or GCP)
func (h *ResumeHandler) resumeOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	switch cluster.Platform {
	case types.PlatformAWS:
		return h.resumeAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return fmt.Errorf("resume not supported for platform %s - cluster was destroyed", cluster.Platform)
	case types.PlatformGCP:
		return h.resumeGCPOpenShift(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported platform for OpenShift resume: %s", cluster.Platform)
	}
}

// resumeAWS resumes an AWS cluster by starting all EC2 instances
func (h *ResumeHandler) resumeAWS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming AWS cluster %s by starting EC2 instances", cluster.Name)

	// Ensure artifacts are available locally
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get instance IDs from the last HIBERNATE job metadata
	var instanceIDs []string
	var infraID string

	// Try to get instance IDs from the most recent successful HIBERNATE job
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob != nil && lastHibernateJob.Metadata != nil {
		if ids, ok := lastHibernateJob.Metadata["instance_ids"].([]interface{}); ok {
			for _, id := range ids {
				if strID, ok := id.(string); ok {
					instanceIDs = append(instanceIDs, strID)
				}
			}
		}
		if id, ok := lastHibernateJob.Metadata["infra_id"].(string); ok {
			infraID = id
		}
	}

	// If we don't have instance IDs, try to discover them
	if len(instanceIDs) == 0 {
		log.Printf("No instance IDs found in hibernate job metadata, discovering instances")

		// Get infraID from metadata.json (now available after ensureArtifactsAvailable)
		if infraID == "" {
			infraID, err = h.getInfraID(cluster)
			if err != nil {
				return fmt.Errorf("get infrastructure ID: %w", err)
			}
		}

		log.Printf("Using infrastructure ID: %s", infraID)

		// Load AWS config
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
		if err != nil {
			return fmt.Errorf("load AWS config: %w", err)
		}

		ec2Client := ec2.NewFromConfig(cfg)

		// Find all stopped instances with tag kubernetes.io/cluster/{infraID}=owned
		tagKey := fmt.Sprintf("kubernetes.io/cluster/%s", infraID)
		describeInput := &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{
				{
					Name:   strPtr("tag-key"),
					Values: []string{tagKey},
				},
				{
					Name:   strPtr("instance-state-name"),
					Values: []string{"stopped"},
				},
			},
		}

		result, err := ec2Client.DescribeInstances(ctx, describeInput)
		if err != nil {
			return fmt.Errorf("describe instances: %w", err)
		}

		// Collect instance IDs
		for _, reservation := range result.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId != nil {
					instanceIDs = append(instanceIDs, *instance.InstanceId)
				}
			}
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("Warning: no stopped instances found for cluster %s", cluster.Name)
		// Update cluster status to READY anyway (maybe instances are already running)
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	log.Printf("Found %d stopped instances to start: %v", len(instanceIDs), instanceIDs)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Instances are guaranteed to be fully stopped (hibernate handler waits for this)
	// Start instances directly without additional waiting
	startInput := &ec2.StartInstancesInput{
		InstanceIds: instanceIDs,
	}

	startResult, err := ec2Client.StartInstances(ctx, startInput)
	if err != nil {
		return fmt.Errorf("start instances: %w", err)
	}

	log.Printf("Successfully initiated start for %d instances", len(startResult.StartingInstances))

	// Wait for instances to reach running state (with timeout)
	log.Printf("Waiting for instances to reach running state...")
	runningWaiter := ec2.NewInstanceRunningWaiter(ec2Client)
	waitInput := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}

	waitCtx, cancel := context.WithTimeout(ctx, ClusterStatusCheckTimeout)
	defer cancel()

	if err := runningWaiter.Wait(waitCtx, waitInput, ClusterStatusCheckTimeout); err != nil {
		log.Printf("Warning: instances may not be fully running yet: %v", err)
		// Don't fail the job, just log the warning
	} else {
		log.Printf("All instances are now running")
	}

	// Wait for OpenShift cluster to become healthy
	if err := h.waitForClusterHealth(ctx, cluster); err != nil {
		return fmt.Errorf("wait for cluster health: %w", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("Cluster %s is now READY", cluster.Name)

	return nil
}

// getInfraID extracts the infrastructure ID from metadata.json
// Reuses the same implementation as HibernateHandler
func (h *ResumeHandler) getInfraID(cluster *types.Cluster) (string, error) {
	hibernateHandler := NewHibernateHandler(h.config, h.store)
	return hibernateHandler.getInfraID(cluster)
}

// ensureArtifactsAvailable downloads cluster artifacts from S3 if they don't exist locally
func (h *ResumeHandler) ensureArtifactsAvailable(ctx context.Context, clusterID string) error {
	workDir := filepath.Join(h.config.WorkDir, clusterID)
	metadataPath := filepath.Join(workDir, "metadata.json")

	// Check if metadata.json already exists
	if _, err := os.Stat(metadataPath); err == nil {
		log.Printf("[ResumeHandler] Artifacts already available locally for cluster %s", clusterID)
		return nil
	}

	// Download artifacts from S3
	log.Printf("[ResumeHandler] Downloading artifacts from S3 for cluster %s", clusterID)
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	if err := artifactStorage.DownloadClusterArtifacts(ctx, clusterID, workDir); err != nil {
		return fmt.Errorf("download artifacts: %w", err)
	}

	log.Printf("[ResumeHandler] Successfully downloaded artifacts for cluster %s", clusterID)
	return nil
}

// resumeEKS resumes an EKS cluster by scaling node groups back to original capacity
func (h *ResumeHandler) resumeEKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming EKS cluster %s by scaling node groups to original capacity", cluster.Name)

	// Get the last hibernate job to retrieve original capacities
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found for cluster %s", cluster.ID)
	}

	// Get original node group capacities from job metadata
	capacitiesJSON, ok := lastHibernateJob.Metadata["node_group_capacities"]
	if !ok {
		return fmt.Errorf("node_group_capacities not found in hibernate job metadata")
	}

	capacitiesStr, ok := capacitiesJSON.(string)
	if !ok {
		return fmt.Errorf("node_group_capacities is not a string")
	}

	var nodeGroupCapacities map[string]int
	if err := json.Unmarshal([]byte(capacitiesStr), &nodeGroupCapacities); err != nil {
		return fmt.Errorf("unmarshal node group capacities: %w", err)
	}

	log.Printf("Restoring node group capacities: %+v", nodeGroupCapacities)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	eksClient := eks.NewFromConfig(cfg)

	// Scale each node group back to original capacity
	for ngName, originalCapacity := range nodeGroupCapacities {
		log.Printf("Scaling node group %s to %d", ngName, originalCapacity)

		// Get current node group config to preserve max size
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   &cluster.Name,
			NodegroupName: &ngName,
		}

		describeOutput, err := eksClient.DescribeNodegroup(ctx, describeInput)
		if err != nil {
			log.Printf("Warning: failed to describe node group %s: %v", ngName, err)
			continue
		}

		// Scale back to original capacity
		updateInput := &eks.UpdateNodegroupConfigInput{
			ClusterName:   &cluster.Name,
			NodegroupName: &ngName,
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: int32Ptr(int32(originalCapacity)),
				MinSize:     int32Ptr(0), // Keep min at 0 for future hibernation
				MaxSize:     describeOutput.Nodegroup.ScalingConfig.MaxSize,
			},
		}

		_, err = eksClient.UpdateNodegroupConfig(ctx, updateInput)
		if err != nil {
			log.Printf("Warning: failed to scale node group %s: %v", ngName, err)
			continue
		}

		log.Printf("Successfully scaled node group %s to %d", ngName, originalCapacity)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("EKS cluster %s resumed successfully", cluster.Name)
	return nil
}

// waitForClusterHealth waits for OpenShift cluster to become fully healthy after resume
func (h *ResumeHandler) waitForClusterHealth(ctx context.Context, cluster *types.Cluster) error {
	log.Printf("Waiting for cluster %s to become healthy...", cluster.Name)

	// Ensure we have kubeconfig available
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Check if kubeconfig exists locally
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		log.Printf("Kubeconfig not found locally, downloading from S3...")
		if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
			return fmt.Errorf("ensure artifacts available: %w", err)
		}
	}

	// Wait for API server to be accessible (with retries)
	log.Printf("Waiting for API server to be accessible...")
	if err := h.waitForAPIServer(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("wait for API server: %w", err)
	}

	// Wait for cluster operators to be ready
	log.Printf("Waiting for cluster operators to be ready...")
	if err := h.waitForClusterOperators(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("wait for cluster operators: %w", err)
	}

	// Wait for router pods to be running
	log.Printf("Waiting for router pods to be running...")
	if err := h.waitForRouterPods(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("wait for router pods: %w", err)
	}

	// Wait for CNI pods to be healthy (multus, OVN) on all nodes
	log.Printf("Waiting for CNI networking pods to be healthy...")
	if err := h.waitForCNIPods(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("wait for CNI pods: %w", err)
	}

	// Update load balancer health checks if needed
	log.Printf("Verifying load balancer health check configuration...")
	if err := h.updateLoadBalancerHealthChecks(ctx, cluster, kubeconfigPath); err != nil {
		return fmt.Errorf("update load balancer health checks: %w", err)
	}

	// Wait for load balancer health checks to pass
	log.Printf("Waiting for load balancer health checks to pass...")
	if err := h.waitForLoadBalancerHealth(ctx, cluster); err != nil {
		return fmt.Errorf("wait for load balancer health: %w", err)
	}

	log.Printf("Cluster %s is now healthy and ready", cluster.Name)
	return nil
}

// waitForAPIServer waits for the Kubernetes API server to be accessible
func (h *ResumeHandler) waitForAPIServer(ctx context.Context, kubeconfigPath string) error {
	maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes
	retryDelay := 10 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "cluster-info")
		if err := cmd.Run(); err == nil {
			log.Printf("API server is accessible")
			return nil
		}

		if attempt%6 == 0 { // Log every minute
			log.Printf("API server not yet accessible (attempt %d/%d)...", attempt, maxAttempts)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for API server")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("API server did not become accessible after %d attempts", maxAttempts)
}

// waitForClusterOperators waits for critical cluster operators to be ready
func (h *ResumeHandler) waitForClusterOperators(ctx context.Context, kubeconfigPath string) error {
	maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes
	retryDelay := 10 * time.Second

	// Critical operators that must be ready
	criticalOperators := []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler", "ingress", "network"}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		allReady := true

		for _, op := range criticalOperators {
			cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
				"get", "co", op, "-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
			output, err := cmd.Output()
			if err != nil || string(output) != "True" {
				allReady = false
				break
			}
		}

		if allReady {
			log.Printf("All critical cluster operators are ready")
			return nil
		}

		if attempt%6 == 0 { // Log every minute
			log.Printf("Cluster operators not yet ready (attempt %d/%d)...", attempt, maxAttempts)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for cluster operators")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("cluster operators did not become ready after %d attempts", maxAttempts)
}

// waitForRouterPods waits for router pods to be running and ready
func (h *ResumeHandler) waitForRouterPods(ctx context.Context, kubeconfigPath string) error {
	maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes
	retryDelay := 10 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"get", "pods", "-n", "openshift-ingress", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=default",
			"-o", "jsonpath={.items[*].status.containerStatuses[0].ready}")
		output, err := cmd.Output()
		if err == nil {
			// Check if all pods are ready (all outputs should be "true")
			statuses := strings.Fields(string(output))
			if len(statuses) > 0 {
				allReady := true
				for _, status := range statuses {
					if status != "true" {
						allReady = false
						break
					}
				}
				if allReady {
					log.Printf("All %d router pods are ready", len(statuses))
					return nil
				}
			}
		}

		if attempt%6 == 0 { // Log every minute
			log.Printf("Router pods not yet ready (attempt %d/%d)...", attempt, maxAttempts)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for router pods")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("router pods did not become ready after %d attempts", maxAttempts)
}

// waitForCNIPods waits for CNI networking pods (multus, OVN) to be healthy on all nodes
// This catches post-hibernation networking issues that operator-level checks miss
func (h *ResumeHandler) waitForCNIPods(ctx context.Context, kubeconfigPath string) error {
	maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes
	retryDelay := 10 * time.Second
	stabilityChecks := 3 // Pods must stay healthy for this many consecutive checks
	stableCount := 0      // Track consecutive healthy checks

	// Track if we've already attempted remediation to avoid infinite loops
	remediationAttempted := false

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check multus pods health
		multusHealthy, multusIssues, err := h.checkPodHealth(ctx, kubeconfigPath, "openshift-multus", "app=multus")
		if err != nil {
			log.Printf("Warning: failed to check multus pod health: %v", err)
		}

		// Check OVN-Kubernetes pods health
		ovnHealthy, ovnIssues, err := h.checkPodHealth(ctx, kubeconfigPath, "openshift-ovn-kubernetes", "app=ovnkube-node")
		if err != nil {
			log.Printf("Warning: failed to check OVN pod health: %v", err)
		}

		// If all CNI pods are healthy, increment stability counter
		if multusHealthy && ovnHealthy {
			stableCount++
			if stableCount >= stabilityChecks {
				log.Printf("All CNI networking pods are healthy and stable (%d consecutive checks)", stabilityChecks)
				return nil
			}
			log.Printf("CNI pods healthy, verifying stability (%d/%d checks)...", stableCount, stabilityChecks)
			// Continue to next check without remediation
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while waiting for CNI pods")
			case <-time.After(retryDelay):
				continue
			}
		}

		// Pods are not healthy - reset stability counter
		stableCount = 0

		// If we detect issues and haven't attempted remediation yet, try automatic recovery
		if !remediationAttempted && (len(multusIssues) > 0 || len(ovnIssues) > 0) {
			log.Printf("Detected CNI pod issues, attempting automatic remediation...")
			if err := h.remediateCNIPods(ctx, kubeconfigPath, multusIssues, ovnIssues); err != nil {
				log.Printf("Warning: automatic remediation failed: %v", err)
			} else {
				log.Printf("Automatic remediation completed, waiting for pods to restart...")
				remediationAttempted = true
				// Give pods time to restart
				time.Sleep(30 * time.Second)
				continue
			}
		}

		if attempt%6 == 0 { // Log every minute
			log.Printf("CNI networking pods not yet healthy (attempt %d/%d)...", attempt, maxAttempts)
			if len(multusIssues) > 0 {
				log.Printf("  Multus issues: %v", multusIssues)
			}
			if len(ovnIssues) > 0 {
				log.Printf("  OVN issues: %v", ovnIssues)
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for CNI pods")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("CNI networking pods did not become healthy after %d attempts", maxAttempts)
}

// checkPodHealth checks if pods in a namespace with a given label are healthy
// Returns: (allHealthy bool, issuesList []string, error)
func (h *ResumeHandler) checkPodHealth(ctx context.Context, kubeconfigPath, namespace, labelSelector string) (bool, []string, error) {
	// Get pod status
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "pods", "-n", namespace, "-l", labelSelector,
		"-o", "jsonpath={range .items[*]}{.metadata.name},{.status.phase},{.status.containerStatuses[*].ready}|{end}")

	output, err := cmd.Output()
	if err != nil {
		return false, nil, fmt.Errorf("get pods: %w", err)
	}

	if len(output) == 0 {
		return true, nil, nil // No pods found, consider healthy
	}

	var issues []string
	allHealthy := true

	// Parse output: name,phase,ready|name,phase,ready|...
	pods := strings.Split(strings.TrimSuffix(string(output), "|"), "|")
	for _, podInfo := range pods {
		if podInfo == "" {
			continue
		}

		parts := strings.Split(podInfo, ",")
		if len(parts) < 3 {
			continue
		}

		podName := parts[0]
		phase := parts[1]
		readyStatuses := parts[2]

		// Check if pod is in a bad state
		if phase == "CrashLoopBackOff" || phase == "Error" || phase == "Failed" {
			issues = append(issues, fmt.Sprintf("%s (phase: %s)", podName, phase))
			allHealthy = false
		} else if phase == "Running" {
			// Check if containers are ready
			if !strings.Contains(readyStatuses, "true") || strings.Contains(readyStatuses, "false") {
				issues = append(issues, fmt.Sprintf("%s (containers not ready)", podName))
				allHealthy = false
			}
		} else if phase == "Pending" || phase == "ContainerCreating" {
			// Check if pod has been pending for too long
			// For now, just mark as not healthy but don't add to issues (may be starting up)
			allHealthy = false
		}
	}

	return allHealthy, issues, nil
}

// remediateCNIPods attempts to fix CNI pod issues by deleting problematic pods
func (h *ResumeHandler) remediateCNIPods(ctx context.Context, kubeconfigPath string, multusIssues, ovnIssues []string) error {
	var deletedPods []string

	// Delete problematic multus pods
	for _, issue := range multusIssues {
		podName := strings.Split(issue, " ")[0]
		log.Printf("Deleting problematic multus pod: %s", podName)
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"delete", "pod", "-n", "openshift-multus", podName, "--wait=false")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: failed to delete pod %s: %v", podName, err)
		} else {
			deletedPods = append(deletedPods, podName)
		}
	}

	// Delete problematic OVN pods
	for _, issue := range ovnIssues {
		podName := strings.Split(issue, " ")[0]
		log.Printf("Deleting problematic OVN pod: %s", podName)
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
			"delete", "pod", "-n", "openshift-ovn-kubernetes", podName, "--wait=false")
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: failed to delete pod %s: %v", podName, err)
		} else {
			deletedPods = append(deletedPods, podName)
		}
	}

	if len(deletedPods) > 0 {
		log.Printf("Deleted %d problematic CNI pods for automatic recovery", len(deletedPods))
		return nil
	}

	return fmt.Errorf("no pods were deleted")
}

// updateLoadBalancerHealthChecks updates ELB health check configuration if NodePort changed
func (h *ResumeHandler) updateLoadBalancerHealthChecks(ctx context.Context, cluster *types.Cluster, kubeconfigPath string) error {
	// Get infraID to find load balancer
	infraID, err := h.getInfraID(cluster)
	if err != nil {
		return fmt.Errorf("get infrastructure ID: %w", err)
	}

	// Get router service NodePort
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"get", "svc", "-n", "openshift-ingress", "router-default",
		"-o", "jsonpath={.spec.ports[?(@.name==\"http\")].nodePort}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("get router service NodePort: %w", err)
	}

	nodePort := strings.TrimSpace(string(output))
	if nodePort == "" {
		return fmt.Errorf("could not determine router service NodePort")
	}

	log.Printf("Router service is using NodePort: %s", nodePort)

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	// Find the ingress load balancer (classic ELB)
	elbClient := elasticloadbalancing.NewFromConfig(awsCfg)
	describeInput := &elasticloadbalancing.DescribeLoadBalancersInput{}
	lbResult, err := elbClient.DescribeLoadBalancers(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("describe load balancers: %w", err)
	}

	var ingressLB *elbtypes.LoadBalancerDescription
	for _, lb := range lbResult.LoadBalancerDescriptions {
		if lb.LoadBalancerName != nil && strings.Contains(*lb.LoadBalancerName, infraID) &&
			strings.Contains(*lb.LoadBalancerName, "ext") {
			// Check if this LB has port 80/443 listeners (ingress LB)
			for _, listener := range lb.ListenerDescriptions {
				if listener.Listener != nil &&
					(listener.Listener.LoadBalancerPort == 80 || listener.Listener.LoadBalancerPort == 443) {
					ingressLB = &lb
					break
				}
			}
		}
		if ingressLB != nil {
			break
		}
	}

	if ingressLB == nil {
		log.Printf("No classic ELB ingress load balancer found (cluster may use NLB)")
		return nil
	}

	log.Printf("Found ingress load balancer: %s", *ingressLB.LoadBalancerName)

	// Check current health check configuration
	currentHealthCheck := ingressLB.HealthCheck
	if currentHealthCheck == nil {
		log.Printf("No health check configured on load balancer")
		return nil
	}

	log.Printf("Current health check: %s", *currentHealthCheck.Target)

	// Parse current health check port
	expectedTarget := fmt.Sprintf("HTTP:%s/healthz", nodePort)
	if *currentHealthCheck.Target == expectedTarget {
		log.Printf("Health check already configured correctly")
		return nil
	}

	// Update health check to use current NodePort
	log.Printf("Updating health check from %s to %s", *currentHealthCheck.Target, expectedTarget)
	configureInput := &elasticloadbalancing.ConfigureHealthCheckInput{
		LoadBalancerName: ingressLB.LoadBalancerName,
		HealthCheck: &elbtypes.HealthCheck{
			Target:             &expectedTarget,
			Interval:           currentHealthCheck.Interval,
			Timeout:            currentHealthCheck.Timeout,
			UnhealthyThreshold: currentHealthCheck.UnhealthyThreshold,
			HealthyThreshold:   currentHealthCheck.HealthyThreshold,
		},
	}

	_, err = elbClient.ConfigureHealthCheck(ctx, configureInput)
	if err != nil {
		return fmt.Errorf("configure health check: %w", err)
	}

	log.Printf("Successfully updated load balancer health check configuration")
	return nil
}

// waitForLoadBalancerHealth waits for all instances in the load balancer to be healthy
func (h *ResumeHandler) waitForLoadBalancerHealth(ctx context.Context, cluster *types.Cluster) error {
	// Get infraID to find load balancer
	infraID, err := h.getInfraID(cluster)
	if err != nil {
		return fmt.Errorf("get infrastructure ID: %w", err)
	}

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	// Find the ingress load balancer
	elbClient := elasticloadbalancing.NewFromConfig(awsCfg)
	describeInput := &elasticloadbalancing.DescribeLoadBalancersInput{}
	lbResult, err := elbClient.DescribeLoadBalancers(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("describe load balancers: %w", err)
	}

	var lbName *string
	for _, lb := range lbResult.LoadBalancerDescriptions {
		if lb.LoadBalancerName != nil && strings.Contains(*lb.LoadBalancerName, infraID) &&
			strings.Contains(*lb.LoadBalancerName, "ext") {
			// Check if this LB has port 80/443 listeners
			for _, listener := range lb.ListenerDescriptions {
				if listener.Listener != nil &&
					(listener.Listener.LoadBalancerPort == 80 || listener.Listener.LoadBalancerPort == 443) {
					lbName = lb.LoadBalancerName
					break
				}
			}
		}
		if lbName != nil {
			break
		}
	}

	if lbName == nil {
		log.Printf("No classic ELB ingress load balancer found, skipping health check wait")
		return nil
	}

	log.Printf("Waiting for load balancer %s instances to be healthy...", *lbName)

	maxAttempts := 30 // 30 attempts * 10 seconds = 5 minutes
	retryDelay := 10 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		healthInput := &elasticloadbalancing.DescribeInstanceHealthInput{
			LoadBalancerName: lbName,
		}

		healthResult, err := elbClient.DescribeInstanceHealth(ctx, healthInput)
		if err != nil {
			log.Printf("Warning: failed to check instance health: %v", err)
			time.Sleep(retryDelay)
			continue
		}

		// Count healthy instances
		healthyCount := 0
		totalCount := len(healthResult.InstanceStates)
		for _, state := range healthResult.InstanceStates {
			if state.State != nil && *state.State == "InService" {
				healthyCount++
			}
		}

		log.Printf("Load balancer health: %d/%d instances healthy", healthyCount, totalCount)

		// We need at least one instance healthy (not all, since masters may not have router pods)
		if healthyCount > 0 {
			log.Printf("Load balancer has healthy instances")
			return nil
		}

		if attempt%6 == 0 { // Log every minute
			log.Printf("Load balancer instances not yet healthy (attempt %d/%d)...", attempt, maxAttempts)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for load balancer health")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("load balancer instances did not become healthy after %d attempts", maxAttempts)
}

// resumeIKS resumes an IKS cluster by scaling workers back to original count
func (h *ResumeHandler) resumeIKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming IKS cluster %s by scaling workers to original count", cluster.Name)

	// Get the last hibernate job to retrieve original worker count
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found for cluster %s", cluster.ID)
	}

	// Get original worker pool sizes from job metadata
	poolSizesVal, ok := lastHibernateJob.Metadata["worker_pool_sizes"]
	if !ok {
		// Fallback to legacy metadata format if new format not available
		return fmt.Errorf("worker_pool_sizes not found in hibernate job metadata (cluster may have been hibernated before this feature was implemented)")
	}

	poolSizesStr, ok := poolSizesVal.(string)
	if !ok {
		return fmt.Errorf("worker_pool_sizes is not a string")
	}

	// Parse the worker pool sizes map
	var originalPoolSizes map[string]int
	if err := json.Unmarshal([]byte(poolSizesStr), &originalPoolSizes); err != nil {
		return fmt.Errorf("parse worker pool sizes: %w", err)
	}

	// Calculate total worker count for logging
	totalWorkers := 0
	for _, size := range originalPoolSizes {
		totalWorkers += size
	}

	log.Printf("Restoring IKS cluster to original configuration (%d worker pools, %d total workers)", len(originalPoolSizes), totalWorkers)

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Extract resource group from profile (if specified)
	resourceGroup := ""
	if prof.PlatformConfig.IBMCloud != nil {
		resourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
	}

	// Create IKS installer
	iksInstaller := installer.NewIKSInstaller()

	// Get IBM Cloud API key from environment
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("IBMCLOUD_API_KEY environment variable not set")
	}

	// Login to IBM Cloud (will query for resource groups if not specified)
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region, resourceGroup); err != nil {
		return fmt.Errorf("IBM Cloud login: %w", err)
	}

	// Restore each worker pool to its original size
	log.Printf("Restoring worker pools to original sizes...")
	restoredCount := 0
	for poolName, originalSize := range originalPoolSizes {
		if originalSize == 0 {
			log.Printf("Skipping pool '%s' (original size was 0)", poolName)
			continue
		}

		log.Printf("Restoring worker pool '%s' to %d workers per zone", poolName, originalSize)
		if err := iksInstaller.ResizeWorkerPool(ctx, cluster.Name, poolName, originalSize); err != nil {
			return fmt.Errorf("restore worker pool %s: %w", poolName, err)
		}

		restoredCount++
	}

	log.Printf("Successfully restored %d worker pools to original sizes", restoredCount)

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("IKS cluster %s resumed successfully (%d workers restored)", cluster.Name, totalWorkers)
	return nil
}

// resumeROSA resumes a ROSA cluster by scaling machine pools back to their original sizes
func (h *ResumeHandler) resumeROSA(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming ROSA cluster %s by restoring machine pool sizes", cluster.Name)

	// Update status to RESUMING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusResuming); err != nil {
		return fmt.Errorf("update cluster status to RESUMING: %w", err)
	}

	// Get the hibernate job to retrieve original pool configurations
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}
	if len(hibernateJobs) == 0 {
		return fmt.Errorf("no hibernate job found for cluster")
	}

	// Get the most recent successful hibernate job
	var hibernateJob *types.Job
	for i := len(hibernateJobs) - 1; i >= 0; i-- {
		if hibernateJobs[i].Status == types.JobStatusSucceeded {
			hibernateJob = hibernateJobs[i]
			break
		}
	}
	if hibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found")
	}

	if hibernateJob.Metadata == nil {
		return fmt.Errorf("hibernate job missing metadata - cannot restore pool sizes")
	}

	// Parse original machine pool configurations from hibernate job metadata
	configsJSON, ok := hibernateJob.Metadata["machine_pool_configs"].(string)
	if !ok {
		return fmt.Errorf("hibernate job metadata missing machine_pool_configs")
	}

	type poolConfig struct {
		ID       string `json:"id"`
		Replicas int    `json:"replicas"`
	}

	var originalConfigs []poolConfig
	if err := json.Unmarshal([]byte(configsJSON), &originalConfigs); err != nil {
		return fmt.Errorf("parse pool configurations: %w", err)
	}

	if len(originalConfigs) == 0 {
		log.Printf("Warning: no machine pools to restore for cluster %s", cluster.Name)
		// Update cluster status to READY anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	// Create ROSA installer
	rosaInstaller := installer.NewROSAInstaller()

	// Restore each pool to its original size
	totalRestoredReplicas := 0
	restoredPools := 0

	for _, config := range originalConfigs {
		log.Printf("Restoring machine pool %s to %d replicas", config.ID, config.Replicas)

		if err := rosaInstaller.ScaleMachinePool(ctx, cluster.Name, config.ID, config.Replicas); err != nil {
			return fmt.Errorf("scale machine pool %s to %d: %w", config.ID, config.Replicas, err)
		}

		totalRestoredReplicas += config.Replicas
		restoredPools++

		log.Printf("Successfully restored machine pool %s to %d replicas", config.ID, config.Replicas)
	}

	log.Printf("Restored %d machine pools with %d total replicas", restoredPools, totalRestoredReplicas)

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to READY: %w", err)
	}

	log.Printf("ROSA cluster %s resumed successfully (%d replicas restored across %d machine pools)",
		cluster.Name, totalRestoredReplicas, restoredPools)

	return nil
}
