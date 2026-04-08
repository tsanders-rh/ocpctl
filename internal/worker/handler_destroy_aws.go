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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleAWSDestroy handles AWS-specific cluster cleanup including CCO IAM roles, OIDC provider, and Route53 hosted zone.
// This should be called AFTER openshift-install destroy cluster completes to clean up resources created by ccoctl.
// Uses the infrastructure ID from metadata.json to identify and delete AWS resources.
// If metadata.json is missing, falls back to direct AWS SDK cleanup using ClusterName tags.
func (h *DestroyHandler) HandleAWSDestroy(ctx context.Context, cluster *types.Cluster, inst *installer.Installer, workDir string) error {
	log.Printf("AWS cluster cleanup: cleaning up CCO IAM roles and OIDC provider for %s", cluster.Name)

	// Extract infraID from metadata.json
	// ccoctl uses the infraID (not cluster name) to identify resources
	infraID, err := h.getInfraID(workDir)
	usedFallback := false

	if err != nil {
		log.Printf("Warning: could not extract infraID from metadata.json: %v", err)
		log.Printf("Will use direct AWS SDK cleanup as fallback")
		usedFallback = true
	} else {
		log.Printf("Using infraID from metadata.json: %s", infraID)
	}

	// Try ccoctl cleanup if we have infraID
	ccoctlSuccess := false
	if !usedFallback {
		// Run ccoctl aws delete to clean up IAM roles and OIDC provider
		// ccoctl aws delete --name <infra-id> --region <region>
		cmdCtx, cancel := context.WithTimeout(ctx, DNSCleanupTimeout)
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
				ccoctlSuccess = true
			} else {
				// ccoctl failed - will try fallback cleanup
				log.Printf("Warning: ccoctl aws delete failed for %s: %v", cluster.Name, err)
				log.Printf("ccoctl output:\n%s", outputStr)
				log.Printf("Will attempt fallback IAM cleanup using ClusterName tags")
				usedFallback = true
			}
		} else {
			log.Printf("Successfully cleaned up AWS CCO resources for %s using ccoctl", cluster.Name)
			log.Printf("ccoctl output:\n%s", outputStr)
			ccoctlSuccess = true
		}
	}

	// If ccoctl failed or metadata.json was missing, use fallback cleanup
	fallbackCleanupSuccess := false
	if usedFallback && !ccoctlSuccess {
		log.Printf("Attempting direct AWS SDK cleanup for cluster %s", cluster.Name)
		if err := h.cleanupIAMResourcesByClusterName(ctx, cluster, infraID); err != nil {
			log.Printf("Warning: fallback IAM cleanup encountered errors: %v", err)
			// Track failure but continue with Route53 cleanup
			fallbackCleanupSuccess = false
		} else {
			log.Printf("Successfully cleaned up IAM resources using fallback method")
			fallbackCleanupSuccess = true
		}
	}

	// Clean up Route53 hosted zone
	route53Success := false
	if err := h.deleteRoute53HostedZone(ctx, cluster); err != nil {
		log.Printf("Warning: failed to delete Route53 hosted zone: %v", err)
		route53Success = false
	} else {
		route53Success = true
	}

	// Determine overall success: ccoctl succeeded OR fallback cleanup succeeded
	// Route53 cleanup is optional (zone might not exist)
	overallSuccess := ccoctlSuccess || fallbackCleanupSuccess

	if !overallSuccess {
		return fmt.Errorf("AWS cleanup failed: ccoctl failed and fallback cleanup failed")
	}

	if !route53Success {
		log.Printf("Warning: Route53 cleanup failed, but IAM cleanup succeeded - marking as success")
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
	// Skip Route53 cleanup for clusters without base domain (EKS/IKS)
	if cluster.BaseDomain == nil || *cluster.BaseDomain == "" {
		log.Printf("Skipping Route53 cleanup - no base domain for cluster %s", cluster.Name)
		return nil
	}

	// Construct the domain name for the cluster
	// Format: <cluster-name>.<base-domain>
	zoneName := fmt.Sprintf("%s.%s.", cluster.Name, *cluster.BaseDomain)

	log.Printf("Looking for Route53 hosted zone: %s", zoneName)

	// Find the hosted zone ID
	cmdCtx, cancel := context.WithTimeout(ctx, AWSCommandTimeout)
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
	cmdCtx, cancel = context.WithTimeout(ctx, AWSCommandTimeout)
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

		cmdCtx, cancel := context.WithTimeout(ctx, AWSCommandTimeout)
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
		time.Sleep(CleanupRetryDelay)
	}

	// Delete the hosted zone
	log.Printf("Deleting hosted zone: %s (ID: %s)", zoneName, zoneID)
	cmdCtx, cancel = context.WithTimeout(ctx, AWSCommandTimeout)
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

