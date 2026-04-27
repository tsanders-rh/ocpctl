package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// GCPStorageManager manages GCP storage resources
type GCPStorageManager struct {
	project string
}

// NewGCPStorageManager creates a new GCP storage manager
func NewGCPStorageManager() *GCPStorageManager {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		log.Printf("[GCP Storage] GCP_PROJECT not set")
		return nil
	}

	return &GCPStorageManager{
		project: project,
	}
}

// PersistentDiskConfig represents persistent disk configuration
type PersistentDiskConfig struct {
	Name        string
	Zone        string
	Type        string // pd-standard, pd-balanced, pd-ssd, pd-extreme
	SizeGB      int
	Description string
	Labels      map[string]string
	Snapshot    string // Optional: create from snapshot
}

// GCSBucketConfig represents GCS bucket configuration
type GCSBucketConfig struct {
	Name              string
	Location          string // us-central1, us, eu, asia, etc.
	StorageClass      string // STANDARD, NEARLINE, COLDLINE, ARCHIVE
	Labels            map[string]string
	LifecycleRules    []LifecycleRule
	Versioning        bool
	UniformBucketLevel bool
}

// LifecycleRule represents a GCS bucket lifecycle rule
type LifecycleRule struct {
	Action    LifecycleAction
	Condition LifecycleCondition
}

// LifecycleAction represents a lifecycle action
type LifecycleAction struct {
	Type         string // Delete, SetStorageClass
	StorageClass string // For SetStorageClass action
}

// LifecycleCondition represents lifecycle condition
type LifecycleCondition struct {
	Age              int      // days
	MatchesPrefix    []string
	MatchesSuffix    []string
	NumNewerVersions int
}

// FilestoreConfig represents Filestore instance configuration
type FilestoreConfig struct {
	Name         string
	Tier         string // BASIC_HDD, BASIC_SSD, HIGH_SCALE_SSD, ENTERPRISE
	Capacity     int    // GB, minimum 1024 for BASIC_HDD
	Network      string
	FileShareName string
	Location     string
	Labels       map[string]string
}

// DiskInfo represents persistent disk information
type DiskInfo struct {
	Name        string            `json:"name"`
	Zone        string            `json:"zone"`
	Type        string            `json:"type"`
	SizeGB      int               `json:"sizeGb,string"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"creationTimestamp"`
	Labels      map[string]string `json:"labels"`
	SourceSnapshot string         `json:"sourceSnapshot"`
}

// BucketInfo represents GCS bucket information
type BucketInfo struct {
	Name         string            `json:"name"`
	Location     string            `json:"location"`
	StorageClass string            `json:"storageClass"`
	TimeCreated  time.Time         `json:"timeCreated"`
	Labels       map[string]string `json:"labels"`
}

// SnapshotInfo represents disk snapshot information
type SnapshotInfo struct {
	Name        string    `json:"name"`
	SourceDisk  string    `json:"sourceDisk"`
	SizeGB      int       `json:"diskSizeGb,string"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"creationTimestamp"`
	Description string    `json:"description"`
}

// CreatePersistentDisk creates a persistent disk
func (m *GCPStorageManager) CreatePersistentDisk(ctx context.Context, config *PersistentDiskConfig) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	// Check if disk already exists
	exists, err := m.DiskExists(ctx, config.Name, config.Zone)
	if err != nil {
		return fmt.Errorf("check disk existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Storage] Disk %s already exists in zone %s", config.Name, config.Zone)
		return nil
	}

	args := []string{"compute", "disks", "create", config.Name,
		"--project", m.project,
		"--zone", config.Zone,
		"--type", config.Type,
		"--size", fmt.Sprintf("%dGB", config.SizeGB),
	}

	if config.Description != "" {
		args = append(args, "--description", config.Description)
	}

	if config.Snapshot != "" {
		args = append(args, "--source-snapshot", config.Snapshot)
	}

	if labelsArg := buildLabelsArg(config.Labels); labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create disk: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Created persistent disk: %s (%s, %dGB) in zone %s",
		config.Name, config.Type, config.SizeGB, config.Zone)
	return nil
}

