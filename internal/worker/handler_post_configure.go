package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigureHandler handles post-configuration tasks (e.g., installing dashboards)
type PostConfigureHandler struct {
	config   *Config
	store    *store.Store
	registry *profile.Registry
}

// NewPostConfigureHandler creates a new post-configure handler
func NewPostConfigureHandler(config *Config, st *store.Store, profileRegistry *profile.Registry) *PostConfigureHandler {
	return &PostConfigureHandler{
		config:   config,
		store:    st,
		registry: profileRegistry,
	}
}

// Handle handles post-deployment configuration tasks such as installing operators, dashboards, and manifests.
// Routes to the appropriate platform-specific handler based on cluster type.
// For EKS clusters, installs Kubernetes Dashboard and generates token-based kubeconfig.
// For OpenShift clusters, executes profile-driven configuration (operators, scripts, manifests, helm charts).
func (h *PostConfigureHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Running post-configuration for cluster %s (type=%s)", cluster.Name, cluster.ClusterType)

	// Route to appropriate handler based on cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeEKS:
		return h.handleEKSPostConfigure(ctx, job, cluster)
	case types.ClusterTypeIKS:
		return h.handleIKSPostConfigure(ctx, job, cluster)
	case types.ClusterTypeOpenShift:
		return h.handleOpenShiftPostConfigure(ctx, job, cluster)
	default:
		return fmt.Errorf("unsupported cluster type: %s", cluster.ClusterType)
	}
}

// handleEKSPostConfigure applies post-deployment manifests for EKS clusters
func (h *PostConfigureHandler) handleEKSPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Running post-deployment for EKS cluster %s", cluster.Name)

	// Get profile to read post-deployment configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Check if post-deployment is enabled
	if prof.PostDeployment == nil || !prof.PostDeployment.Enabled {
		log.Printf("Post-deployment not enabled for profile %s", cluster.Profile)
		return nil
	}

	// Create work directory with restrictive permissions (0700)
	// This directory contains sensitive files like kubeconfig with cluster credentials
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "kubeconfig")
	eksInstaller := installer.NewEKSInstaller()
	if err := eksInstaller.GetKubeconfig(ctx, cluster.Name, cluster.Region, kubeconfigPath); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	// Apply all manifests from profile
	hasDashboard := false
	if len(prof.PostDeployment.Manifests) > 0 {
		log.Printf("Applying %d manifests from profile", len(prof.PostDeployment.Manifests))
		for _, manifest := range prof.PostDeployment.Manifests {
			if err := h.applyManifest(ctx, kubeconfigPath, manifest); err != nil {
				return fmt.Errorf("apply manifest %s: %w", manifest.Name, err)
			}
			// Check if this is the kubernetes-dashboard
			if manifest.Name == "kubernetes-dashboard" {
				hasDashboard = true
			}
		}
	}

	// If Kubernetes Dashboard was installed, perform dashboard-specific setup
	if hasDashboard {
		log.Printf("Kubernetes Dashboard detected, configuring access...")

		// Create admin service account
		if err := h.createDashboardServiceAccount(ctx, kubeconfigPath); err != nil {
			return fmt.Errorf("create dashboard service account: %w", err)
		}

		// Get service account token
		token, err := h.getDashboardToken(ctx, kubeconfigPath)
		if err != nil {
			return fmt.Errorf("get dashboard token: %w", err)
		}

		// Expose dashboard via LoadBalancer
		dashboardURL, err := h.exposeDashboardLoadBalancer(ctx, kubeconfigPath)
		if err != nil {
			return fmt.Errorf("expose dashboard: %w", err)
		}

		log.Printf("Kubernetes Dashboard configured successfully at: %s", dashboardURL)
		log.Printf("Dashboard token stored securely in cluster outputs")

		// Update cluster outputs with dashboard URL
		if err := h.updateClusterConsoleURL(ctx, cluster.ID, dashboardURL, token); err != nil {
			return fmt.Errorf("update cluster console URL: %w", err)
		}
	}

	// Generate token-based kubeconfig for kubectl access
	tokenKubeconfigPath := filepath.Join(filepath.Dir(kubeconfigPath), "auth", "kubeconfig")
	if err := h.generateTokenKubeconfig(ctx, kubeconfigPath, tokenKubeconfigPath, cluster); err != nil {
		return fmt.Errorf("generate token kubeconfig: %w", err)
	}

	log.Printf("Post-deployment completed for cluster %s", cluster.Name)
	return nil
}

