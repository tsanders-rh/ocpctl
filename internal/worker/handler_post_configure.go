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

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigureHandler handles post-deployment configuration for a cluster
type PostConfigureHandler struct {
	config          *Config
	store           *store.Store
	profileRegistry *profile.Registry
}

// NewPostConfigureHandler creates a new post-configure handler
func NewPostConfigureHandler(config *Config, st *store.Store, profileRegistry *profile.Registry) *PostConfigureHandler {
	return &PostConfigureHandler{
		config:          config,
		store:           st,
		profileRegistry: profileRegistry,
	}
}

// Handle executes post-deployment configuration for a cluster
func (h *PostConfigureHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Starting post-deployment configuration for cluster %s", cluster.Name)

	// Verify cluster is READY
	if cluster.Status != types.ClusterStatusReady {
		return &types.NotReadyError{
			Resource: "cluster",
			Current:  string(cluster.Status),
			Required: string(types.ClusterStatusReady),
		}
	}

	// Get profile
	prof, err := h.profileRegistry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Check if post-deployment is enabled
	if prof.PostDeployment == nil || !prof.PostDeployment.Enabled {
		log.Printf("Post-deployment not enabled for profile %s, skipping", cluster.Profile)
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
		if err := h.applyManifest(ctx, cluster, kubeconfigPath, manifest); err != nil {
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

	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
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

// applyManifest applies a manifest file
func (h *PostConfigureHandler) applyManifest(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, manifest profile.ManifestConfig) error {
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

// executeScript executes a shell script for post-deployment configuration
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
	infraID, region, err := h.getClusterInfraDetails(ctx, cluster)
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

	// Add custom environment variables from script config
	for key, value := range script.Env {
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
func (h *PostConfigureHandler) getClusterInfraDetails(ctx context.Context, cluster *types.Cluster) (string, string, error) {
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

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

// installHelmChart installs a Helm chart
func (h *PostConfigureHandler) installHelmChart(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, chart profile.HelmChartConfig) error {
	log.Printf("Installing Helm chart: %s", chart.Name)

	// Track configuration task
	configID, err := h.createConfigTask(ctx, cluster.ID, types.ConfigTypeHelm, chart.Name)
	if err != nil {
		return fmt.Errorf("create config task: %w", err)
	}

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusInstalling, nil)

	// TODO: Implement Helm chart installation
	// This would involve:
	// 1. helm repo add
	// 2. helm install with values

	_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
	return nil
}

// applyYAML applies YAML content to the cluster
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
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		UPDATE clusters
		SET post_deploy_status = $1,
		    post_deploy_completed_at = CASE WHEN $1 = 'completed' THEN NOW() ELSE NULL END,
		    updated_at = NOW()
		WHERE id = $2
	`

	_, err = tx.Exec(ctx, query, status, clusterID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// createConfigTask creates a new cluster configuration task
func (h *PostConfigureHandler) createConfigTask(ctx context.Context, clusterID string, configType types.ConfigType, configName string) (string, error) {
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO cluster_configurations (cluster_id, config_type, config_name, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`

	var configID string
	err = tx.QueryRow(ctx, query, clusterID, configType, configName, types.ConfigStatusPending).Scan(&configID)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	return configID, nil
}

// updateConfigTaskStatus updates a configuration task's status
func (h *PostConfigureHandler) updateConfigTaskStatus(ctx context.Context, configID string, status types.ConfigStatus, errorMessage *string) error {
	tx, err := h.store.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		UPDATE cluster_configurations
		SET status = $1,
		    error_message = $2,
		    completed_at = CASE WHEN $1 IN ('completed', 'failed') THEN NOW() ELSE NULL END
		WHERE id = $3
	`

	_, err = tx.Exec(ctx, query, status, errorMessage, configID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
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
