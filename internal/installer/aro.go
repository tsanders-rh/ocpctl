package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors for ARO service-principal handling (#88).
var (
	// ErrInsufficientGraphPermissions is returned by CreateServicePrincipal when
	// the logged-in identity lacks Microsoft Graph write (Application.ReadWrite.
	// OwnedBy) and therefore cannot create a per-cluster service principal. Callers
	// use it to fall back to a shared SP instead of failing the create.
	ErrInsufficientGraphPermissions = errors.New("insufficient Microsoft Graph permissions to create a service principal")

	// ErrDuplicateClientID is returned when az aro create rejects the cluster's
	// service principal because Azure has it bound to another ARO cluster (a shared
	// SP can back only one ARO cluster at a time).
	ErrDuplicateClientID = errors.New("service principal already in use by another ARO cluster")
)

// AROInstaller wraps the Azure CLI for ARO cluster operations
type AROInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// AROClusterConfig represents an ARO cluster configuration
type AROClusterConfig struct {
	Name             string
	SubscriptionID   string
	ResourceGroup    string
	Region           string
	MasterVMSize     string
	WorkerVMSize     string
	WorkerCount      int
	OpenShiftVersion string
	PullSecret       string
	Tags             map[string]string
	// ClientID/ClientSecret are the cluster service principal passed to
	// az aro create (Red Hat recommended BYO-SP pattern). Providing an existing
	// SP prevents az from creating a new app registration in Entra ID, which
	// would require Microsoft Graph write permissions the worker does not have.
	ClientID     string
	ClientSecret string
}

// AROClusterInfo represents cluster information from Azure
type AROClusterInfo struct {
	Name              string `json:"name"`
	ProvisioningState string `json:"provisioningState"`
	// az aro show returns the URLs nested under consoleProfile/apiserverProfile.
	// Go's encoding/json does not treat dotted json tags as nested-path access,
	// so these must be modeled as nested structs and flattened after unmarshal.
	ConsoleProfile struct {
		URL string `json:"url"`
	} `json:"consoleProfile"`
	APIServerProfile struct {
		URL string `json:"url"`
	} `json:"apiserverProfile"`
	Tags map[string]string `json:"tags"`

	// ConsoleURL/APIURL are flattened from the nested profiles above in
	// GetClusterInfo so callers keep a stable, simple accessor.
	ConsoleURL string `json:"-"`
	APIURL     string `json:"-"`
}

// NewAROInstaller creates a new ARO installer instance
func NewAROInstaller() *AROInstaller {
	binaryPath := os.Getenv("AZ_BINARY")
	if binaryPath == "" {
		binaryPath = "az"
	}

	return &AROInstaller{
		binaryPath: binaryPath,
		timeout:    90 * time.Minute, // ARO clusters take 30-40 minutes
	}
}

// VerifyAuthentication checks if Azure CLI is authenticated
func (a *AROInstaller) VerifyAuthentication(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.binaryPath, "account", "show", "--output", "json")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Azure CLI not authenticated: %w\nRun: az login", err)
	}

	return nil
}

// CreateVNet creates an Azure Virtual Network for ARO
func (a *AROInstaller) CreateVNet(ctx context.Context, resourceGroup, vnetName, region string) error {
	args := []string{
		"network", "vnet", "create",
		"--resource-group", resourceGroup,
		"--name", vnetName,
		"--location", region,
		"--address-prefixes", "10.0.0.0/16",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create vnet: %w: %s", err, stderr.String())
	}

	return nil
}

// CreateSubnet creates a subnet within a VNet
func (a *AROInstaller) CreateSubnet(ctx context.Context, resourceGroup, vnetName, subnetName, addressPrefix string, serviceEndpoints bool) error {
	args := []string{
		"network", "vnet", "subnet", "create",
		"--resource-group", resourceGroup,
		"--vnet-name", vnetName,
		"--name", subnetName,
		"--address-prefixes", addressPrefix,
	}

	// ARO master and worker subnets need service endpoints
	if serviceEndpoints {
		args = append(args, "--service-endpoints", "Microsoft.ContainerRegistry")
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create subnet %s: %w: %s", subnetName, err, stderr.String())
	}

	return nil
}