// applyManifest applies a single manifest from the profile
func (h *PostConfigureHandler) applyManifest(ctx context.Context, kubeconfigPath string, manifest profile.ManifestConfig) error {
	log.Printf("Applying manifest: %s", manifest.Name)

	// Determine source (URL or local file path)
	var manifestSource string
	if manifest.URL != "" {
		manifestSource = manifest.URL
	} else if manifest.Path != "" {
		manifestSource = manifest.Path
	} else {
		return fmt.Errorf("manifest %s has neither URL nor Path specified", manifest.Name)
	}

	// Apply the manifest
	args := []string{"apply", "-f", manifestSource, "--kubeconfig", kubeconfigPath}

	// Add namespace if specified
	if manifest.Namespace != "" {
		args = append(args, "-n", manifest.Namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Manifest %s applied successfully: %s", manifest.Name, string(output))

	// If this is the kubernetes-dashboard, wait for it to be ready
	if manifest.Name == "kubernetes-dashboard" && manifest.Namespace != "" {
		log.Println("Waiting for dashboard deployment to be ready...")
		waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available",
			"--timeout=300s", "deployment/kubernetes-dashboard",
			"-n", manifest.Namespace, "--kubeconfig", kubeconfigPath)
		if output, err := waitCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to wait for dashboard: %v\nOutput: %s", err, string(output))
			// Don't fail - the deployment might still be rolling out
		} else {
			log.Println("Dashboard deployment is ready")
		}
	}

	return nil
}

// installKubernetesDashboard installs the Kubernetes Dashboard
// DEPRECATED: This function is no longer used. Manifests are now applied via applyManifest()
func (h *PostConfigureHandler) installKubernetesDashboard(ctx context.Context, kubeconfigPath string) error {
	log.Println("Installing Kubernetes Dashboard...")

	// Apply recommended dashboard manifest
	// Using v2.7.0 as it's a stable version with good LoadBalancer support
	dashboardURL := "https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml"

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", dashboardURL, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply dashboard: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Dashboard installed: %s", string(output))

	// Wait for dashboard to be ready
	log.Println("Waiting for dashboard deployment to be ready...")
	waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available",
		"--timeout=300s", "deployment/kubernetes-dashboard",
		"-n", "kubernetes-dashboard", "--kubeconfig", kubeconfigPath)
	if output, err := waitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wait for dashboard: %w\nOutput: %s", err, string(output))
	}

	log.Println("Dashboard deployment is ready")
	return nil
}

// createDashboardServiceAccount creates an admin service account for dashboard access
func (h *PostConfigureHandler) createDashboardServiceAccount(ctx context.Context, kubeconfigPath string) error {
	log.Println("Creating dashboard admin service account...")

	// Create service account manifest
	manifest := `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: admin-user
  namespace: kubernetes-dashboard
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: admin-user
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: admin-user
  namespace: kubernetes-dashboard
---
apiVersion: v1
kind: Secret
metadata:
  name: admin-user-token
  namespace: kubernetes-dashboard
  annotations:
    kubernetes.io/service-account.name: admin-user
type: kubernetes.io/service-account-token
`

	// Write manifest to file
	manifestPath := filepath.Join(filepath.Dir(kubeconfigPath), "dashboard-admin.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0600); err != nil {
		return fmt.Errorf("write service account manifest: %w", err)
	}

	// Apply manifest
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestPath, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply service account: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Service account created: %s", string(output))

	// Wait a moment for the secret to be created
	time.Sleep(APIStabilizationDelay)

	return nil
}

// getDashboardToken retrieves the admin service account token
func (h *PostConfigureHandler) getDashboardToken(ctx context.Context, kubeconfigPath string) (string, error) {
	log.Println("Retrieving dashboard token...")

	cmd := exec.CommandContext(ctx, "kubectl", "get", "secret", "admin-user-token",
		"-n", "kubernetes-dashboard", "-o", "jsonpath={.data.token}", "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get token: %w\nOutput: %s", err, string(output))
	}

	// Token is base64 encoded, decode it
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("empty token received")
	}

	// Decode base64
	decodeCmd := exec.Command("base64", "-d")
	decodeCmd.Stdin = strings.NewReader(token)
	decoded, err := decodeCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	return strings.TrimSpace(string(decoded)), nil
}

// exposeDashboardLoadBalancer exposes the dashboard via LoadBalancer and returns the URL
func (h *PostConfigureHandler) exposeDashboardLoadBalancer(ctx context.Context, kubeconfigPath string) (string, error) {
	log.Println("Exposing dashboard via LoadBalancer...")

	// Patch the kubernetes-dashboard service to be LoadBalancer type
	patchCmd := exec.CommandContext(ctx, "kubectl", "patch", "service", "kubernetes-dashboard",
		"-n", "kubernetes-dashboard", "-p", `{"spec":{"type":"LoadBalancer"}}`, "--kubeconfig", kubeconfigPath)
	if output, err := patchCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("patch service: %w\nOutput: %s", err, string(output))
	}

	log.Println("Service patched to LoadBalancer type")

	// Wait for external IP (up to 5 minutes)
	log.Println("Waiting for LoadBalancer external IP (this may take a few minutes)...")
	var externalIP string
	for i := 0; i < 60; i++ {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "service", "kubernetes-dashboard",
			"-n", "kubernetes-dashboard", "-o", "jsonpath={.status.loadBalancer.ingress[0].hostname}",
			"--kubeconfig", kubeconfigPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Attempt %d/60: Error getting LoadBalancer IP: %v", i+1, err)
		} else {
			externalIP = strings.TrimSpace(string(output))
			if externalIP != "" {
				break
			}
		}
		time.Sleep(NodeReadyCheckInterval)
	}

	if externalIP == "" {
		return "", fmt.Errorf("timeout waiting for LoadBalancer external IP")
	}

	// Dashboard runs on HTTPS port 443
	dashboardURL := fmt.Sprintf("https://%s", externalIP)
	log.Printf("Dashboard is available at: %s", dashboardURL)

	return dashboardURL, nil
}

