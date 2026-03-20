package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigureHandler handles post-configuration tasks (e.g., installing dashboards)
type PostConfigureHandler struct {
	config *Config
	store  *store.Store
}

// NewPostConfigureHandler creates a new post-configure handler
func NewPostConfigureHandler(config *Config, st *store.Store) *PostConfigureHandler {
	return &PostConfigureHandler{
		config: config,
		store:  st,
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

// handleEKSPostConfigure installs Kubernetes Dashboard for EKS clusters
func (h *PostConfigureHandler) handleEKSPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Installing Kubernetes Dashboard for EKS cluster %s", cluster.Name)

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

	// Install Kubernetes Dashboard
	if err := h.installKubernetesDashboard(ctx, kubeconfigPath); err != nil {
		return fmt.Errorf("install kubernetes dashboard: %w", err)
	}

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

	log.Printf("Kubernetes Dashboard installed successfully at: %s", dashboardURL)
	log.Printf("Dashboard token: %s", token)

	// Update cluster outputs with dashboard URL
	if err := h.updateClusterConsoleURL(ctx, cluster.ID, dashboardURL, token); err != nil {
		return fmt.Errorf("update cluster console URL: %w", err)
	}

	return nil
}

// installKubernetesDashboard installs the Kubernetes Dashboard
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

	// Update console URL
	outputs.ConsoleURL = &consoleURL
	outputs.UpdatedAt = time.Now()

	// Store the token in a separate field or metadata
	// For now, we'll log it - in production you might want to store it securely
	log.Printf("IMPORTANT: Save this dashboard token for accessing the cluster:")
	log.Printf("Token: %s", token)

	// Upsert cluster outputs
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("upsert cluster outputs: %w", err)
	}

	log.Printf("Successfully updated cluster console URL")
	return nil
}

// handleIKSPostConfigure handles post-configuration for IKS clusters
func (h *PostConfigureHandler) handleIKSPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	// IKS clusters use the IBM Cloud console, no additional configuration needed
	log.Printf("IKS cluster %s uses IBM Cloud console, skipping post-configuration", cluster.Name)
	return nil
}