// ResolveVersion turns a MAJOR.MINOR OpenShift version (e.g. "4.20") into the
// highest available patch release (e.g. "4.20.15") for the given Azure region.
// `az aro create --version` rejects a bare minor with "--version is invalid",
// but ocpctl profiles declare minor versions by convention. This bridges that
// gap, mirroring the ROSA ResolveVersion path.
//
// Already fully-qualified versions (X.Y.Z) and empty versions are returned
// unchanged (empty lets az aro create pick its own default).
func (a *AROInstaller) ResolveVersion(ctx context.Context, region, version string) (string, error) {
	if version == "" || fullPatchVersionRe.MatchString(version) {
		return version, nil
	}
	if !minorVersionRe.MatchString(version) {
		return "", fmt.Errorf("unrecognized OpenShift version %q: expected MAJOR.MINOR or MAJOR.MINOR.PATCH", version)
	}

	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(listCtx, a.binaryPath, "aro", "get-versions", "--location", region, "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("az aro get-versions: %w: %s", err, stderr.String())
	}

	// az aro get-versions returns a JSON array of patch version strings.
	var versions []string
	if err := json.Unmarshal(stdout.Bytes(), &versions); err != nil {
		return "", fmt.Errorf("parse aro versions: %w", err)
	}

	// Pick the highest patch matching the requested minor.
	prefix := version + "."
	best := ""
	bestPatch := -1
	for _, v := range versions {
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		patch, ok := patchNumber(v)
		if !ok {
			continue
		}
		if patch > bestPatch {
			bestPatch = patch
			best = v
		}
	}

	if best == "" {
		return "", fmt.Errorf("no ARO-supported patch version found for OpenShift %s in region %s", version, region)
	}
	return best, nil
}

// ServicePrincipal is a freshly created Azure AD application/service principal
// used as a cluster's BYO SP for az aro create.
type ServicePrincipal struct {
	DisplayName  string
	ClientID     string // appId
	ClientSecret string // password
}