// exposeDashboardNodePort exposes the dashboard via NodePort and returns the URL
// This is used for IKS clusters which don't support LoadBalancer without portable subnets
func (h *PostConfigureHandler) exposeDashboardNodePort(ctx context.Context, kubeconfigPath string, cluster *types.Cluster) (string, error) {
	log.Println("Exposing dashboard via NodePort (IKS doesn't support LoadBalancer without portable subnets)...")

	// Patch the kubernetes-dashboard service to be NodePort type
	patchCmd := exec.CommandContext(ctx, "kubectl", "patch", "service", "kubernetes-dashboard",
		"-n", "kubernetes-dashboard", "-p", `{"spec":{"type":"NodePort"}}`, "--kubeconfig", kubeconfigPath)
	if output, err := patchCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("patch service: %w\nOutput: %s", err, string(output))
	}

	log.Println("Service patched to NodePort type")

	// Get the NodePort value
	cmd := exec.CommandContext(ctx, "kubectl", "get", "service", "kubernetes-dashboard",
		"-n", "kubernetes-dashboard", "-o", "jsonpath={.spec.ports[0].nodePort}",
		"--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get NodePort: %w\nOutput: %s", err, string(output))
	}

	nodePort := strings.TrimSpace(string(output))
	if nodePort == "" {
		return "", fmt.Errorf("empty NodePort received")
	}

	log.Printf("Dashboard exposed on NodePort: %s", nodePort)

	// Get a worker node's public IP using ibmcloud CLI
	log.Println("Getting worker node public IP...")
	workersCmd := exec.CommandContext(ctx, "ibmcloud", "ks", "workers", "--cluster", cluster.Name, "--output", "json")
	workersOutput, err := workersCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get workers: %w\nOutput: %s", err, string(workersOutput))
	}

	// Parse JSON to extract public IP
	var workers []struct {
		PublicIP string `json:"publicIP"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(workersOutput, &workers); err != nil {
		return "", fmt.Errorf("parse workers JSON: %w", err)
	}

	// Find first worker with a public IP in normal state
	var publicIP string
	for _, worker := range workers {
		if worker.State == "normal" && worker.PublicIP != "" && worker.PublicIP != "-" {
			publicIP = worker.PublicIP
			break
		}
	}

	if publicIP == "" {
		return "", fmt.Errorf("no worker nodes with public IP found")
	}

	// Dashboard runs on HTTPS at the NodePort
	dashboardURL := fmt.Sprintf("https://%s:%s", publicIP, nodePort)
	log.Printf("Dashboard is available at: %s", dashboardURL)

	return dashboardURL, nil
}

// updateClusterConsoleURL updates the cluster's console URL in the database
func (h *PostConfigureHandler) updateClusterConsoleURL(ctx context.Context, clusterID, consoleURL, token string) error {
	log.Printf("Updating cluster console URL to: %s", consoleURL)

	// Get existing outputs or create new ones
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, clusterID)
	if err != nil {
		// If outputs don't exist, we need to create them first
		// Let's check if the error is "not found"
		outputs = &types.ClusterOutputs{
			ClusterID: clusterID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// Update console URL and dashboard token
	outputs.ConsoleURL = &consoleURL
	outputs.DashboardToken = &token
	outputs.UpdatedAt = time.Now()

	log.Printf("Dashboard token securely stored in database for cluster access")

	// Upsert cluster outputs
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("upsert cluster outputs: %w", err)
	}

	log.Printf("Successfully updated cluster console URL")
	return nil
}

// generateTokenKubeconfig generates a token-based kubeconfig that doesn't require AWS credentials.
// This is broken down into smaller helper functions for better maintainability.
func (h *PostConfigureHandler) generateTokenKubeconfig(ctx context.Context, awsKubeconfigPath, outputPath string, cluster *types.Cluster) error {
	log.Println("Generating token-based kubeconfig for kubectl access...")

	// Create and apply service account manifest
	if err := h.createKubectlServiceAccount(ctx, awsKubeconfigPath); err != nil {
		return err
	}

	// Wait for Kubernetes API to create the secret
	time.Sleep(APIStabilizationDelay)

	// Extract token and CA cert from the service account secret
	token, caCert, err := h.extractServiceAccountCredentials(ctx, awsKubeconfigPath)
	if err != nil {
		return err
	}

	// Build and write the kubeconfig file
	return h.writeTokenBasedKubeconfig(cluster, token, caCert, outputPath)
}

// createKubectlServiceAccount creates and applies a service account manifest for kubectl access
func (h *PostConfigureHandler) createKubectlServiceAccount(ctx context.Context, kubeconfigPath string) error {
	manifest := `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ocpctl-kubectl
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ocpctl-kubectl
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: ocpctl-kubectl
  namespace: kube-system
---
apiVersion: v1
kind: Secret
metadata:
  name: ocpctl-kubectl-token
  namespace: kube-system
  annotations:
    kubernetes.io/service-account.name: ocpctl-kubectl
type: kubernetes.io/service-account-token
`

	// Write manifest to temporary file
	manifestPath := filepath.Join(filepath.Dir(kubeconfigPath), "kubectl-sa.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0600); err != nil {
		return fmt.Errorf("write kubectl service account manifest: %w", err)
	}

	// Apply manifest
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestPath, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply service account: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Service account created: %s", string(output))
	return nil
}

// extractServiceAccountCredentials retrieves the token and CA certificate from the service account secret
func (h *PostConfigureHandler) extractServiceAccountCredentials(ctx context.Context, kubeconfigPath string) (token, caCert string, err error) {
	// Get service account token (base64 encoded)
	cmd := exec.CommandContext(ctx, "kubectl", "get", "secret", "ocpctl-kubectl-token",
		"-n", "kube-system", "-o", "jsonpath={.data.token}", "--kubeconfig", kubeconfigPath)
	tokenBytes, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("get token: %w\nOutput: %s", err, string(tokenBytes))
	}

	// Decode token
	token, err = decodeBase64String(strings.TrimSpace(string(tokenBytes)))
	if err != nil {
		return "", "", fmt.Errorf("decode token: %w", err)
	}

	// Get CA certificate (base64 encoded)
	cmd = exec.CommandContext(ctx, "kubectl", "get", "secret", "ocpctl-kubectl-token",
		"-n", "kube-system", "-o", "jsonpath={.data.ca\\.crt}", "--kubeconfig", kubeconfigPath)
	caBytes, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("get CA cert: %w\nOutput: %s", err, string(caBytes))
	}

	// Decode CA certificate
	caCert, err = decodeBase64String(strings.TrimSpace(string(caBytes)))
	if err != nil {
		return "", "", fmt.Errorf("decode CA cert: %w", err)
	}

	return token, caCert, nil
}

// writeTokenBasedKubeconfig generates and writes a token-based kubeconfig file
func (h *PostConfigureHandler) writeTokenBasedKubeconfig(cluster *types.Cluster, token, caCert, outputPath string) error {
	apiURL := fmt.Sprintf("https://%s.%s.eks.amazonaws.com", cluster.Name, cluster.Region)

	// Generate kubeconfig YAML
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: %s
  name: %s
contexts:
- context:
    cluster: %s
    user: ocpctl-kubectl
  name: %s
current-context: %s
users:
- name: ocpctl-kubectl
  user:
    token: %s
`, base64Encode(caCert), apiURL, cluster.Name, cluster.Name, cluster.Name, cluster.Name, token)

	// Ensure output directory exists with restrictive permissions
	if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write kubeconfig with restrictive permissions (owner read/write only)
	if err := os.WriteFile(outputPath, []byte(kubeconfig), 0600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	log.Printf("Token-based kubeconfig generated at: %s", outputPath)
	return nil
}

