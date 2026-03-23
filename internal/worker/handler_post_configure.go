package worker

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

// Handle handles a post-configuration job
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
		// OpenShift has built-in console, no post-configuration needed
		log.Printf("OpenShift cluster %s has built-in console, skipping post-configuration", cluster.Name)
		return nil
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

	// Create work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("create work directory: %w", err)
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
		log.Printf("Dashboard token: %s", token)

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
	time.Sleep(2 * time.Second)

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
		time.Sleep(5 * time.Second)
	}

	if externalIP == "" {
		return "", fmt.Errorf("timeout waiting for LoadBalancer external IP")
	}

	// Dashboard runs on HTTPS port 443
	dashboardURL := fmt.Sprintf("https://%s", externalIP)
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

	log.Printf("IMPORTANT: Dashboard token stored for cluster access")
	log.Printf("Token: %s", token)

	// Upsert cluster outputs
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("upsert cluster outputs: %w", err)
	}

	log.Printf("Successfully updated cluster console URL")
	return nil
}

// generateTokenKubeconfig generates a token-based kubeconfig that doesn't require AWS credentials
func (h *PostConfigureHandler) generateTokenKubeconfig(ctx context.Context, awsKubeconfigPath, outputPath string, cluster *types.Cluster) error {
	log.Println("Generating token-based kubeconfig for kubectl access...")

	// Create service account for kubectl access in kube-system namespace
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

	// Write manifest to file
	manifestPath := filepath.Join(filepath.Dir(awsKubeconfigPath), "kubectl-sa.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0600); err != nil {
		return fmt.Errorf("write kubectl service account manifest: %w", err)
	}

	// Apply manifest using AWS kubeconfig
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestPath, "--kubeconfig", awsKubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply service account: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Service account created: %s", string(output))

	// Wait for secret to be created
	time.Sleep(2 * time.Second)

	// Get the service account token
	cmd = exec.CommandContext(ctx, "kubectl", "get", "secret", "ocpctl-kubectl-token",
		"-n", "kube-system", "-o", "jsonpath={.data.token}", "--kubeconfig", awsKubeconfigPath)
	tokenBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("get token: %w\nOutput: %s", err, string(tokenBytes))
	}

	// Decode base64 token
	decodeCmd := exec.Command("base64", "-d")
	decodeCmd.Stdin = strings.NewReader(strings.TrimSpace(string(tokenBytes)))
	decodedToken, err := decodeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	token := strings.TrimSpace(string(decodedToken))

	// Get CA certificate
	cmd = exec.CommandContext(ctx, "kubectl", "get", "secret", "ocpctl-kubectl-token",
		"-n", "kube-system", "-o", "jsonpath={.data.ca\\.crt}", "--kubeconfig", awsKubeconfigPath)
	caBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("get CA cert: %w\nOutput: %s", err, string(caBytes))
	}

	// Decode base64 CA cert
	decodeCmd = exec.Command("base64", "-d")
	decodeCmd.Stdin = strings.NewReader(strings.TrimSpace(string(caBytes)))
	decodedCA, err := decodeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("decode CA cert: %w", err)
	}
	caCert := strings.TrimSpace(string(decodedCA))

	// Get API server URL
	apiURL := fmt.Sprintf("https://%s.%s.eks.amazonaws.com", cluster.Name, cluster.Region)

	// Generate kubeconfig with token authentication
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

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write kubeconfig to file
	if err := os.WriteFile(outputPath, []byte(kubeconfig), 0600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	log.Printf("Token-based kubeconfig generated at: %s", outputPath)
	return nil
}

// base64Encode encodes a string to base64
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// handleIKSPostConfigure handles post-configuration for IKS clusters
func (h *PostConfigureHandler) handleIKSPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	// IKS clusters use the IBM Cloud console, no additional configuration needed
	log.Printf("IKS cluster %s uses IBM Cloud console, skipping post-configuration", cluster.Name)
	return nil
}