// CreateServicePrincipal creates a dedicated Azure AD app + service principal for
// one ARO cluster and grants it Contributor on the cluster's resource group only.
//
// Why per-cluster (#88): Azure binds an ARO cluster to its SP by client ID, so a
// single shared SP can back only one ARO cluster at a time — a second concurrent
// create fails with DuplicateClientID. Giving every cluster its own SP removes
// that ceiling. It also shrinks #81's blast radius: each secret is ephemeral,
// scoped to a single resource group, and deleted when the cluster is destroyed.
//
// The cluster SP needs only Contributor on its RG. The *caller* (the worker SP
// running az aro create) is the identity that assigns the ARO resource-provider
// roles on the VNet and therefore needs User Access Administrator — the cluster
// SP does not.
//
// Requires the logged-in worker SP to hold Microsoft Graph Application.ReadWrite.
// OwnedBy. When that grant is absent az fails with an authorization error; this
// returns ErrInsufficientGraphPermissions so the caller can fall back to a shared
// SP rather than failing the create outright.
func (a *AROInstaller) CreateServicePrincipal(ctx context.Context, displayName, subscriptionID, resourceGroup string) (*ServicePrincipal, error) {
	if subscriptionID == "" || resourceGroup == "" {
		return nil, fmt.Errorf("CreateServicePrincipal: subscriptionID and resourceGroup are required")
	}

	scope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s", subscriptionID, resourceGroup)
	args := []string{
		"ad", "sp", "create-for-rbac",
		"--name", displayName,
		"--role", "Contributor",
		"--scopes", scope,
		"--output", "json",
	}

	// create-for-rbac emits {"appId","password","tenant","displayName"}.
	var out struct {
		AppID       string `json:"appId"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}

	// create-for-rbac creates the app registration and then, in the same call,
	// creates the Contributor role assignment that references the freshly created
	// principal. Azure AD is eventually consistent, so that assignment step can fire
	// before the new principal has replicated, failing with "Resource ... does not
	// exist or one of its queried reference-property objects are not present" (#88).
	// Retry to let replication catch up. create-for-rbac is idempotent by display
	// name: a retry reuses the same app registration (stable appId) and only needs
	// the role assignment to succeed once the principal is visible.
	const maxAttempts = 5
	const retryDelay = 20 * time.Second
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, a.binaryPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil {
			if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
				return nil, fmt.Errorf("parse create-for-rbac output: %w", jerr)
			}
			if out.AppID == "" || out.Password == "" {
				return nil, fmt.Errorf("create-for-rbac returned an empty appId/password")
			}
			break
		}

		combined := stdout.String() + stderr.String()
		if isInsufficientGraphPermissions(combined) {
			return nil, ErrInsufficientGraphPermissions
		}
		lastErr = fmt.Errorf("az ad sp create-for-rbac failed (attempt %d/%d): %w: %s",
			attempt, maxAttempts, err, strings.TrimSpace(stderr.String()))
		if isTransientAADReplication(combined) && attempt < maxAttempts {
			log.Printf("ARO service principal %s not yet replicated in Azure AD (attempt %d/%d), retrying in %s",
				displayName, attempt, maxAttempts, retryDelay)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("cancelled while waiting for service principal to replicate: %w", ctx.Err())
			case <-time.After(retryDelay):
			}
			continue
		}
		return nil, lastErr
	}

	sp := &ServicePrincipal{
		DisplayName:  displayName,
		ClientID:     out.AppID,
		ClientSecret: out.Password,
	}

	// Wait for the new SP to replicate through Azure AD before az aro create tries
	// to authenticate with it. Using the credential too soon yields AADSTS7000215
	// ("invalid client secret") purely from propagation lag, not a bad secret.
	if err := a.waitForServicePrincipal(ctx, sp.ClientID); err != nil {
		return nil, fmt.Errorf("service principal %s did not become available: %w", displayName, err)
	}

	return sp, nil
}

// waitForServicePrincipal polls until the service principal object is visible in
// Azure AD, then adds a short buffer for token-endpoint propagation. az ad sp show
// is a read against the worker's existing session, so it does not disturb the
// active az login used by az aro create.
func (a *AROInstaller) waitForServicePrincipal(ctx context.Context, clientID string) error {
	const (
		maxWait  = 3 * time.Minute
		interval = 10 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	for {
		showCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cmd := exec.CommandContext(showCtx, a.binaryPath, "ad", "sp", "show", "--id", clientID, "--output", "json")
		err := cmd.Run()
		cancel()
		if err == nil {
			// Object is queryable; give the token endpoint a moment to catch up
			// before the credential is used to authenticate.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for service principal %s", maxWait, clientID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// DeleteServicePrincipal removes the Azure AD application (and its backing service
// principal) identified by display name. It is best-effort and idempotent: a
// missing SP is treated as success so cluster destroy never blocks on it. Deleting
// the app registration cascades to the service principal.
func (a *AROInstaller) DeleteServicePrincipal(ctx context.Context, displayName string) error {
	// Look up the app registration by display name to get its appId.
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	listCmd := exec.CommandContext(listCtx, a.binaryPath,
		"ad", "app", "list", "--display-name", displayName, "--query", "[].appId", "--output", "tsv")
	var stdout, stderr bytes.Buffer
	listCmd.Stdout = &stdout
	listCmd.Stderr = &stderr
	if err := listCmd.Run(); err != nil {
		return fmt.Errorf("az ad app list for %s failed: %w: %s", displayName, err, stderr.String())
	}

	appIDs := strings.Fields(stdout.String())
	if len(appIDs) == 0 {
		// Nothing to delete (never created, or already cleaned up).
		return nil
	}

	for _, appID := range appIDs {
		delCtx, cancelDel := context.WithTimeout(ctx, 60*time.Second)
		delCmd := exec.CommandContext(delCtx, a.binaryPath, "ad", "app", "delete", "--id", appID)
		var delErr bytes.Buffer
		delCmd.Stderr = &delErr
		err := delCmd.Run()
		cancelDel()
		if err != nil {
			return fmt.Errorf("az ad app delete %s failed: %w: %s", appID, err, delErr.String())
		}
	}

	return nil
}

// isInsufficientGraphPermissions reports whether az output indicates the caller
// lacks Microsoft Graph write permission to create an app/service principal.
func isInsufficientGraphPermissions(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "authorization_requestdenied") ||
		strings.Contains(l, "insufficient privileges") ||
		strings.Contains(l, "does not have permission") ||
		(strings.Contains(l, "graph") && strings.Contains(l, "forbidden"))
}

// isTransientAADReplication reports whether az output indicates a transient Azure
// AD eventual-consistency failure — the created principal is not yet visible to a
// dependent operation (e.g. create-for-rbac's role assignment). These clear on
// their own once replication completes, so callers should retry rather than fail.
func isTransientAADReplication(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "does not exist or one of its queried reference-property objects are not present") ||
		strings.Contains(l, "reference-property objects are not present")
}

// isDuplicateClientID reports whether az aro create failed because the supplied
// service principal is already bound to another ARO cluster.
func isDuplicateClientID(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "duplicateclientid") ||
		(strings.Contains(l, "client id") && strings.Contains(l, "already"))
}

// CreateCluster creates an ARO cluster using az aro create
func (a *AROInstaller) CreateCluster(ctx context.Context, config *AROClusterConfig, logFile string, vnetName, masterSubnetName, workerSubnetName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build az aro create command
	args := []string{
		"aro", "create",
		"--resource-group", config.ResourceGroup,
		"--name", config.Name,
		"--vnet", vnetName,
		"--master-subnet", masterSubnetName,
		"--worker-subnet", workerSubnetName,
		"--master-vm-size", config.MasterVMSize,
		"--worker-vm-size", config.WorkerVMSize,
		"--worker-count", fmt.Sprintf("%d", config.WorkerCount),
	}

	// Add version if specified
	if config.OpenShiftVersion != "" {
		args = append(args, "--version", config.OpenShiftVersion)
	}

	// BYO service principal (Red Hat recommended pattern). Passing an existing SP
	// stops az aro create from creating a new app registration in Entra ID, which
	// requires Microsoft Graph write permissions the worker SP lacks.
	//
	// The secret is passed on argv (visible via ps, see #81). This was tried via
	// Azure CLI's @<file> convention to keep it out of argv, but az does NOT expand
	// @file for --client-secret (verified: az login -p @file yields AADSTS7000215
	// "Invalid client secret" — the literal "@/path" is sent). argv is the only
	// supported form, so #81 must be mitigated by blast-radius reduction instead
	// (dedicated least-privilege ARO SP / short-lived secret / managed identity).
	if config.ClientID != "" && config.ClientSecret != "" {
		args = append(args, "--client-id", config.ClientID, "--client-secret", config.ClientSecret)
	}

	// Add pull secret
	if config.PullSecret != "" {
		args = append(args, "--pull-secret", config.PullSecret)
	}

	// Add tags. Each key=value pair must be a separate argument to --tags; joining
	// them into a single space-delimited string makes az treat the whole string as
	// one tag value, which trips Azure's 256-char tag-value limit.
	if len(config.Tags) > 0 {
		args = append(args, "--tags")
		for k, v := range config.Tags {
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	// Open log file for appending
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Run(); err != nil {
		logData, _ := os.ReadFile(logFile)
		// Fail fast with an actionable error when Azure rejects the SP because it
		// already backs another ARO cluster. Retrying is pointless — the operator
		// must supply a distinct SP (per-cluster SP creation, #88).
		if isDuplicateClientID(string(logData)) {
			return string(logData), fmt.Errorf("%w: client id %s is already assigned to another ARO cluster; a service principal can back only one ARO cluster at a time", ErrDuplicateClientID, config.ClientID)
		}
		return string(logData), fmt.Errorf("az aro create failed: %w", err)
	}

	logData, _ := os.ReadFile(logFile)
	return string(logData), nil
}

// DestroyCluster destroys an ARO cluster using az aro delete
func (a *AROInstaller) DestroyCluster(ctx context.Context, resourceGroup, clusterName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	args := []string{
		"aro", "delete",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--yes", // Skip confirmation
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("az aro delete failed: %w", err)
	}

	return stdout.String(), nil
}

// GetClusterInfo retrieves ARO cluster details
func (a *AROInstaller) GetClusterInfo(ctx context.Context, resourceGroup, clusterName string) (*AROClusterInfo, error) {
	args := []string{
		"aro", "show",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--output", "json",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az aro show failed: %w", err)
	}

	var info AROClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	// Flatten the nested profile URLs into the simple accessors.
	info.ConsoleURL = info.ConsoleProfile.URL
	info.APIURL = info.APIServerProfile.URL

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for an ARO cluster
func (a *AROInstaller) GetKubeconfig(ctx context.Context, resourceGroup, clusterName, outputPath string) error {
	args := []string{
		"aro", "get-admin-kubeconfig",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--file", outputPath,
	}

	// Create auth directory if it doesn't exist
	authDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	return nil
}

// AROCredentials holds the kubeadmin console credentials for an ARO cluster,
// as returned by `az aro list-credentials`.
type AROCredentials struct {
	KubeadminUsername string `json:"kubeadminUsername"`
	KubeadminPassword string `json:"kubeadminPassword"`
}

// ListCredentials retrieves the kubeadmin username/password for an ARO cluster
// via `az aro list-credentials`. Azure manages these credentials (they are not
// derivable from the kubeconfig), so this is the only way to surface console
// login credentials for ARO — mirroring the admin credentials ROSA/IPI persist.
func (a *AROInstaller) ListCredentials(ctx context.Context, resourceGroup, clusterName string) (*AROCredentials, error) {
	args := []string{
		"aro", "list-credentials",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--output", "json",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az aro list-credentials failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var creds AROCredentials
	if err := json.Unmarshal(stdout.Bytes(), &creds); err != nil {
		return nil, fmt.Errorf("parse aro credentials: %w", err)
	}
	if creds.KubeadminUsername == "" || creds.KubeadminPassword == "" {
		return nil, fmt.Errorf("az aro list-credentials returned empty credentials")
	}

	return &creds, nil
}

// CreateResourceGroup creates an Azure resource group
func (a *AROInstaller) CreateResourceGroup(ctx context.Context, name, region string, tags map[string]string) error {
	args := []string{
		"group", "create",
		"--name", name,
		"--location", region,
	}

	// Add tags. Each key=value pair must be a separate argument to --tags; joining
	// them into a single space-delimited string makes az treat the whole string as
	// one tag value, which trips Azure's 256-char tag-value limit.
	if len(tags) > 0 {
		args = append(args, "--tags")
		for k, v := range tags {
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create resource group: %w: %s", err, stderr.String())
	}

	return nil
}

// DeleteResourceGroup deletes an Azure resource group and all contained resources
func (a *AROInstaller) DeleteResourceGroup(ctx context.Context, name string) error {
	args := []string{
		"group", "delete",
		"--name", name,
		"--yes", // Skip confirmation
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("delete resource group: %w: %s", err, stderr.String())
	}

	return nil
}

// Version returns the Azure CLI version
func (a *AROInstaller) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "version", "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("az version failed: %w", err)
	}

	return stdout.String(), nil
}

// SaveMetadata saves cluster metadata to a JSON file
func (a *AROInstaller) SaveMetadata(workDir string, metadata map[string]string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "metadata.json"), data, 0644)
}

// LoadMetadata loads cluster metadata from a JSON file
func (a *AROInstaller) LoadMetadata(workDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(workDir, "metadata.json"))
	if err != nil {
		return nil, err
	}

	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}