// decodeBase64String decodes a base64-encoded string using the base64 command
func decodeBase64String(encoded string) (string, error) {
	cmd := exec.Command("base64", "-d")
	cmd.Stdin = strings.NewReader(encoded)
	decoded, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(decoded)), nil
}

// base64Encode encodes a string to base64
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// handleIKSPostConfigure handles post-configuration for IKS clusters
func (h *PostConfigureHandler) handleIKSPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Running post-deployment for IKS cluster %s", cluster.Name)

	// Get profile to read post-deployment configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Check if post-deployment is enabled
	if prof.PostDeployment == nil || !prof.PostDeployment.Enabled {
		log.Printf("Post-deployment not enabled for profile %s, skipping", cluster.Profile)
		return nil
	}

	// Create work directory with restrictive permissions (0700)
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "kubeconfig")
	iksInstaller := installer.NewIKSInstaller()
	if err := iksInstaller.GetKubeconfig(ctx, cluster.Name, kubeconfigPath); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	// Apply all manifests from profile
	hasDashboard := false
	if len(prof.PostDeployment.Manifests) > 0 {
		log.Printf("Applying %d manifests from profile", len(prof.PostDeployment.Manifests))
		for _, manifest := range prof.PostDeployment.Manifests {
			if err := h.applyManifest(ctx, kubeconfigPath, manifest); err != nil {
				return fmt.Errorf("apply manifest %s: %w", manifest.Name, err)
			}
			// Check if this is the kubernetes-dashboard
			if manifest.Name == "kubernetes-dashboard" {
				hasDashboard = true
			}
		}
	}

	// If Kubernetes Dashboard was installed, perform dashboard-specific setup
	if hasDashboard {
		log.Printf("Kubernetes Dashboard detected, configuring access...")

		// Create admin service account
		if err := h.createDashboardServiceAccount(ctx, kubeconfigPath); err != nil {
			return fmt.Errorf("create dashboard service account: %w", err)
		}

		// Get service account token
		token, err := h.getDashboardToken(ctx, kubeconfigPath)
		if err != nil {
			return fmt.Errorf("get dashboard token: %w", err)
		}

		// Expose dashboard via NodePort (IBM Cloud IKS doesn't support LoadBalancer without portable subnets)
		dashboardURL, err := h.exposeDashboardNodePort(ctx, kubeconfigPath, cluster)
		if err != nil {
			return fmt.Errorf("expose dashboard: %w", err)
		}

		log.Printf("Kubernetes Dashboard configured successfully at: %s", dashboardURL)
		log.Printf("Dashboard token stored securely in cluster outputs")

		// Update cluster outputs with dashboard URL
		if err := h.updateClusterConsoleURL(ctx, cluster.ID, dashboardURL, token); err != nil {
			return fmt.Errorf("update cluster console URL: %w", err)
		}
	}

	log.Printf("Post-deployment completed for cluster %s", cluster.Name)
	return nil
}