// DeletePersistentDisk deletes a persistent disk
func (m *GCPStorageManager) DeletePersistentDisk(ctx context.Context, name, zone string) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "delete", name,
		"--project", m.project,
		"--zone", zone,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete disk: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Deleted persistent disk: %s in zone %s", name, zone)
	return nil
}

// CreateSnapshot creates a snapshot of a persistent disk
func (m *GCPStorageManager) CreateSnapshot(ctx context.Context, diskName, zone, snapshotName, description string, labels map[string]string) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	args := []string{"compute", "disks", "snapshot", diskName,
		"--project", m.project,
		"--zone", zone,
		"--snapshot-names", snapshotName,
	}

	if description != "" {
		args = append(args, "--description", description)
	}

	if labelsArg := buildLabelsArg(labels); labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Created snapshot: %s from disk %s", snapshotName, diskName)
	return nil
}

// DeleteSnapshot deletes a disk snapshot
func (m *GCPStorageManager) DeleteSnapshot(ctx context.Context, snapshotName string) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	cmd := exec.CommandContext(ctx, "gcloud", "compute", "snapshots", "delete", snapshotName,
		"--project", m.project,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Deleted snapshot: %s", snapshotName)
	return nil
}

// CreateGCSBucket creates a Cloud Storage bucket
func (m *GCPStorageManager) CreateGCSBucket(ctx context.Context, config *GCSBucketConfig) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	// Check if bucket already exists
	exists, err := m.BucketExists(ctx, config.Name)
	if err != nil {
		return fmt.Errorf("check bucket existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Storage] Bucket %s already exists", config.Name)
		return nil
	}

	args := []string{"mb",
		"-p", m.project,
		"-c", config.StorageClass,
		"-l", config.Location,
	}

	if config.UniformBucketLevel {
		args = append(args, "-b", "on")
	}

	args = append(args, fmt.Sprintf("gs://%s", config.Name))

	cmd := exec.CommandContext(ctx, "gsutil", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create bucket: %w\nOutput: %s", err, output)
	}

	// Apply labels
	if len(config.Labels) > 0 {
		if err := m.setBucketLabels(ctx, config.Name, config.Labels); err != nil {
			log.Printf("[GCP Storage] Warning: failed to set bucket labels: %v", err)
		}
	}

	// Enable versioning if requested
	if config.Versioning {
		if err := m.enableBucketVersioning(ctx, config.Name); err != nil {
			log.Printf("[GCP Storage] Warning: failed to enable versioning: %v", err)
		}
	}

	// Apply lifecycle rules
	if len(config.LifecycleRules) > 0 {
		if err := m.setBucketLifecycle(ctx, config.Name, config.LifecycleRules); err != nil {
			log.Printf("[GCP Storage] Warning: failed to set lifecycle rules: %v", err)
		}
	}

	log.Printf("[GCP Storage] Created GCS bucket: %s (%s, %s)", config.Name, config.StorageClass, config.Location)
	return nil
}

// DeleteGCSBucket deletes a Cloud Storage bucket
func (m *GCPStorageManager) DeleteGCSBucket(ctx context.Context, bucketName string, force bool) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	args := []string{"rb"}
	if force {
		// Delete all objects first
		cmd := exec.CommandContext(ctx, "gsutil", "-m", "rm", "-r", fmt.Sprintf("gs://%s/*", bucketName))
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[GCP Storage] Warning: failed to delete bucket contents: %v\nOutput: %s", err, output)
		}
	}

	args = append(args, fmt.Sprintf("gs://%s", bucketName))

	cmd := exec.CommandContext(ctx, "gsutil", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Deleted GCS bucket: %s", bucketName)
	return nil
}

