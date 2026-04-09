package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AWSCleanupManifest tracks exact AWS resources created during cluster installation.
// This manifest is persisted during create and used during destroy to avoid discovery.
// Key benefit: Direct deletion by name/ARN instead of scanning entire AWS account.
type AWSCleanupManifest struct {
	ClusterName         string   `json:"clusterName"`
	InfraID             string   `json:"infraID"`
	Region              string   `json:"region"`
	IAMRoles            []string `json:"iamRoles,omitempty"`
	InstanceProfiles    []string `json:"instanceProfiles,omitempty"`
	OIDCProviderArn     string   `json:"oidcProviderArn,omitempty"`
	WindowsIRSARole     string   `json:"windowsIRSARole,omitempty"`
	Route53HostedZoneID string   `json:"route53HostedZoneId,omitempty"`
}

// awsCleanupManifestPath returns the path to the cleanup manifest file
func awsCleanupManifestPath(workDir string) string {
	return filepath.Join(workDir, "aws-cleanup-manifest.json")
}

// LoadAWSCleanupManifest loads the cleanup manifest from the cluster work directory.
// Returns an empty manifest if the file doesn't exist (not an error - allows fallback).
func LoadAWSCleanupManifest(workDir string) (*AWSCleanupManifest, error) {
	path := awsCleanupManifestPath(workDir)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty manifest - not an error, just means we need fallback
			return &AWSCleanupManifest{}, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m AWSCleanupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &m, nil
}

// SaveAWSCleanupManifest writes the cleanup manifest to the cluster work directory
func SaveAWSCleanupManifest(workDir string, m *AWSCleanupManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	path := awsCleanupManifestPath(workDir)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}

// RecordAWSInfraID initializes the manifest with cluster metadata
func RecordAWSInfraID(workDir, clusterName, infraID, region string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.ClusterName = clusterName
	m.InfraID = infraID
	m.Region = region

	return SaveAWSCleanupManifest(workDir, m)
}

// RecordAWSIAMRole adds an IAM role name to the cleanup manifest
func RecordAWSIAMRole(workDir, roleName string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.IAMRoles = appendUnique(m.IAMRoles, roleName)

	return SaveAWSCleanupManifest(workDir, m)
}

// RecordAWSInstanceProfile adds an instance profile name to the cleanup manifest
func RecordAWSInstanceProfile(workDir, profileName string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.InstanceProfiles = appendUnique(m.InstanceProfiles, profileName)

	return SaveAWSCleanupManifest(workDir, m)
}

// RecordAWSOIDCProvider sets the OIDC provider ARN in the cleanup manifest
func RecordAWSOIDCProvider(workDir, arn string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.OIDCProviderArn = arn

	return SaveAWSCleanupManifest(workDir, m)
}

// RecordAWSWindowsIRSARole sets the Windows IRSA role name in the cleanup manifest
func RecordAWSWindowsIRSARole(workDir, roleName string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.WindowsIRSARole = roleName

	return SaveAWSCleanupManifest(workDir, m)
}

// RecordAWSRoute53HostedZone sets the Route53 hosted zone ID in the cleanup manifest
func RecordAWSRoute53HostedZone(workDir, zoneID string) error {
	m, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return err
	}

	m.Route53HostedZoneID = zoneID

	return SaveAWSCleanupManifest(workDir, m)
}

// appendUnique appends a string to a slice only if it's not already present
func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}

// IsManifestEmpty returns true if the manifest has no recorded resources
func (m *AWSCleanupManifest) IsManifestEmpty() bool {
	return len(m.IAMRoles) == 0 &&
		len(m.InstanceProfiles) == 0 &&
		m.OIDCProviderArn == "" &&
		m.WindowsIRSARole == "" &&
		m.Route53HostedZoneID == ""
}