// handleOpenShiftPostConfigure handles profile-driven post-deployment for OpenShift clusters
func (h *PostConfigureHandler) handleOpenShiftPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Running post-deployment for OpenShift cluster %s", cluster.Name)

	// Get profile
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Check if post-deployment is enabled
	if prof.PostDeployment == nil || !prof.PostDeployment.Enabled {
		log.Printf("Post-deployment not enabled for profile %s, skipping (OpenShift has built-in console)", cluster.Profile)
		return nil
	}

	// Update cluster post_deploy_status to 'in_progress'
	if err := h.updatePostDeployStatus(ctx, cluster.ID, "in_progress"); err != nil {
		return fmt.Errorf("update post-deploy status: %w", err)
	}

	// Ensure artifacts are available locally
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get kubeconfig path
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Verify kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return fmt.Errorf("kubeconfig not found at %s", kubeconfigPath)
	}

	// Install operators
	for _, op := range prof.PostDeployment.Operators {
		if err := h.installOperator(ctx, cluster, kubeconfigPath, op); err != nil {
			// Mark as failed
			_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
			return fmt.Errorf("install operator %s: %w", op.Name, err)
		}
	}

	// Execute scripts
	for _, script := range prof.PostDeployment.Scripts {
		if err := h.executeScript(ctx, cluster, kubeconfigPath, script); err != nil {
			_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
			return fmt.Errorf("execute script %s: %w", script.Name, err)
		}
	}

	// Apply manifests
	for _, manifest := range prof.PostDeployment.Manifests {
		if err := h.applyOpenShiftManifest(ctx, cluster, kubeconfigPath, manifest); err != nil {
			_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
			return fmt.Errorf("apply manifest %s: %w", manifest.Name, err)
		}
	}

	// Install Helm charts
	for _, chart := range prof.PostDeployment.HelmCharts {
		if err := h.installHelmChart(ctx, cluster, kubeconfigPath, chart); err != nil {
			_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
			return fmt.Errorf("install helm chart %s: %w", chart.Name, err)
		}
	}

	// Mark as completed
	if err := h.updatePostDeployStatus(ctx, cluster.ID, "completed"); err != nil {
		return fmt.Errorf("update post-deploy status: %w", err)
	}

	log.Printf("Successfully completed post-deployment configuration for cluster %s", cluster.Name)
	return nil
}