// CreateFilestore creates a Filestore instance
func (m *GCPStorageManager) CreateFilestore(ctx context.Context, config *FilestoreConfig) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	// Check minimum capacity based on tier
	minCapacity := 1024 // GB
	if config.Tier == "BASIC_SSD" {
		minCapacity = 2560
	} else if config.Tier == "HIGH_SCALE_SSD" {
		minCapacity = 10240
	}

	if config.Capacity < minCapacity {
		return fmt.Errorf("capacity %dGB is below minimum %dGB for tier %s", config.Capacity, minCapacity, config.Tier)
	}

	args := []string{"filestore", "instances", "create", config.Name,
		"--project", m.project,
		"--location", config.Location,
		"--tier", config.Tier,
		"--file-share", fmt.Sprintf("name=%s,capacity=%dGB", config.FileShareName, config.Capacity),
		"--network", fmt.Sprintf("name=%s", config.Network),
	}

	if labelsArg := buildLabelsArg(config.Labels); labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create filestore: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Created Filestore instance: %s (%s, %dGB) in %s",
		config.Name, config.Tier, config.Capacity, config.Location)
	return nil
}

// DeleteFilestore deletes a Filestore instance
func (m *GCPStorageManager) DeleteFilestore(ctx context.Context, name, location string) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	cmd := exec.CommandContext(ctx, "gcloud", "filestore", "instances", "delete", name,
		"--project", m.project,
		"--location", location,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete filestore: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Storage] Deleted Filestore instance: %s in %s", name, location)
	return nil
}

// DiskExists checks if a persistent disk exists
func (m *GCPStorageManager) DiskExists(ctx context.Context, name, zone string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "describe", name,
		"--project", m.project,
		"--zone", zone,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(string(output), "was not found") {
			return false, nil
		}
		return false, fmt.Errorf("check disk existence: %w", err)
	}

	return true, nil
}

// BucketExists checks if a GCS bucket exists
func (m *GCPStorageManager) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gsutil", "ls", "-b", fmt.Sprintf("gs://%s", bucketName))
	err := cmd.Run()
	if err != nil {
		if strings.Contains(err.Error(), "BucketNotFoundException") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("check bucket existence: %w", err)
	}

	return true, nil
}

// GetDiskInfo retrieves persistent disk information
func (m *GCPStorageManager) GetDiskInfo(ctx context.Context, name, zone string) (*DiskInfo, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "describe", name,
		"--project", m.project,
		"--zone", zone,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get disk info: %w", err)
	}

	var info DiskInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("parse disk info: %w", err)
	}

	return &info, nil
}

// ListDisksForCluster lists all persistent disks for a cluster
func (m *GCPStorageManager) ListDisksForCluster(ctx context.Context, clusterID string) ([]*DiskInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("GCP storage manager not initialized")
	}

	// List all disks with cluster-id label
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "list",
		"--project", m.project,
		"--filter", fmt.Sprintf("labels.cluster-id=%s", sanitizeGCPLabel(clusterID)),
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list cluster disks: %w", err)
	}

	var disks []*DiskInfo
	if err := json.Unmarshal(output, &disks); err != nil {
		return nil, fmt.Errorf("parse disk list: %w", err)
	}

	return disks, nil
}

// ListSnapshotsForCluster lists all snapshots for a cluster
func (m *GCPStorageManager) ListSnapshotsForCluster(ctx context.Context, clusterID string) ([]*SnapshotInfo, error) {
	if m == nil {
		return nil, fmt.Errorf("GCP storage manager not initialized")
	}

	cmd := exec.CommandContext(ctx, "gcloud", "compute", "snapshots", "list",
		"--project", m.project,
		"--filter", fmt.Sprintf("labels.cluster-id=%s", sanitizeGCPLabel(clusterID)),
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list cluster snapshots: %w", err)
	}

	var snapshots []*SnapshotInfo
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("parse snapshot list: %w", err)
	}

	return snapshots, nil
}

