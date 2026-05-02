package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/postconfig"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Context key for job ID
type contextKey string

const jobIDContextKey contextKey = "jobID"

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
	// Add job ID to context for log streaming
	ctx = context.WithValue(ctx, jobIDContextKey, job.ID)

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

// getJobIDFromContext retrieves the job ID from context
func getJobIDFromContext(ctx context.Context) string {
	if jobID, ok := ctx.Value(jobIDContextKey).(string); ok {
		return jobID
	}
	return ""
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

// exposeDashboardNodePort exposes the dashboard via Ingress and returns the URL
// This is used for IKS clusters which use IBM Cloud's built-in Ingress controller (ALB)
func (h *PostConfigureHandler) exposeDashboardNodePort(ctx context.Context, kubeconfigPath string, cluster *types.Cluster) (string, error) {
	log.Println("Exposing dashboard via IKS Ingress (IBM Cloud ALB)...")

	// Get IKS cluster Ingress configuration
	log.Println("Getting IKS cluster Ingress configuration...")
	clusterInfoCmd := exec.CommandContext(ctx, "ibmcloud", "ks", "cluster", "get", "--cluster", cluster.Name, "--output", "json")
	clusterInfoOutput, err := clusterInfoCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get cluster info: %w\nOutput: %s", err, string(clusterInfoOutput))
	}

	// Parse cluster info to extract Ingress hostname and secret
	var clusterInfo struct {
		IngressHostname  string `json:"ingressHostname"`
		IngressSecretName string `json:"ingressSecretName"`
	}
	if err := json.Unmarshal(clusterInfoOutput, &clusterInfo); err != nil {
		return "", fmt.Errorf("parse cluster info JSON: %w", err)
	}

	if clusterInfo.IngressHostname == "" {
		return "", fmt.Errorf("cluster does not have Ingress configured")
	}

	log.Printf("IKS Ingress hostname: %s", clusterInfo.IngressHostname)
	log.Printf("IKS TLS secret: %s", clusterInfo.IngressSecretName)

	// Construct dashboard hostname using IBM's Ingress subdomain
	dashboardHost := fmt.Sprintf("dashboard.%s", clusterInfo.IngressHostname)

	// Create Ingress resource for the dashboard
	// Note: Kubernetes Dashboard backend uses HTTPS, so we need backend-protocol annotation
	ingressYAML := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kubernetes-dashboard
  namespace: kubernetes-dashboard
  annotations:
    kubernetes.io/ingress.class: "public-iks-k8s-nginx"
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
spec:
  tls:
  - hosts:
    - %s
    secretName: %s
  rules:
  - host: %s
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: kubernetes-dashboard
            port:
              number: 443
`, dashboardHost, clusterInfo.IngressSecretName, dashboardHost)

	// Write Ingress manifest to file
	ingressPath := filepath.Join(filepath.Dir(kubeconfigPath), "dashboard-ingress.yaml")
	if err := os.WriteFile(ingressPath, []byte(ingressYAML), 0600); err != nil {
		return "", fmt.Errorf("write ingress manifest: %w", err)
	}

	// Apply Ingress manifest
	log.Println("Creating Ingress resource for dashboard...")
	applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", ingressPath, "--kubeconfig", kubeconfigPath)
	applyOutput, err := applyCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("apply ingress: %w\nOutput: %s", err, string(applyOutput))
	}

	log.Printf("Ingress created: %s", string(applyOutput))

	// Wait a moment for Ingress to be configured
	time.Sleep(5 * time.Second)

	// Dashboard URL
	dashboardURL := fmt.Sprintf("https://%s", dashboardHost)
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

	// Start log streaming for job output visibility
	logPath := filepath.Join(workDir, "post-configure.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Printf("Warning: failed to create log file: %v", err)
	} else {
		defer logFile.Close()
	}

	logWriter := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		log.Print(msg)
		if logFile != nil {
			fmt.Fprintln(logFile, strings.TrimPrefix(msg, "Running post-deployment for IKS cluster "+cluster.Name))
			logFile.Sync() // Flush to disk so LogStreamer can read it
		}
	}

	// Start log streaming to database
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		logWriter("Warning: failed to start log streaming: %v", err)
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
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		_ = streamer.Stop()
		return fmt.Errorf("IBMCLOUD_API_KEY environment variable not set")
	}

	// Login to IBM Cloud (required for ibmcloud commands)
	logWriter("Logging in to IBM Cloud (region: %s)...", cluster.Region)
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region, resourceGroup); err != nil {
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		_ = streamer.Stop()
		return fmt.Errorf("IBM Cloud login: %w", err)
	}
	logWriter("IBM Cloud login successful")

	logWriter("Getting kubeconfig for IKS cluster %s...", cluster.Name)

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "kubeconfig")
	if err := iksInstaller.GetKubeconfig(ctx, cluster.Name, kubeconfigPath); err != nil {
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		_ = streamer.Stop()
		return fmt.Errorf("get kubeconfig: %w", err)
	}
	logWriter("Kubeconfig retrieved successfully")

	// Apply all manifests from profile
	hasDashboard := false
	if len(prof.PostDeployment.Manifests) > 0 {
		logWriter("Applying %d manifests from profile", len(prof.PostDeployment.Manifests))
		for _, manifest := range prof.PostDeployment.Manifests {
			logWriter("Applying manifest: %s", manifest.Name)
			if err := h.applyManifest(ctx, kubeconfigPath, manifest); err != nil {
				streamCancel()
				time.Sleep(LogBatchFlushDelay)
				_ = streamer.Stop()
				return fmt.Errorf("apply manifest %s: %w", manifest.Name, err)
			}
			// Check if this is the kubernetes-dashboard
			if manifest.Name == "kubernetes-dashboard" {
				hasDashboard = true
			}
		}
	}

	// If Kubernetes Dashboard was installed, perform dashboard-specific setup
	var dashboardURL string
	if hasDashboard {
		logWriter("Kubernetes Dashboard detected, configuring access...")

		// Create admin service account
		logWriter("Creating dashboard admin service account...")
		if err := h.createDashboardServiceAccount(ctx, kubeconfigPath); err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("create dashboard service account: %w", err)
		}

		// Get service account token
		logWriter("Retrieving dashboard token...")
		token, err := h.getDashboardToken(ctx, kubeconfigPath)
		if err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("get dashboard token: %w", err)
		}

		// Wait for IKS Ingress to be configured
		// IKS takes additional time after cluster is READY to set up Ingress controller
		logWriter("Waiting for IBM Cloud Ingress (ALB) to be configured...")
		if err := h.waitForIKSIngressReady(ctx, cluster.Name, logWriter); err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("wait for Ingress: %w", err)
		}
		logWriter("IBM Cloud Ingress is ready")

		// Expose dashboard via Ingress (using IBM Cloud's built-in Ingress controller)
		logWriter("Exposing dashboard via IBM Cloud Ingress (ALB)...")
		dashboardURL, err = h.exposeDashboardNodePort(ctx, kubeconfigPath, cluster)
		if err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("expose dashboard: %w", err)
		}

		logWriter("Dashboard Ingress created: %s", dashboardURL)
		logWriter("Waiting for IBM Cloud ALB to configure route (may take 2-3 minutes)...")

		// Wait for dashboard URL to be accessible
		if err := h.waitForDashboardURL(ctx, dashboardURL, logWriter); err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("dashboard URL verification: %w", err)
		}

		logWriter("Dashboard is accessible at: %s", dashboardURL)
		logWriter("Dashboard token stored securely in cluster outputs")

		// Update cluster outputs with dashboard URL
		if err := h.updateClusterConsoleURL(ctx, cluster.ID, dashboardURL, token); err != nil {
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			_ = streamer.Stop()
			return fmt.Errorf("update cluster console URL: %w", err)
		}
	}

	logWriter("Post-deployment completed successfully for cluster %s", cluster.Name)

	// Stop log streaming
	streamCancel()
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	return nil
}

// waitForDashboardURL polls the dashboard URL until it responds with HTTP 200
func (h *PostConfigureHandler) waitForDashboardURL(ctx context.Context, url string, logWriter func(string, ...interface{})) error {
	maxAttempts := 40 // 40 attempts * 5 seconds = 3 minutes 20 seconds timeout
	retryDelay := 5 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Use curl to test the URL (accepts self-signed certs)
		cmd := exec.CommandContext(ctx, "curl", "-k", "-s", "-o", "/dev/null", "-w", "%{http_code}", url)
		output, err := cmd.CombinedOutput()

		if err == nil && strings.TrimSpace(string(output)) == "200" {
			logWriter("Dashboard URL is now accessible (HTTP 200)")
			return nil
		}

		if attempt%6 == 0 { // Log every 30 seconds (6 attempts * 5s)
			logWriter("Still waiting for dashboard URL... (attempt %d/%d)", attempt, maxAttempts)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for dashboard URL")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("dashboard URL did not become accessible after %d attempts (%v)", maxAttempts, time.Duration(maxAttempts)*retryDelay)
}

// waitForIKSIngressReady polls the cluster until Ingress is configured
func (h *PostConfigureHandler) waitForIKSIngressReady(ctx context.Context, clusterName string, logWriter func(string, ...interface{})) error {
	maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes timeout
	retryDelay := 10 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Query cluster info for Ingress status
		cmd := exec.CommandContext(ctx, "ibmcloud", "ks", "cluster", "get", "--cluster", clusterName, "--output", "json")
		output, err := cmd.CombinedOutput()
		if err != nil {
			logWriter("Warning: failed to query cluster info (attempt %d/%d): %v", attempt, maxAttempts, err)
		} else {
			// Parse cluster info
			var clusterInfo struct {
				IngressHostname string `json:"ingressHostname"`
				IngressStatus   string `json:"ingressStatus"`
			}
			if err := json.Unmarshal(output, &clusterInfo); err == nil {
				// Check if Ingress is configured
				if clusterInfo.IngressHostname != "" && clusterInfo.IngressStatus == "healthy" {
					logWriter("Ingress is configured and healthy (hostname: %s)", clusterInfo.IngressHostname)
					return nil
				}

				// Log progress
				if attempt%6 == 0 { // Log every minute (6 attempts * 10s)
					if clusterInfo.IngressHostname == "" {
						logWriter("Ingress hostname not yet assigned (attempt %d/%d)", attempt, maxAttempts)
					} else {
						logWriter("Ingress status: %s (attempt %d/%d)", clusterInfo.IngressStatus, attempt, maxAttempts)
					}
				}
			}
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for Ingress")
		case <-time.After(retryDelay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("Ingress did not become ready after %d attempts (%v)", maxAttempts, time.Duration(maxAttempts)*retryDelay)
}

// handleOpenShiftPostConfigure handles profile-driven post-deployment for OpenShift clusters
func (h *PostConfigureHandler) handleOpenShiftPostConfigure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Running post-deployment for OpenShift cluster %s", cluster.Name)

	// Get profile
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Check if post-deployment is enabled or if custom post-config exists
	hasProfileConfig := prof.PostDeployment != nil && prof.PostDeployment.Enabled
	hasCustomConfig := cluster.CustomPostConfig != nil

	if !hasProfileConfig && !hasCustomConfig {
		log.Printf("Post-deployment not enabled for profile %s and no custom config, skipping (OpenShift has built-in console)", cluster.Profile)
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

	// Start log streaming for job output visibility
	logPath := filepath.Join(workDir, "post-configure.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		log.Printf("Warning: failed to create log file: %v", err)
	} else {
		defer logFile.Close()
	}

	// Create log writer that writes to both stdout and file
	logWriter := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		log.Print(msg)
		if logFile != nil {
			fmt.Fprintln(logFile, msg)
			logFile.Sync() // Flush to disk so LogStreamer can read it
		}
	}

	// Start log streaming to database
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		logWriter("Warning: failed to start log streaming: %v", err)
	}

	// Ensure final logs are flushed when function returns
	defer func() {
		streamCancel()
		time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}
	}()

	logWriter("Starting post-deployment configuration for OpenShift cluster %s", cluster.Name)

	// Execute profile-defined post-deployment configuration first
	if hasProfileConfig {
		logWriter("Executing profile-defined post-deployment configuration")

		// Install operators
		for _, op := range prof.PostDeployment.Operators {
			if err := h.installOperator(ctx, cluster, kubeconfigPath, op); err != nil {
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
	}

	// Execute user-defined custom post-deployment configuration
	if hasCustomConfig {
		logWriter("Executing user-defined custom post-deployment configuration with DAG-based execution")

		// Build execution DAG to resolve dependencies
		dag, err := postconfig.BuildExecutionDAG(cluster.CustomPostConfig)
		if err != nil {
			_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
			return fmt.Errorf("build execution DAG: %w", err)
		}

		logWriter("Execution order: %v", dag.ExecutionOrder)

		// Get infrastructure details for template context
		infraID, _, err := h.getClusterInfraDetails(ctx, cluster, kubeconfigPath)
		if err != nil {
			log.Printf("Warning: failed to get infra details for templating: %v", err)
			infraID = "" // Continue without infra ID
		}

		// Execute tasks in dependency order
		for _, task := range dag.GetTasksByExecutionOrder() {
			logWriter("[DAG] Executing task: %s (type=%s, dependencies=%v)", task.Name, task.Type, task.Dependencies)

			// Execute based on task type
			switch task.Type {
			case "operator":
				op := task.Config.(types.CustomOperatorConfig)
				if err := h.executeCustomOperatorWithFeatures(ctx, cluster, kubeconfigPath, op, infraID); err != nil {
					_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
					return fmt.Errorf("install custom operator %s: %w", op.Name, err)
				}
			case "script":
				script := task.Config.(types.CustomScriptConfig)
				if err := h.executeCustomScriptWithFeatures(ctx, cluster, kubeconfigPath, script, infraID); err != nil {
					_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
					return fmt.Errorf("execute custom script %s: %w", script.Name, err)
				}
			case "manifest":
				manifest := task.Config.(types.CustomManifestConfig)
				if err := h.executeCustomManifestWithFeatures(ctx, cluster, kubeconfigPath, manifest, infraID); err != nil {
					_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
					return fmt.Errorf("apply custom manifest %s: %w", manifest.Name, err)
				}
			case "helmChart":
				chart := task.Config.(types.CustomHelmChartConfig)
				if err := h.executeCustomHelmChartWithFeatures(ctx, cluster, kubeconfigPath, chart, infraID); err != nil {
					_ = h.updatePostDeployStatus(ctx, cluster.ID, "failed")
					return fmt.Errorf("install custom helm chart %s: %w", chart.Name, err)
				}
			default:
				return fmt.Errorf("unknown task type: %s", task.Type)
			}

			logWriter("[DAG] Task %s completed successfully", task.Name)
		}
	}

	// Mark as completed
	if err := h.updatePostDeployStatus(ctx, cluster.ID, "completed"); err != nil {
		return fmt.Errorf("update post-deploy status: %w", err)
	}

	logWriter("Successfully completed post-deployment configuration for cluster %s", cluster.Name)
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
	// If source is not specified, default to redhat-operators
	source := op.Source
	if source == "" {
		source = "redhat-operators"
	}

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
`, op.Name, op.Namespace, op.Channel, op.Name, source)

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

	// Build script path with security validation
	// Prevents path traversal attacks by ensuring paths are within allowed directories
	var scriptPath string
	if filepath.IsAbs(script.Path) {
		// Validate absolute paths are within OcpctlBaseDir
		validatedPath, err := validateSecurePath(script.Path, OcpctlBaseDir)
		if err != nil {
			errMsg := fmt.Sprintf("invalid script path: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		scriptPath = validatedPath
	} else {
		// Relative paths are joined with manifests directory and validated
		validatedPath, err := validateSecurePath(filepath.Join("manifests", script.Path), OcpctlBaseDir)
		if err != nil {
			errMsg := fmt.Sprintf("invalid script path: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		scriptPath = validatedPath
	}

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

		// Validate environment variable value to prevent command injection
		if !isValidEnvVarValue(value) {
			log.Printf("Warning: blocked environment variable with potentially dangerous value: %s (contains shell metacharacters)", key)
			continue
		}

		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Execute script with real-time log streaming
	cmd := exec.CommandContext(ctx, scriptPath)
	cmd.Env = env
	cmd.Dir = filepath.Dir(scriptPath) // Set working directory to script's directory

	// Create temporary log file for script output
	logFile, err := os.CreateTemp("", fmt.Sprintf("script-%s-*.log", script.Name))
	if err != nil {
		errMsg := fmt.Sprintf("create log file: %v", err)
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	defer os.Remove(logFile.Name())
	defer logFile.Close()

	// Redirect both stdout and stderr to log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Get job ID from context for log streaming
	jobID := getJobIDFromContext(ctx)
	if jobID == "" {
		log.Printf("Warning: no job ID in context for script %s, logs may not be streamed", script.Name)
	}

	// Start log streamer to stream output to deployment logs in real-time
	var streamer *LogStreamer
	if jobID != "" {
		streamer = NewLogStreamer(h.store, cluster.ID, jobID, logFile.Name())
		if err := streamer.Start(ctx); err != nil {
			log.Printf("Warning: failed to start log streamer for script %s: %v", script.Name, err)
		}
		defer func() {
			if streamer != nil {
				streamer.Stop()
			}
		}()
	}

	// Start the script
	if err := cmd.Start(); err != nil {
		errMsg := fmt.Sprintf("start script: %v", err)
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Wait for script to complete
	err = cmd.Wait()

	// Give log streamer a moment to catch up
	time.Sleep(1 * time.Second)

	if err != nil {
		// Read last 1000 bytes of log for error message
		logFile.Seek(-1000, 2) // Seek to 1000 bytes before end
		lastOutput, _ := os.ReadFile(logFile.Name())

		errMsg := fmt.Sprintf("script failed: %v\nOutput: %s", err, string(lastOutput))
		_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	log.Printf("Script %s completed successfully", script.Name)
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

	// Read manifest file with security validation
	// Prevents path traversal attacks by validating paths are within allowed directories
	var manifestPath string
	if filepath.IsAbs(manifest.Path) {
		// Validate absolute paths are within OcpctlBaseDir
		validatedPath, err := validateSecurePath(manifest.Path, OcpctlBaseDir)
		if err != nil {
			errMsg := fmt.Sprintf("invalid manifest path: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		manifestPath = validatedPath
	} else {
		// Relative paths are joined with manifests directory and validated
		validatedPath, err := validateSecurePath(filepath.Join("manifests", manifest.Path), OcpctlBaseDir)
		if err != nil {
			errMsg := fmt.Sprintf("invalid manifest path: %v", err)
			_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		manifestPath = validatedPath
	}

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

// isValidEnvVarValue validates environment variable values to prevent command injection
// Rejects values containing shell metacharacters that could be exploited
func isValidEnvVarValue(value string) bool {
	// List of dangerous shell metacharacters and sequences
	// These could be used for command injection if environment variables are used in shell contexts
	dangerousChars := []string{
		"$(",  // Command substitution
		"${",  // Parameter expansion
		"`",   // Command substitution (backticks)
		";",   // Command separator
		"|",   // Pipe
		"&",   // Background process / AND
		">",   // Redirection
		"<",   // Redirection
		"\n",  // Newline (command separator)
		"\r",  // Carriage return
		"\\",  // Escape character
		"*",   // Glob pattern
		"?",   // Glob pattern
		"[",   // Glob pattern
		"]",   // Glob pattern
		"!",   // History expansion (in some shells)
		"#",   // Comment (could hide malicious code)
	}

	for _, dangerous := range dangerousChars {
		if strings.Contains(value, dangerous) {
			return false
		}
	}

	return true
}

// validateSecurePath validates that a file path is safe to use
// Prevents path traversal attacks by ensuring absolute paths are within allowed directories
func validateSecurePath(path string, allowedBase string) (string, error) {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(path)

	// If it's an absolute path, verify it's within the allowed base directory
	if filepath.IsAbs(cleanPath) {
		// Ensure the allowed base is also clean
		cleanBase := filepath.Clean(allowedBase)

		// Check if the clean path is within the allowed base
		if !strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) && cleanPath != cleanBase {
			return "", fmt.Errorf("absolute path %s is not within allowed directory %s", path, allowedBase)
		}

		return cleanPath, nil
	}

	// For relative paths, join with base and validate the result
	fullPath := filepath.Join(allowedBase, cleanPath)
	fullPath = filepath.Clean(fullPath)

	// Verify the joined path is still within the base (prevents ../ attacks)
	cleanBase := filepath.Clean(allowedBase)
	if !strings.HasPrefix(fullPath, cleanBase+string(filepath.Separator)) && fullPath != cleanBase {
		return "", fmt.Errorf("path %s resolves outside allowed directory %s", path, allowedBase)
	}

	return fullPath, nil
}

// validateSecureURL validates that a URL is safe to download from
// Prevents SSRF attacks by blocking private/internal IP addresses and requiring HTTPS
func validateSecureURL(urlStr string) error {
	// Parse the URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTPS URLs (prevents protocol smuggling and ensures encryption)
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed, got: %s", parsedURL.Scheme)
	}

	// Resolve hostname to IP address to check if it's private
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// Check for localhost/loopback addresses
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return fmt.Errorf("localhost URLs are not allowed")
	}

	// Resolve DNS to get IP addresses
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
	}

	// Check each resolved IP
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}

		// Block private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
		if ip.IsPrivate() {
			return fmt.Errorf("private IP addresses are not allowed: %s", ipStr)
		}

		// Block loopback addresses
		if ip.IsLoopback() {
			return fmt.Errorf("loopback addresses are not allowed: %s", ipStr)
		}

		// Block link-local addresses (169.254.0.0/16 for IPv4, fe80::/10 for IPv6)
		if ip.IsLinkLocalUnicast() {
			return fmt.Errorf("link-local addresses are not allowed: %s", ipStr)
		}

		// Block AWS metadata endpoint specifically (169.254.169.254)
		if ipStr == "169.254.169.254" {
			return fmt.Errorf("AWS metadata endpoint is blocked")
		}
	}

	return nil
}

// Custom post-config handlers with user tracking

// installCustomOperator installs a user-defined operator with tracking
func (h *PostConfigureHandler) installCustomOperator(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, customOp types.CustomOperatorConfig) error {
	log.Printf("[CUSTOM POST-CONFIG] Installing custom operator: %s in namespace %s (user-defined, owner: %s)", customOp.Name, customOp.Namespace, cluster.OwnerID)

	// Convert to profile OperatorConfig
	profileOp := profile.OperatorConfig{
		Name:      customOp.Name,
		Namespace: customOp.Namespace,
		Source:    customOp.Source,
		Channel:   customOp.Channel,
	}

	// Convert CustomResource if provided
	if customOp.CustomResource != nil {
		profileOp.CustomResource = &profile.CustomResourceConfig{
			APIVersion: customOp.CustomResource.APIVersion,
			Kind:       customOp.CustomResource.Kind,
			Name:       customOp.CustomResource.Name,
			Namespace:  customOp.CustomResource.Namespace,
			Spec:       customOp.CustomResource.Spec,
		}
	}

	// Call existing installOperator method (creates its own tracking)
	// TODO: Phase 4 - Enhance to use CreateWithTracking for full audit trail
	return h.installOperator(ctx, cluster, kubeconfigPath, profileOp)
}

// executeCustomScript executes a user-defined script with tracking
func (h *PostConfigureHandler) executeCustomScript(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, customScript types.CustomScriptConfig) error {
	log.Printf("[CUSTOM POST-CONFIG] Executing custom script: %s (user-defined, owner: %s)", customScript.Name, cluster.OwnerID)

	// Handle inline content or URL
	scriptPath := ""
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	if customScript.Content != "" {
		// Inline script content - write to temp file
		scriptsDir := filepath.Join(workDir, "custom-scripts")
		if err := os.MkdirAll(scriptsDir, 0700); err != nil {
			return fmt.Errorf("create scripts dir: %w", err)
		}

		scriptPath = filepath.Join(scriptsDir, customScript.Name+".sh")
		if err := os.WriteFile(scriptPath, []byte(customScript.Content), 0700); err != nil {
			return fmt.Errorf("write script file: %w", err)
		}
	} else if customScript.URL != "" {
		// Download script from URL with security validation
		scriptsDir := filepath.Join(workDir, "custom-scripts")
		if err := os.MkdirAll(scriptsDir, 0700); err != nil {
			return fmt.Errorf("create scripts dir: %w", err)
		}

		scriptPath = filepath.Join(scriptsDir, customScript.Name+".sh")

		log.Printf("[CUSTOM POST-CONFIG] Downloading script from URL: %s", customScript.URL)

		// Validate URL to prevent SSRF attacks
		if err := validateSecureURL(customScript.URL); err != nil {
			return fmt.Errorf("invalid script URL: %w", err)
		}

		// Create HTTP client with timeout to prevent indefinite hangs
		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		// Download the script
		resp, err := client.Get(customScript.URL)
		if err != nil {
			return fmt.Errorf("download script from URL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to download script: HTTP %d", resp.StatusCode)
		}

		// Read the response body with size limit (10MB max)
		limitedReader := io.LimitReader(resp.Body, 10*1024*1024)
		scriptContent, err := io.ReadAll(limitedReader)
		if err != nil {
			return fmt.Errorf("read script content: %w", err)
		}

		// Write to file with execute permissions
		if err := os.WriteFile(scriptPath, scriptContent, 0700); err != nil {
			return fmt.Errorf("write script file: %w", err)
		}

		log.Printf("[CUSTOM POST-CONFIG] Downloaded script to: %s (%d bytes)", scriptPath, len(scriptContent))
	} else if customScript.Path != "" {
		// Path to script with security validation
		// Prevents path traversal attacks by validating paths are within allowed directories
		if filepath.IsAbs(customScript.Path) {
			// Validate absolute paths are within OcpctlBaseDir
			validatedPath, err := validateSecurePath(customScript.Path, OcpctlBaseDir)
			if err != nil {
				return fmt.Errorf("invalid script path: %w", err)
			}
			scriptPath = validatedPath
		} else {
			// Relative paths are joined with manifests directory and validated
			validatedPath, err := validateSecurePath(filepath.Join("manifests", customScript.Path), OcpctlBaseDir)
			if err != nil {
				return fmt.Errorf("invalid script path: %w", err)
			}
			scriptPath = validatedPath
		}

		log.Printf("[CUSTOM POST-CONFIG] Using script from path: %s", scriptPath)

		// Verify script exists
		if _, err := os.Stat(scriptPath); err != nil {
			return fmt.Errorf("script not found at path %s: %w", scriptPath, err)
		}
	} else {
		return fmt.Errorf("script must have either content, url, or path")
	}

	// Convert to profile ScriptConfig
	profileScript := profile.ScriptConfig{
		Name:        customScript.Name,
		Path:        scriptPath,
		Description: customScript.Description,
		Env:         customScript.Env,
	}

	// Call existing executeScript method (creates its own tracking)
	// TODO: Phase 4 - Enhance to use CreateWithTracking for full audit trail
	return h.executeScript(ctx, cluster, kubeconfigPath, profileScript)
}

// applyCustomManifest applies a user-defined manifest with tracking
func (h *PostConfigureHandler) applyCustomManifest(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, customManifest types.CustomManifestConfig) error {
	log.Printf("[CUSTOM POST-CONFIG] Applying custom manifest: %s (user-defined, owner: %s)", customManifest.Name, cluster.OwnerID)

	// Handle inline content or URL
	manifestPath := ""
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	if customManifest.Content != "" {
		// Inline manifest content - write to temp file
		manifestsDir := filepath.Join(workDir, "custom-manifests")
		if err := os.MkdirAll(manifestsDir, 0700); err != nil {
			return fmt.Errorf("create manifests dir: %w", err)
		}

		manifestPath = filepath.Join(manifestsDir, customManifest.Name+".yaml")
		if err := os.WriteFile(manifestPath, []byte(customManifest.Content), 0600); err != nil {
			return fmt.Errorf("write manifest file: %w", err)
		}
	} else if customManifest.URL != "" {
		// URL-based manifest - will be handled by applyOpenShiftManifest
		manifestPath = customManifest.URL
	} else {
		return fmt.Errorf("manifest must have either content or url")
	}

	// Convert to profile ManifestConfig
	profileManifest := profile.ManifestConfig{
		Name:        customManifest.Name,
		Description: customManifest.Description,
		Namespace:   customManifest.Namespace,
	}

	// Set path or URL based on which was provided
	if customManifest.URL != "" {
		profileManifest.URL = customManifest.URL
	} else{
		profileManifest.Path = manifestPath
	}

	// Call existing applyOpenShiftManifest method
	// TODO: In Phase 2, enhance tracking to mark as user-defined
	return h.applyOpenShiftManifest(ctx, cluster, kubeconfigPath, profileManifest)
}

// installCustomHelmChart installs a user-defined Helm chart with tracking
func (h *PostConfigureHandler) installCustomHelmChart(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, customChart types.CustomHelmChartConfig) error {
	log.Printf("[CUSTOM POST-CONFIG] Installing custom Helm chart: %s from repo %s (user-defined, owner: %s)", customChart.Name, customChart.Repo, cluster.OwnerID)

	// Convert to profile HelmChartConfig
	profileChart := profile.HelmChartConfig{
		Name:   customChart.Name,
		Repo:   customChart.Repo,
		Chart:  customChart.Chart,
		Values: customChart.Values,
	}

	// Call existing installHelmChart method
	// TODO: In Phase 2, enhance tracking to mark as user-defined
	return h.installHelmChart(ctx, cluster, kubeconfigPath, profileChart)
}

// Phase 4: Advanced execution methods with template rendering, conditional execution, and variable support

// executeCustomOperatorWithFeatures installs an operator with conditional execution support
func (h *PostConfigureHandler) executeCustomOperatorWithFeatures(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, op types.CustomOperatorConfig, infraID string) error {
	// Build template context
	templateCtx := postconfig.BuildTemplateContext(cluster, infraID, nil)

	// Evaluate condition
	if op.Condition != "" {
		shouldExecute, err := postconfig.EvaluateCondition(op.Condition, templateCtx)
		if err != nil {
			return fmt.Errorf("evaluate condition: %w", err)
		}
		if !shouldExecute {
			log.Printf("[CONDITIONAL] Skipping operator %s (condition not met: %s)", op.Name, op.Condition)
			return nil
		}
		log.Printf("[CONDITIONAL] Operator %s condition met: %s", op.Name, op.Condition)
	}

	// Execute operator installation
	return h.installCustomOperator(ctx, cluster, kubeconfigPath, op)
}

// executeCustomScriptWithFeatures executes a script with template rendering, variables, and conditional execution
func (h *PostConfigureHandler) executeCustomScriptWithFeatures(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, script types.CustomScriptConfig, infraID string) error {
	// Build template context with custom variables
	templateCtx := postconfig.BuildTemplateContext(cluster, infraID, script.Variables)

	// Evaluate condition
	if script.Condition != "" {
		shouldExecute, err := postconfig.EvaluateCondition(script.Condition, templateCtx)
		if err != nil {
			return fmt.Errorf("evaluate condition: %w", err)
		}
		if !shouldExecute {
			log.Printf("[CONDITIONAL] Skipping script %s (condition not met: %s)", script.Name, script.Condition)
			return nil
		}
		log.Printf("[CONDITIONAL] Script %s condition met: %s", script.Name, script.Condition)
	}

	// Render template for script content
	renderedContent := script.Content
	if script.Content != "" {
		rendered, err := postconfig.RenderTemplate(script.Content, templateCtx)
		if err != nil {
			return fmt.Errorf("render script content: %w", err)
		}
		renderedContent = rendered
		log.Printf("[TEMPLATE] Script %s content rendered with variables", script.Name)
	}

	// Render environment variables
	renderedEnv, err := postconfig.RenderMapValues(script.Env, templateCtx)
	if err != nil {
		return fmt.Errorf("render environment variables: %w", err)
	}

	// Create modified script config with rendered values
	renderedScript := script
	renderedScript.Content = renderedContent
	renderedScript.Env = renderedEnv

	// Execute script with rendered values
	return h.executeCustomScript(ctx, cluster, kubeconfigPath, renderedScript)
}

// executeCustomManifestWithFeatures applies a manifest with template rendering and conditional execution
func (h *PostConfigureHandler) executeCustomManifestWithFeatures(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, manifest types.CustomManifestConfig, infraID string) error {
	// Build template context with custom variables
	templateCtx := postconfig.BuildTemplateContext(cluster, infraID, manifest.Variables)

	// Evaluate condition
	if manifest.Condition != "" {
		shouldExecute, err := postconfig.EvaluateCondition(manifest.Condition, templateCtx)
		if err != nil {
			return fmt.Errorf("evaluate condition: %w", err)
		}
		if !shouldExecute {
			log.Printf("[CONDITIONAL] Skipping manifest %s (condition not met: %s)", manifest.Name, manifest.Condition)
			return nil
		}
		log.Printf("[CONDITIONAL] Manifest %s condition met: %s", manifest.Name, manifest.Condition)
	}

	// Render template for manifest content
	renderedContent := manifest.Content
	if manifest.Content != "" {
		rendered, err := postconfig.RenderTemplate(manifest.Content, templateCtx)
		if err != nil {
			return fmt.Errorf("render manifest content: %w", err)
		}
		renderedContent = rendered
		log.Printf("[TEMPLATE] Manifest %s content rendered with variables", manifest.Name)
	}

	// Render namespace
	renderedNamespace := manifest.Namespace
	if manifest.Namespace != "" {
		rendered, err := postconfig.RenderTemplate(manifest.Namespace, templateCtx)
		if err != nil {
			return fmt.Errorf("render namespace: %w", err)
		}
		renderedNamespace = rendered
	}

	// Create modified manifest config with rendered values
	renderedManifest := manifest
	renderedManifest.Content = renderedContent
	renderedManifest.Namespace = renderedNamespace

	// Execute manifest with rendered values
	return h.applyCustomManifest(ctx, cluster, kubeconfigPath, renderedManifest)
}

// executeCustomHelmChartWithFeatures installs a Helm chart with template rendering and conditional execution
func (h *PostConfigureHandler) executeCustomHelmChartWithFeatures(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, chart types.CustomHelmChartConfig, infraID string) error {
	// Build template context with custom variables
	templateCtx := postconfig.BuildTemplateContext(cluster, infraID, chart.Variables)

	// Evaluate condition
	if chart.Condition != "" {
		shouldExecute, err := postconfig.EvaluateCondition(chart.Condition, templateCtx)
		if err != nil {
			return fmt.Errorf("evaluate condition: %w", err)
		}
		if !shouldExecute {
			log.Printf("[CONDITIONAL] Skipping Helm chart %s (condition not met: %s)", chart.Name, chart.Condition)
			return nil
		}
		log.Printf("[CONDITIONAL] Helm chart %s condition met: %s", chart.Name, chart.Condition)
	}

	// Render namespace
	renderedNamespace := chart.Namespace
	if chart.Namespace != "" {
		rendered, err := postconfig.RenderTemplate(chart.Namespace, templateCtx)
		if err != nil {
			return fmt.Errorf("render namespace: %w", err)
		}
		renderedNamespace = rendered
	}

	// Render Helm values (string values only)
	renderedValues := make(map[string]interface{})
	for k, v := range chart.Values {
		if strVal, ok := v.(string); ok {
			rendered, err := postconfig.RenderTemplate(strVal, templateCtx)
			if err != nil {
				return fmt.Errorf("render helm value %s: %w", k, err)
			}
			renderedValues[k] = rendered
		} else {
			renderedValues[k] = v
		}
	}

	if len(renderedValues) > 0 {
		log.Printf("[TEMPLATE] Helm chart %s values rendered with variables", chart.Name)
	}

	// Create modified chart config with rendered values
	renderedChart := chart
	renderedChart.Namespace = renderedNamespace
	renderedChart.Values = renderedValues

	// Execute Helm chart with rendered values
	return h.installCustomHelmChart(ctx, cluster, kubeconfigPath, renderedChart)
}