// cleanupIAMResourcesByClusterName finds and deletes IAM roles and OIDC providers by ClusterName tag or infraID pattern.
// This is a fallback method used when metadata.json is missing or ccoctl cleanup fails.
// First tries tag-based cleanup, then falls back to name pattern matching if no tagged resources found.
// Only deletes resources tagged with ManagedBy=ocpctl OR matching infraID/cluster name patterns to avoid deleting user resources.
func (h *DestroyHandler) cleanupIAMResourcesByClusterName(ctx context.Context, cluster *types.Cluster, infraID string) error {
	// Load AWS SDK config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Track cleanup results
	deletedRoles := 0
	deletedProviders := 0
	errors := []string{}

	// Find and delete IAM roles
	log.Printf("Searching for IAM roles with ClusterName=%s and ManagedBy=ocpctl", cluster.Name)
	if infraID != "" {
		log.Printf("Will also search by name pattern: %s-*", infraID)
	}

	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			errors = append(errors, fmt.Sprintf("list roles: %v", err))
			break
		}

		for _, role := range page.Roles {
			roleName := aws.ToString(role.RoleName)

			// Get role tags
			tagsResult, err := iamClient.ListRoleTags(ctx, &iam.ListRoleTagsInput{
				RoleName: role.RoleName,
			})
			if err != nil {
				log.Printf("Warning: failed to get tags for role %s: %v", roleName, err)
				continue
			}

			// Check if this role belongs to our cluster via tags
			var matchesCluster bool
			var managedByOcpctl bool

			for _, tag := range tagsResult.Tags {
				key := aws.ToString(tag.Key)
				value := aws.ToString(tag.Value)

				if key == "ClusterName" && value == cluster.Name {
					matchesCluster = true
				}
				if key == "ManagedBy" && (value == "ocpctl" || value == "cluster-control-plane") {
					managedByOcpctl = true
				}
			}

			// If tags match, delete the role
			if matchesCluster && managedByOcpctl {
				log.Printf("Deleting IAM role (matched by tag): %s", roleName)

				// Detach all policies before deleting role
				if err := h.detachRolePolicies(ctx, iamClient, roleName); err != nil {
					errors = append(errors, fmt.Sprintf("detach policies from %s: %v", roleName, err))
					continue
				}

				// Delete the role
				_, err := iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
					RoleName: role.RoleName,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("delete role %s: %v", roleName, err))
				} else {
					deletedRoles++
					log.Printf("Deleted IAM role: %s", roleName)
				}
				continue
			}

			// Fallback: If no tags matched, try name pattern matching
			// This handles cases where tagging failed during cluster creation
			matchesByName := false
			if infraID != "" && strings.HasPrefix(roleName, infraID+"-") {
				// Match by infraID prefix (most reliable)
				matchesByName = true
				log.Printf("Role %s matches by infraID pattern", roleName)
			} else if strings.Contains(roleName, cluster.Name) &&
				      (strings.Contains(roleName, "openshift-") || strings.Contains(roleName, "ocpctl-")) {
				// Match by cluster name + openshift pattern (less reliable, more cautious)
				matchesByName = true
				log.Printf("Role %s matches by cluster name pattern", roleName)
			}

			if matchesByName {
				log.Printf("Deleting IAM role (matched by name): %s", roleName)

				// Detach all policies before deleting role
				if err := h.detachRolePolicies(ctx, iamClient, roleName); err != nil {
					errors = append(errors, fmt.Sprintf("detach policies from %s: %v", roleName, err))
					continue
				}

				// Delete the role
				_, err := iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
					RoleName: role.RoleName,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("delete role %s: %v", roleName, err))
				} else {
					deletedRoles++
					log.Printf("Deleted IAM role: %s", roleName)
				}
			}
		}
	}

	// Find and delete OIDC providers
	log.Printf("Searching for OIDC providers with ClusterName=%s", cluster.Name)
	if infraID != "" {
		log.Printf("Will also search OIDC providers by name pattern: %s-oidc", infraID)
	}

	providersResult, err := iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		errors = append(errors, fmt.Sprintf("list OIDC providers: %v", err))
	} else {
		for _, provider := range providersResult.OpenIDConnectProviderList {
			providerArn := aws.ToString(provider.Arn)

			// Get provider details and tags
			detailsResult, err := iamClient.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: provider.Arn,
			})
			if err != nil {
				log.Printf("Warning: failed to get OIDC provider details for %s: %v", providerArn, err)
				continue
			}

			// Check if this provider belongs to our cluster via tags
			var matchesCluster bool
			for _, tag := range detailsResult.Tags {
				if aws.ToString(tag.Key) == "ClusterName" && aws.ToString(tag.Value) == cluster.Name {
					matchesCluster = true
					break
				}
			}

			if matchesCluster {
				log.Printf("Deleting OIDC provider (matched by tag): %s", providerArn)

				_, err := iamClient.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
					OpenIDConnectProviderArn: provider.Arn,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("delete OIDC provider %s: %v", providerArn, err))
				} else {
					deletedProviders++
					log.Printf("Deleted OIDC provider: %s", providerArn)
				}
				continue
			}

			// Fallback: If no tags matched, try name pattern matching
			// OIDC providers for OpenShift have format: arn:aws:iam::ACCOUNT:oidc-provider/INFRAID-oidc.s3.REGION.amazonaws.com
			matchesByName := false
			if infraID != "" && strings.Contains(providerArn, infraID+"-oidc") {
				// Match by infraID-oidc pattern
				matchesByName = true
				log.Printf("OIDC provider %s matches by infraID pattern", providerArn)
			} else if strings.Contains(providerArn, cluster.Name) && strings.Contains(providerArn, "-oidc") {
				// Match by cluster name + oidc pattern
				matchesByName = true
				log.Printf("OIDC provider %s matches by cluster name pattern", providerArn)
			}

			if matchesByName {
				log.Printf("Deleting OIDC provider (matched by name): %s", providerArn)

				_, err := iamClient.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
					OpenIDConnectProviderArn: provider.Arn,
				})
				if err != nil {
					errors = append(errors, fmt.Sprintf("delete OIDC provider %s: %v", providerArn, err))
				} else {
					deletedProviders++
					log.Printf("Deleted OIDC provider: %s", providerArn)
				}
			}
		}
	}

	// Clean up Windows IRSA role if it exists (created during Windows VM post-config)
	// Pattern: ocpctl-win-s3-{cluster-id}
	windowsRoleName := fmt.Sprintf("ocpctl-win-s3-%s", cluster.ID)
	log.Printf("Checking for Windows IRSA role: %s", windowsRoleName)

	_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(windowsRoleName),
	})
	if err == nil {
		// Role exists, delete it
		log.Printf("Deleting Windows IRSA role: %s", windowsRoleName)

		// Detach all policies before deleting role
		if err := h.detachRolePolicies(ctx, iamClient, windowsRoleName); err != nil {
			errors = append(errors, fmt.Sprintf("detach policies from Windows role %s: %v", windowsRoleName, err))
		} else {
			_, err := iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
				RoleName: aws.String(windowsRoleName),
			})
			if err != nil {
				errors = append(errors, fmt.Sprintf("delete Windows role %s: %v", windowsRoleName, err))
			} else {
				deletedRoles++
				log.Printf("Deleted Windows IRSA role: %s", windowsRoleName)
			}
		}
	} else {
		// Role doesn't exist or error checking - this is OK
		log.Printf("Windows IRSA role not found (cluster may not have used Windows VMs)")
	}

	log.Printf("IAM cleanup summary: deleted %d roles, %d OIDC providers", deletedRoles, deletedProviders)

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during cleanup: %v", len(errors), strings.Join(errors, "; "))
	}

	return nil
}

// detachRolePolicies detaches all managed and inline policies from an IAM role
func (h *DestroyHandler) detachRolePolicies(ctx context.Context, iamClient *iam.Client, roleName string) error {
	// Detach managed policies
	attachedPolicies, err := iamClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("list attached policies: %w", err)
	}

	for _, policy := range attachedPolicies.AttachedPolicies {
		_, err := iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: policy.PolicyArn,
		})
		if err != nil {
			return fmt.Errorf("detach policy %s: %w", aws.ToString(policy.PolicyArn), err)
		}
		log.Printf("Detached policy %s from role %s", aws.ToString(policy.PolicyName), roleName)
	}

	// Delete inline policies
	inlinePolicies, err := iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("list inline policies: %w", err)
	}

	for _, policyName := range inlinePolicies.PolicyNames {
		_, err := iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(policyName),
		})
		if err != nil {
			return fmt.Errorf("delete inline policy %s: %w", policyName, err)
		}
		log.Printf("Deleted inline policy %s from role %s", policyName, roleName)
	}

	return nil
}