// SetupClusterStorage creates storage resources for a cluster
func (m *GCPStorageManager) SetupClusterStorage(ctx context.Context, cluster *types.Cluster, bucketName string) error {
	if m == nil {
		return fmt.Errorf("GCP storage manager not initialized")
	}

	// Build labels for resources
	labels := map[string]string{
		"managed-by":   "ocpctl",
		"cluster-id":   sanitizeGCPLabel(cluster.ID),
		"cluster-name": sanitizeGCPLabel(cluster.Name),
		"profile":      sanitizeGCPLabel(cluster.Profile),
	}

	// Create GCS bucket for cluster artifacts
	bucketConfig := &GCSBucketConfig{
		Name:         bucketName,
		Location:     cluster.Region,
		StorageClass: "STANDARD",
		Labels:       labels,
		Versioning:   true,
		LifecycleRules: []LifecycleRule{
			{
				Action: LifecycleAction{
					Type: "Delete",
				},
				Condition: LifecycleCondition{
					Age: 90, // Delete objects older than 90 days
				},
			},
		},
	}

	if err := m.CreateGCSBucket(ctx, bucketConfig); err != nil {
		return fmt.Errorf("create GCS bucket: %w", err)
	}

	log.Printf("[GCP Storage] Completed storage setup for cluster %s", cluster.Name)
	return nil
}

// Helper functions

func (m *GCPStorageManager) setBucketLabels(ctx context.Context, bucketName string, labels map[string]string) error {
	// Convert labels to JSON
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	cmd := exec.CommandContext(ctx, "gsutil", "label", "set", "-",
		fmt.Sprintf("gs://%s", bucketName))
	cmd.Stdin = strings.NewReader(string(labelsJSON))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("set bucket labels: %w\nOutput: %s", err, output)
	}

	return nil
}

func (m *GCPStorageManager) enableBucketVersioning(ctx context.Context, bucketName string) error {
	cmd := exec.CommandContext(ctx, "gsutil", "versioning", "set", "on",
		fmt.Sprintf("gs://%s", bucketName))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable versioning: %w\nOutput: %s", err, output)
	}

	return nil
}

func (m *GCPStorageManager) setBucketLifecycle(ctx context.Context, bucketName string, rules []LifecycleRule) error {
	// Convert lifecycle rules to JSON format expected by gsutil
	type lifecycleJSON struct {
		Lifecycle struct {
			Rule []map[string]interface{} `json:"rule"`
		} `json:"lifecycle"`
	}

	var lc lifecycleJSON
	for _, rule := range rules {
		ruleMap := make(map[string]interface{})

		// Action
		actionMap := map[string]string{
			"type": rule.Action.Type,
		}
		if rule.Action.StorageClass != "" {
			actionMap["storageClass"] = rule.Action.StorageClass
		}
		ruleMap["action"] = actionMap

		// Condition
		condMap := make(map[string]interface{})
		if rule.Condition.Age > 0 {
			condMap["age"] = rule.Condition.Age
		}
		if len(rule.Condition.MatchesPrefix) > 0 {
			condMap["matchesPrefix"] = rule.Condition.MatchesPrefix
		}
		if len(rule.Condition.MatchesSuffix) > 0 {
			condMap["matchesSuffix"] = rule.Condition.MatchesSuffix
		}
		if rule.Condition.NumNewerVersions > 0 {
			condMap["numNewerVersions"] = rule.Condition.NumNewerVersions
		}
		ruleMap["condition"] = condMap

		lc.Lifecycle.Rule = append(lc.Lifecycle.Rule, ruleMap)
	}

	lifecycleData, err := json.Marshal(lc)
	if err != nil {
		return fmt.Errorf("marshal lifecycle rules: %w", err)
	}

	cmd := exec.CommandContext(ctx, "gsutil", "lifecycle", "set", "-",
		fmt.Sprintf("gs://%s", bucketName))
	cmd.Stdin = strings.NewReader(string(lifecycleData))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("set lifecycle rules: %w\nOutput: %s", err, output)
	}

	return nil
}

func buildLabelsArg(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	var pairs []string
	for k, v := range labels {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, ",")
}

func sanitizeGCPLabel(s string) string {
	if s == "" {
		return ""
	}

	s = strings.ToLower(s)
	var result strings.Builder
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}

		if i == 0 && !((r >= 'a' && r <= 'z')) {
			temp := result.String()
			result.Reset()
			result.WriteString("x")
			result.WriteString(temp)
		}
	}

	sanitized := result.String()
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return strings.TrimRight(sanitized, "-_")
}