// ensureArtifactsAvailable downloads cluster artifacts from S3 if they don't exist locally
func (h *PostConfigureHandler) ensureArtifactsAvailable(ctx context.Context, clusterID string) error {
	workDir := filepath.Join(h.config.WorkDir, clusterID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Check if kubeconfig already exists
	if _, err := os.Stat(kubeconfigPath); err == nil {
		log.Printf("[PostConfigureHandler] Artifacts already available locally for cluster %s", clusterID)
		return nil
	}

	// Download artifacts from S3
	log.Printf("[PostConfigureHandler] Downloading artifacts from S3 for cluster %s", clusterID)
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	if err := artifactStorage.DownloadClusterArtifacts(ctx, clusterID, workDir); err != nil {
		return fmt.Errorf("download artifacts: %w", err)
	}

	log.Printf("[PostConfigureHandler] Successfully downloaded artifacts for cluster %s", clusterID)
	return nil
}

// installOperator installs an OpenShift operator via Subscription
func (h *PostConfigureHandler) installOperator(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, op profile.OperatorConfig) error {
	log.Printf("Installing operator: %s in namespace %s", op.Name, op.Namespace)

	// Track configuration task
	configID, err := h.createConfigTask(ctx, cluster.ID, types.ConfigTypeOperator, op.Name)
	if err != nil {
		return fmt.Errorf("create config task: %w", err)
	}

	// Update status to installing
	if err := h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusInstalling, nil); err != nil {
		return fmt.Errorf("update config status: %w", err)
	}

	// Create namespace if it doesn't exist
	if err := h.ensureNamespace(ctx, kubeconfigPath, op.Namespace); err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("ensure namespace: %w", err)
	}

	// Create OperatorGroup (required for OLM)
	if err := h.createOperatorGroup(ctx, kubeconfigPath, op.Namespace); err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("create operator group: %w", err)
	}

	// Create Subscription
	if err := h.createSubscription(ctx, kubeconfigPath, op); err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("create subscription: %w", err)
	}

	// Wait for operator to be ready
	if err := h.waitForOperatorReady(ctx, kubeconfigPath, op); err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("wait for operator: %w", err)
	}

	// Create custom resource if specified
	if op.CustomResource != nil {
		if err := h.createCustomResource(ctx, kubeconfigPath, *op.CustomResource); err != nil {
			errMsg := err.Error()
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
			return fmt.Errorf("create custom resource: %w", err)
		}
	}

	// Mark as completed
	if err := h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil); err != nil {
		return fmt.Errorf("update config status: %w", err)
	}

	log.Printf("Successfully installed operator %s", op.Name)
	return nil
}

// ensureNamespace creates a namespace if it doesn't exist
func (h *PostConfigureHandler) ensureNamespace(ctx context.Context, kubeconfigPath, namespace string) error {
	yamlContent := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)

	return h.applyYAML(ctx, kubeconfigPath, yamlContent)
}

// createOperatorGroup creates an OperatorGroup for the namespace
func (h *PostConfigureHandler) createOperatorGroup(ctx context.Context, kubeconfigPath, namespace string) error {
	yamlContent := fmt.Sprintf(`apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: %s-operator-group
  namespace: %s
spec:
  targetNamespaces:
  - %s
`, namespace, namespace, namespace)

	return h.applyYAML(ctx, kubeconfigPath, yamlContent)
}

// createSubscription creates an OLM Subscription for the operator
func (h *PostConfigureHandler) createSubscription(ctx context.Context, kubeconfigPath string, op profile.OperatorConfig) error {
	yamlContent := fmt.Sprintf(`apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: %s
  namespace: %s
spec:
  channel: %s
  name: %s
  source: %s
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
`, op.Name, op.Namespace, op.Channel, op.Name, op.Source)

	return h.applyYAML(ctx, kubeconfigPath, yamlContent)
}

// waitForOperatorReady waits for the operator CSV to reach Succeeded phase
func (h *PostConfigureHandler) waitForOperatorReady(ctx context.Context, kubeconfigPath string, op profile.OperatorConfig) error {
	log.Printf("Waiting for operator %s to be ready (timeout: 10 minutes)...", op.Name)

	timeout := time.After(PostConfigWaitTimeout)
	ticker := time.NewTicker(PostConfigPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for operator %s to be ready", op.Name)
		case <-ticker.C:
			// Check if CSV is ready
			cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
				"get", "csv", "-n", op.Namespace, "-o", "jsonpath={.items[?(@.spec.displayName contains '"+op.Name+"')].status.phase}")
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Checking operator status: %v (will retry)", err)
				continue
			}

			if strings.Contains(string(output), "Succeeded") {
				log.Printf("Operator %s is ready", op.Name)
				return nil
			}

			log.Printf("Operator %s status: %s (waiting...)", op.Name, strings.TrimSpace(string(output)))
		}
	}
}

// createCustomResource creates a custom resource after operator installation
func (h *PostConfigureHandler) createCustomResource(ctx context.Context, kubeconfigPath string, cr profile.CustomResourceConfig) error {
	log.Printf("Creating custom resource: %s/%s", cr.Kind, cr.Name)

	// Build YAML for custom resource
	// Note: This is a simplified version. In production, you'd want to marshal the spec properly
	namespace := cr.Namespace
	if namespace == "" {
		namespace = "default"
	}

	yamlContent := fmt.Sprintf(`apiVersion: %s
kind: %s
metadata:
  name: %s
  namespace: %s
spec: {}
`, cr.APIVersion, cr.Kind, cr.Name, namespace)

	return h.applyYAML(ctx, kubeconfigPath, yamlContent)
}

