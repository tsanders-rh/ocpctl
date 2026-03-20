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

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleAWSDestroy handles AWS-specific cluster cleanup
// This should be called AFTER openshift-install destroy cluster completes
func (h *DestroyHandler) HandleAWSDestroy(ctx context.Context, cluster *types.Cluster, inst *installer.Installer, workDir string) error {
	log.Printf("AWS cluster cleanup: cleaning up CCO IAM roles and OIDC provider for %s", cluster.Name)

	// Extract infraID from metadata.json
	// ccoctl uses the infraID (not cluster name) to identify resources
	infraID, err := h.getInfraID(workDir)
	if err != nil {
		log.Printf("Warning: could not extract infraID from metadata.json: %v", err)
		log.Printf("Attempting cleanup with cluster name as fallback (may not find resources)")
		infraID = cluster.Name
	} else {
		log.Printf("Using infraID from metadata.json: %s", infraID)
	}

	// Run ccoctl aws delete to clean up IAM roles and OIDC provider
	// ccoctl aws delete --name <infra-id> --region <region>
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "ccoctl", "aws", "delete",
		"--name", infraID,
		"--region", cluster.Region,
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		// Check if resources were already deleted (not an error)
		if strings.Contains(outputStr, "NoSuchEntity") ||
			strings.Contains(outputStr, "not found") ||
			strings.Contains(outputStr, "does not exist") {
			log.Printf("CCO resources for %s already deleted or not found", cluster.Name)
			return nil
		}

		// Log the error but don't fail - resources might be partially deleted
		log.Printf("Warning: ccoctl aws delete encountered errors for %s: %v", cluster.Name, err)
		log.Printf("ccoctl output:\n%s", outputStr)

		// Return error for visibility but don't fail the destroy job
		return fmt.Errorf("ccoctl aws delete: %w\nOutput: %s", err, outputStr)
	}

	log.Printf("Successfully cleaned up AWS CCO resources for %s", cluster.Name)
	log.Printf("ccoctl output:\n%s", outputStr)

	// Clean up Route53 hosted zone
	if err := h.deleteRoute53HostedZone(ctx, cluster); err != nil {
		log.Printf("Warning: failed to delete Route53 hosted zone: %v", err)
		// Don't fail the destroy job - log and continue
	}

	return nil
}

// getInfraID extracts the infrastructure ID from metadata.json
func (h *DestroyHandler) getInfraID(workDir string) (string, error) {
	metadataPath := filepath.Join(workDir, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", fmt.Errorf("read metadata.json: %w", err)
	}

	var metadata struct {
		InfraID string `json:"infraID"`
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse metadata.json: %w", err)
	}

	if metadata.InfraID == "" {
		return "", fmt.Errorf("infraID not found in metadata.json")
	}

	return metadata.InfraID, nil
}

// deleteRoute53HostedZone deletes the Route53 hosted zone for the cluster
func (h *DestroyHandler) deleteRoute53HostedZone(ctx context.Context, cluster *types.Cluster) error {
	// Construct the domain name for the cluster
	// Format: <cluster-name>.<base-domain>
	zoneName := fmt.Sprintf("%s.%s.", cluster.Name, cluster.BaseDomain)

	log.Printf("Looking for Route53 hosted zone: %s", zoneName)

	// Find the hosted zone ID
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "aws", "route53", "list-hosted-zones",
		"--query", fmt.Sprintf("HostedZones[?Name=='%s'].Id", zoneName),
		"--output", "text",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("list hosted zones: %w\nOutput: %s", err, string(output))
	}

	zoneID := strings.TrimSpace(string(output))
	if zoneID == "" {
		log.Printf("No Route53 hosted zone found for %s (already deleted or never created)", zoneName)
		return nil
	}

	// Route53 returns zone IDs with /hostedzone/ prefix, extract just the ID
	zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")
	log.Printf("Found hosted zone ID: %s", zoneID)

	// List all resource record sets
	cmdCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd = exec.CommandContext(cmdCtx, "aws", "route53", "list-resource-record-sets",
		"--hosted-zone-id", zoneID,
		"--output", "json",
	)

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("list resource record sets: %w\nOutput: %s", err, string(output))
	}

	// Parse the resource record sets
	var recordSets struct {
		ResourceRecordSets []struct {
			Name string `json:"Name"`
			Type string `json:"Type"`
		} `json:"ResourceRecordSets"`
	}

	if err := json.Unmarshal(output, &recordSets); err != nil {
		return fmt.Errorf("parse record sets: %w", err)
	}

	// Delete all records except NS and SOA (required records for the zone)
	deletedCount := 0
	for _, record := range recordSets.ResourceRecordSets {
		if record.Type == "NS" || record.Type == "SOA" {
			// Skip NS and SOA records - these are managed by Route53
			continue
		}

		log.Printf("Deleting DNS record: %s (%s)", record.Name, record.Type)

		// Delete the record using change-resource-record-sets
		changeBatch := fmt.Sprintf(`{
			"Changes": [{
				"Action": "DELETE",
				"ResourceRecordSet": %s
			}]
		}`, getRecordSetJSON(output, record.Name, record.Type))

		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, "aws", "route53", "change-resource-record-sets",
			"--hosted-zone-id", zoneID,
			"--change-batch", changeBatch,
		)

		if deleteOutput, deleteErr := cmd.CombinedOutput(); deleteErr != nil {
			log.Printf("Warning: failed to delete record %s: %v\nOutput: %s", record.Name, deleteErr, string(deleteOutput))
		} else {
			deletedCount++
		}
	}

	if deletedCount > 0 {
		log.Printf("Deleted %d DNS records from zone %s", deletedCount, zoneName)
		// Wait a moment for DNS propagation
		time.Sleep(2 * time.Second)
	}

	// Delete the hosted zone
	log.Printf("Deleting hosted zone: %s (ID: %s)", zoneName, zoneID)
	cmdCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd = exec.CommandContext(cmdCtx, "aws", "route53", "delete-hosted-zone",
		"--id", zoneID,
	)

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete hosted zone: %w\nOutput: %s", err, string(output))
	}

	log.Printf("Successfully deleted Route53 hosted zone %s", zoneName)
	return nil
}

// getRecordSetJSON extracts a single record set from the JSON output
func getRecordSetJSON(jsonOutput []byte, name, recordType string) string {
	var data struct {
		ResourceRecordSets []json.RawMessage `json:"ResourceRecordSets"`
	}

	if err := json.Unmarshal(jsonOutput, &data); err != nil {
		return "{}"
	}

	for _, rawRecord := range data.ResourceRecordSets {
		var record struct {
			Name string `json:"Name"`
			Type string `json:"Type"`
		}

		if err := json.Unmarshal(rawRecord, &record); err != nil {
			continue
		}

		if record.Name == name && record.Type == recordType {
			return string(rawRecord)
		}
	}

	return "{}"
}