// executeScript executes a post-deployment script
func (h *PostConfigureHandler) executeScript(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, script profile.ScriptConfig) error {
	log.Printf("Executing script: %s", script.Name)

	// Track configuration task
	configID, err := h.createConfigTask(ctx, cluster.ID, types.ConfigTypeScript, script.Name)
	if err != nil {
		return fmt.Errorf("create config task: %w", err)
	}

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusInstalling, nil)

	// Build script path
	scriptPath := filepath.Join("manifests", script.Path)

	// Verify script exists and is executable
	info, err := os.Stat(scriptPath)
	if err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("script not found: %w", err)
	}

	// Check if executable
	if info.Mode()&0111 == 0 {
		errMsg := "script is not executable"
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Get cluster infrastructure details
	infraID, region, err := h.getClusterInfraDetails(ctx, cluster, kubeconfigPath)
	if err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("get cluster details: %w", err)
	}

	// Prepare environment variables
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("CLUSTER_ID=%s", cluster.ID),
		fmt.Sprintf("CLUSTER_NAME=%s", cluster.Name),
		fmt.Sprintf("INFRA_ID=%s", infraID),
		fmt.Sprintf("REGION=%s", region),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath),
	)

	// Add custom environment variables from script config (with validation)
	for key, value := range script.Env {
		// Validate environment variable name format (alphanumeric + underscore, must start with letter or underscore)
		if !isValidEnvVarName(key) {
			log.Printf("Warning: skipping invalid environment variable name: %s", key)
			continue
		}

		// Block dangerous environment variables that could lead to privilege escalation
		if isDangerousEnvVar(key) {
			log.Printf("Warning: blocked dangerous environment variable: %s", key)
			continue
		}

		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Execute script
	cmd := exec.CommandContext(ctx, scriptPath)
	cmd.Env = env
	cmd.Dir = filepath.Dir(scriptPath) // Set working directory to script's directory

	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := fmt.Sprintf("script failed: %v\nOutput: %s", err, string(output))
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	log.Printf("Script %s completed successfully:\n%s", script.Name, string(output))
	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
	return nil
}

// getClusterInfraDetails retrieves infrastructure ID and region from the cluster
func (h *PostConfigureHandler) getClusterInfraDetails(ctx context.Context, cluster *types.Cluster, kubeconfigPath string) (string, string, error) {
	// Get infraID from cluster using oc command
	cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
		"get", "infrastructure", "cluster",
		"-o", "jsonpath={.status.infrastructureName}")

	infraIDBytes, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("get infrastructure name: %w", err)
	}

	infraID := strings.TrimSpace(string(infraIDBytes))
	if infraID == "" {
		return "", "", fmt.Errorf("infrastructure name is empty")
	}

	// Get region
	region := cluster.Region
	if region == "" {
		region = "us-east-1" // Default fallback
	}

	return infraID, region, nil
}

// applyOpenShiftManifest applies a manifest file for OpenShift clusters
func (h *PostConfigureHandler) applyOpenShiftManifest(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, manifest profile.ManifestConfig) error {
	log.Printf("Applying manifest: %s", manifest.Name)

	// Track configuration task
	configID, err := h.createConfigTask(ctx, cluster.ID, types.ConfigTypeManifest, manifest.Name)
	if err != nil {
		return fmt.Errorf("create config task: %w", err)
	}

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusInstalling, nil)

	// Read manifest file
	manifestPath := filepath.Join("manifests", manifest.Path)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("read manifest: %w", err)
	}

	// Apply manifest
	if err := h.applyYAML(ctx, kubeconfigPath, string(content)); err != nil {
		errMsg := err.Error()
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("apply manifest: %w", err)
	}

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
	return nil
}

// installHelmChart installs a Helm chart
func (h *PostConfigureHandler) installHelmChart(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, chart profile.HelmChartConfig) error {
	log.Printf("Installing Helm chart: %s from repo %s", chart.Name, chart.Repo)

	// Track configuration task
	configID, err := h.createConfigTask(ctx, cluster.ID, types.ConfigTypeHelm, chart.Name)
	if err != nil {
		return fmt.Errorf("create config task: %w", err)
	}

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusInstalling, nil)

	// 1. Add Helm repository
	repoName := fmt.Sprintf("%s-repo", chart.Name)
	log.Printf("Adding Helm repository: %s", repoName)

	addRepoCmd := exec.CommandContext(ctx, "helm", "repo", "add", repoName, chart.Repo)
	addRepoCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

	output, err := addRepoCmd.CombinedOutput()
	if err != nil {
		// Check if repo already exists (not a fatal error)
		if !strings.Contains(string(output), "already exists") {
			errorMsg := fmt.Sprintf("helm repo add failed: %v\nOutput: %s", err, string(output))
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errorMsg)
			return fmt.Errorf("%s", errorMsg)
		}
		log.Printf("Helm repository %s already exists, continuing...", repoName)
	}

	// 2. Update Helm repositories
	log.Printf("Updating Helm repositories...")
	updateCmd := exec.CommandContext(ctx, "helm", "repo", "update")
	updateCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

	if output, err := updateCmd.CombinedOutput(); err != nil {
		log.Printf("WARNING: helm repo update failed (non-fatal): %v\nOutput: %s", err, string(output))
	}

	// 3. Install Helm chart
	chartRef := fmt.Sprintf("%s/%s", repoName, chart.Chart)
	log.Printf("Installing Helm chart: %s", chartRef)

	installArgs := []string{"install", chart.Name, chartRef}

	// Add custom values if provided
	if len(chart.Values) > 0 {
		// Marshal values to YAML and pass via --set-json or create temp values file
		valuesJSON, err := json.Marshal(chart.Values)
		if err != nil {
			errorMsg := fmt.Sprintf("marshal helm values: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errorMsg)
			return fmt.Errorf("marshal helm values: %w", err)
		}

		// Create temporary values file
		valuesFile, err := os.CreateTemp("", fmt.Sprintf("helm-values-%s-*.json", chart.Name))
		if err != nil {
			errorMsg := fmt.Sprintf("create temp values file: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errorMsg)
			return fmt.Errorf("create temp values file: %w", err)
		}
		defer os.Remove(valuesFile.Name())
		defer valuesFile.Close()

		if _, err := valuesFile.Write(valuesJSON); err != nil {
			errorMsg := fmt.Sprintf("write values file: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errorMsg)
			return fmt.Errorf("write values file: %w", err)
		}
		valuesFile.Close()

		installArgs = append(installArgs, "-f", valuesFile.Name())
	}

	// Execute helm install
	installCmd := exec.CommandContext(ctx, "helm", installArgs...)
	installCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

	output, err = installCmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("helm install failed: %v\nOutput: %s", err, string(output))
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errorMsg)
		return fmt.Errorf("%s", errorMsg)
	}

	log.Printf("Helm chart %s installed successfully", chart.Name)
	log.Printf("Output: %s", strings.TrimSpace(string(output)))

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
	return nil
}

// applyYAML applies YAML content to the cluster using oc
func (h *PostConfigureHandler) applyYAML(ctx context.Context, kubeconfigPath, yamlContent string) error {
	cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("oc apply failed: %v\nOutput: %s", err, string(output))
	}

	log.Printf("Applied YAML successfully: %s", strings.TrimSpace(string(output)))
	return nil
}

// updatePostDeployStatus updates the cluster's post_deploy_status
func (h *PostConfigureHandler) updatePostDeployStatus(ctx context.Context, clusterID, status string) error {
	return h.store.Clusters.UpdatePostDeployStatus(ctx, clusterID, status)
}

// createConfigTask creates a new cluster configuration task
func (h *PostConfigureHandler) createConfigTask(ctx context.Context, clusterID string, configType types.ConfigType, configName string) (string, error) {
	return h.store.ClusterConfigurations.Create(ctx, clusterID, configType, configName)
}

// updateConfigTaskStatus updates a configuration task's status
func (h *PostConfigureHandler) updateConfigTaskStatus(ctx context.Context, configID string, status types.ConfigStatus, errorMessage *string) error {
	return h.store.ClusterConfigurations.UpdateStatus(ctx, configID, status, errorMessage)
}

// isValidEnvVarName validates environment variable name format
// Valid names: alphanumeric + underscore, must start with letter or underscore
func isValidEnvVarName(name string) bool {
	// Environment variable names must match: [A-Za-z_][A-Za-z0-9_]*
	matched, _ := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*$`, name)
	return matched
}

// isDangerousEnvVar checks if an environment variable is in the blocklist
// These variables can lead to privilege escalation or code injection
func isDangerousEnvVar(name string) bool {
	// Blocklist of dangerous environment variables
	dangerousVars := map[string]bool{
		"LD_PRELOAD":       true, // Can hijack dynamic linker
		"LD_LIBRARY_PATH":  true, // Can override library loading
		"PYTHONPATH":       true, // Can inject Python modules
		"PATH":             true, // Can prepend malicious binaries
		"PERL5LIB":         true, // Can inject Perl modules
		"RUBYLIB":          true, // Can inject Ruby modules
		"NODE_PATH":        true, // Can inject Node.js modules
		"CLASSPATH":        true, // Can inject Java classes
		"JAVA_TOOL_OPTIONS": true, // Can inject Java agents
		"PROMPT_COMMAND":   true, // Executes on each shell prompt
		"BASH_ENV":         true, // Executes on shell startup
		"ENV":              true, // Executes on shell startup
		"IFS":              true, // Can break command parsing
	}

	return dangerousVars[name]
}
