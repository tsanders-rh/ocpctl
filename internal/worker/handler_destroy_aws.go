package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

const (
	iamDeleteWorkers         = 6
	route53ChangeBatchSize   = 100
	awsRetryAttempts         = 4
	awsRetryInitialBackoff   = 500 * time.Millisecond
	route53ChangePollDelay   = 2 * time.Second
	route53ChangePollTimeout = 2 * time.Minute
)

// HandleAWSDestroy handles AWS-specific cluster cleanup including CCO IAM roles, OIDC provider, and Route53 hosted zone.
// This should be called AFTER openshift-install destroy cluster completes to clean up resources created by ccoctl.
// Cleanup strategy (in order of preference):
// 1. ccoctl aws delete (fastest, uses infraID)
// 2. Manifest-driven cleanup (uses exact resource names/ARNs recorded during create)
// 3. Discovery-based fallback (scans AWS account for matching resources - slowest)
func (h *DestroyHandler) HandleAWSDestroy(ctx context.Context, cluster *types.Cluster, inst *installer.Installer, workDir string) error {
	log.Printf("AWS cluster cleanup: cleaning up CCO IAM roles, OIDC provider, and Route53 for %s", cluster.Name)

	infraID, infraErr := h.getInfraID(workDir)
	usedFallback := false

	if infraErr != nil {
		log.Printf("Warning: could not extract infraID from metadata.json: %v", infraErr)
		log.Printf("Will use fallback AWS SDK cleanup where possible")
		usedFallback = true
	} else {
		log.Printf("Using infraID from metadata.json: %s", infraID)
	}

	// Try ccoctl cleanup first (fastest path)
	ccoctlSuccess := false
	if !usedFallback {
		if err := h.runCCOCTLAWSDelete(ctx, infraID, cluster.Region); err != nil {
			log.Printf("Warning: ccoctl aws delete failed for %s: %v", cluster.Name, err)
			log.Printf("Will attempt manifest-driven or discovery-based cleanup")
			usedFallback = true
		} else {
			log.Printf("Successfully cleaned up AWS CCO resources for %s using ccoctl", cluster.Name)
			ccoctlSuccess = true
		}
	}

	// If ccoctl failed, try manifest-driven cleanup (much faster than discovery)
	manifestCleanupSuccess := false
	if usedFallback && !ccoctlSuccess {
		log.Printf("Attempting manifest-driven cleanup for cluster %s", cluster.Name)
		if err := h.cleanupFromManifest(ctx, cluster, workDir); err != nil {
			log.Printf("Warning: manifest-driven cleanup failed or incomplete: %v", err)
			log.Printf("Will attempt discovery-based fallback")
		} else {
			log.Printf("Successfully cleaned up AWS resources using manifest")
			manifestCleanupSuccess = true
		}
	}

	// If both ccoctl and manifest failed, fall back to discovery-based cleanup (slowest)
	discoveryCleanupSuccess := false
	if usedFallback && !ccoctlSuccess && !manifestCleanupSuccess {
		log.Printf("Attempting discovery-based AWS SDK cleanup for cluster %s", cluster.Name)
		if err := h.cleanupIAMResourcesByClusterName(ctx, cluster, infraID); err != nil {
			log.Printf("Warning: discovery-based IAM cleanup encountered errors: %v", err)
		} else {
			log.Printf("Successfully cleaned up IAM resources using discovery-based method")
			discoveryCleanupSuccess = true
		}
	}

	// Route53 cleanup (uses manifest if available, otherwise discovery)
	route53Success := false
	if err := h.deleteRoute53HostedZone(ctx, cluster, workDir); err != nil {
		log.Printf("Warning: failed to delete Route53 hosted zone: %v", err)
	} else {
		route53Success = true
	}

	overallSuccess := ccoctlSuccess || manifestCleanupSuccess || discoveryCleanupSuccess
	if !overallSuccess {
		return fmt.Errorf("AWS cleanup failed: all cleanup methods failed")
	}

	if !route53Success {
		log.Printf("Warning: Route53 cleanup failed, but IAM cleanup succeeded - marking overall cleanup as success")
	}

	return nil
}

// cleanupFromManifest deletes AWS resources using exact names/ARNs from the cleanup manifest.
// This is much faster than discovery because it deletes resources directly without scanning.
func (h *DestroyHandler) cleanupFromManifest(ctx context.Context, cluster *types.Cluster, workDir string) error {
	manifest, err := LoadAWSCleanupManifest(workDir)
	if err != nil {
		return fmt.Errorf("load cleanup manifest: %w", err)
	}

	if manifest.IsManifestEmpty() {
		return fmt.Errorf("cleanup manifest is empty - falling back to discovery")
	}

	log.Printf("Found cleanup manifest with %d IAM roles, OIDC provider: %v, Windows IRSA: %v, Route53 zone: %v",
		len(manifest.IAMRoles),
		manifest.OIDCProviderArn != "",
		manifest.WindowsIRSARole != "",
		manifest.Route53HostedZoneID != "")

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	var errs []string
	deletedCount := 0

	// Delete instance profiles first (prevents role deletion failures)
	for _, profileName := range manifest.InstanceProfiles {
		log.Printf("Deleting instance profile from manifest: %s", profileName)
		err := retryAWS(ctx, "delete instance profile", func(callCtx context.Context) error {
			_, err := iamClient.DeleteInstanceProfile(callCtx, &iam.DeleteInstanceProfileInput{
				InstanceProfileName: aws.String(profileName),
			})
			return err
		})
		if err != nil && !isNoSuchEntityError(err) {
			errs = append(errs, fmt.Sprintf("delete instance profile %s: %v", profileName, err))
		} else if err == nil {
			deletedCount++
			log.Printf("Deleted instance profile: %s", profileName)
		}
	}

	// Delete IAM roles recorded in manifest
	for _, roleName := range manifest.IAMRoles {
		log.Printf("Deleting IAM role from manifest: %s", roleName)
		if err := h.deleteSingleIAMRole(ctx, iamClient, roleName); err != nil && !isNoSuchEntityError(err) {
			errs = append(errs, fmt.Sprintf("delete role %s: %v", roleName, err))
		} else if err == nil {
			deletedCount++
			log.Printf("Deleted IAM role: %s", roleName)
		}
	}

	// Delete Windows IRSA role if recorded
	if manifest.WindowsIRSARole != "" {
		log.Printf("Deleting Windows IRSA role from manifest: %s", manifest.WindowsIRSARole)
		if err := h.deleteSingleIAMRole(ctx, iamClient, manifest.WindowsIRSARole); err != nil && !isNoSuchEntityError(err) {
			errs = append(errs, fmt.Sprintf("delete Windows IRSA role %s: %v", manifest.WindowsIRSARole, err))
		} else if err == nil {
			deletedCount++
			log.Printf("Deleted Windows IRSA role: %s", manifest.WindowsIRSARole)
		}
	}

	// Delete OIDC provider if recorded
	if manifest.OIDCProviderArn != "" {
		log.Printf("Deleting OIDC provider from manifest: %s", manifest.OIDCProviderArn)
		err := retryAWS(ctx, "delete OIDC provider", func(callCtx context.Context) error {
			_, err := iamClient.DeleteOpenIDConnectProvider(callCtx, &iam.DeleteOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: aws.String(manifest.OIDCProviderArn),
			})
			return err
		})
		if err != nil && !isNoSuchEntityError(err) {
			errs = append(errs, fmt.Sprintf("delete OIDC provider %s: %v", manifest.OIDCProviderArn, err))
		} else if err == nil {
			deletedCount++
			log.Printf("Deleted OIDC provider: %s", manifest.OIDCProviderArn)
		}
	}

	log.Printf("Manifest-driven cleanup: deleted %d resources, encountered %d errors", deletedCount, len(errs))

	if len(errs) > 0 {
		return fmt.Errorf("cleanup completed with %d errors: %s", len(errs), strings.Join(errs, "; "))
	}

	return nil
}

func (h *DestroyHandler) runCCOCTLAWSDelete(ctx context.Context, infraID, region string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, DNSCleanupTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "ccoctl", "aws", "delete",
		"--name", infraID,
		"--region", region,
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		if strings.Contains(outputStr, "NoSuchEntity") ||
			strings.Contains(outputStr, "not found") ||
			strings.Contains(outputStr, "does not exist") {
			log.Printf("CCO resources already deleted or not found")
			return nil
		}
		return fmt.Errorf("ccoctl aws delete failed: %w\noutput:\n%s", err, outputStr)
	}

	log.Printf("ccoctl output:\n%s", outputStr)
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

// deleteRoute53HostedZone deletes the Route53 hosted zone for the cluster using the AWS SDK.
// It batches record deletions instead of issuing one CLI call per record.
// Uses manifest zone ID if available, otherwise discovers by zone name.
func (h *DestroyHandler) deleteRoute53HostedZone(ctx context.Context, cluster *types.Cluster, workDir string) error {
	if cluster.BaseDomain == nil || *cluster.BaseDomain == "" {
		log.Printf("Skipping Route53 cleanup - no base domain for cluster %s", cluster.Name)
		return nil
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	r53 := route53.NewFromConfig(cfg)

	// Try to get zone ID from manifest first (fastest - no discovery needed)
	var zoneID string
	manifest, err := LoadAWSCleanupManifest(workDir)
	if err == nil && manifest.Route53HostedZoneID != "" {
		zoneID = manifest.Route53HostedZoneID
		log.Printf("Using Route53 hosted zone ID from manifest: %s", zoneID)
	} else {
		// Fall back to discovery by zone name
		zoneName := fmt.Sprintf("%s.%s.", cluster.Name, *cluster.BaseDomain)
		log.Printf("Manifest zone ID not available, looking for Route53 hosted zone by name: %s", zoneName)

		zoneID, err = h.findHostedZoneIDByName(ctx, r53, zoneName)
		if err != nil {
			return err
		}
		if zoneID == "" {
			log.Printf("No Route53 hosted zone found for %s (already deleted or never created)", zoneName)
			return nil
		}

		log.Printf("Found hosted zone ID: %s", zoneID)
	}

	recordSets, err := h.listAllRecordSets(ctx, r53, zoneID)
	if err != nil {
		return fmt.Errorf("list resource record sets: %w", err)
	}

	changes := make([]route53types.Change, 0, len(recordSets))
	for _, rrset := range recordSets {
		recordType := string(rrset.Type)
		if recordType == "NS" || recordType == "SOA" {
			continue
		}

		log.Printf("Queueing DNS record deletion: %s (%s)", aws.ToString(rrset.Name), recordType)
		rrsetCopy := rrset
		changes = append(changes, route53types.Change{
			Action:            route53types.ChangeActionDelete,
			ResourceRecordSet: &rrsetCopy,
		})
	}

	if len(changes) > 0 {
		log.Printf("Deleting %d DNS records from zone %s in batches", len(changes), zoneName)
		if err := h.deleteRoute53RecordBatches(ctx, r53, zoneID, changes); err != nil {
			return fmt.Errorf("delete DNS records: %w", err)
		}
		log.Printf("Deleted %d DNS records from zone %s", len(changes), zoneName)
	}

	if err := retryAWS(ctx, "delete hosted zone", func(callCtx context.Context) error {
		_, err := r53.DeleteHostedZone(callCtx, &route53.DeleteHostedZoneInput{
			Id: aws.String(zoneID),
		})
		return err
	}); err != nil {
		return fmt.Errorf("delete hosted zone %s: %w", zoneName, err)
	}

	log.Printf("Successfully deleted Route53 hosted zone %s", zoneName)
	return nil
}

func (h *DestroyHandler) findHostedZoneIDByName(ctx context.Context, client *route53.Client, zoneName string) (string, error) {
	var out *route53.ListHostedZonesByNameOutput

	err := retryAWS(ctx, "list hosted zones by name", func(callCtx context.Context) error {
		var err error
		out, err = client.ListHostedZonesByName(callCtx, &route53.ListHostedZonesByNameInput{
			DNSName: aws.String(zoneName),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("list hosted zones by name: %w", err)
	}

	for _, zone := range out.HostedZones {
		if aws.ToString(zone.Name) == zoneName {
			return strings.TrimPrefix(aws.ToString(zone.Id), "/hostedzone/"), nil
		}
	}

	return "", nil
}

func (h *DestroyHandler) listAllRecordSets(ctx context.Context, client *route53.Client, zoneID string) ([]route53types.ResourceRecordSet, error) {
	paginator := route53.NewListResourceRecordSetsPaginator(client, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
	})

	var out []route53types.ResourceRecordSet
	for paginator.HasMorePages() {
		var page *route53.ListResourceRecordSetsOutput
		err := retryAWS(ctx, "list resource record sets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		out = append(out, page.ResourceRecordSets...)
	}

	return out, nil
}

func (h *DestroyHandler) deleteRoute53RecordBatches(ctx context.Context, client *route53.Client, zoneID string, changes []route53types.Change) error {
	for start := 0; start < len(changes); start += route53ChangeBatchSize {
		end := start + route53ChangeBatchSize
		if end > len(changes) {
			end = len(changes)
		}

		batch := changes[start:end]
		log.Printf("Submitting Route53 change batch %d-%d", start+1, end)

		var changeOut *route53.ChangeResourceRecordSetsOutput
		err := retryAWS(ctx, "change resource record sets", func(callCtx context.Context) error {
			var err error
			changeOut, err = client.ChangeResourceRecordSets(callCtx, &route53.ChangeResourceRecordSetsInput{
				HostedZoneId: aws.String(zoneID),
				ChangeBatch: &route53types.ChangeBatch{
					Changes: batch,
				},
			})
			return err
		})
		if err != nil {
			return err
		}

		changeID := aws.ToString(changeOut.ChangeInfo.Id)
		if changeID == "" {
			continue
		}

		if err := h.waitForRoute53ChangeInsync(ctx, client, changeID); err != nil {
			return fmt.Errorf("wait for Route53 change %s: %w", changeID, err)
		}
	}

	return nil
}

func (h *DestroyHandler) waitForRoute53ChangeInsync(ctx context.Context, client *route53.Client, changeID string) error {
	deadline := time.Now().Add(route53ChangePollTimeout)
	changeID = strings.TrimPrefix(changeID, "/change/")

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Route53 change %s to become INSYNC", changeID)
		}

		var out *route53.GetChangeOutput
		err := retryAWS(ctx, "get route53 change", func(callCtx context.Context) error {
			var err error
			out, err = client.GetChange(callCtx, &route53.GetChangeInput{
				Id: aws.String(changeID),
			})
			return err
		})
		if err != nil {
			return err
		}

		if out.ChangeInfo.Status == route53types.ChangeStatusInsync {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(route53ChangePollDelay):
		}
	}
}

// cleanupIAMResourcesByClusterName deletes IAM roles and OIDC providers using targeted lookups.
// Uses infraID-based exact prefix matching instead of tag inspection on every role.
func (h *DestroyHandler) cleanupIAMResourcesByClusterName(ctx context.Context, cluster *types.Cluster, infraID string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)
	stsClient := sts.NewFromConfig(cfg)

	deletedRoles := 0
	deletedProviders := 0
	var errMu sync.Mutex
	var countMu sync.Mutex
	var errorsFound []string

	addErr := func(format string, args ...any) {
		errMu.Lock()
		defer errMu.Unlock()
		errorsFound = append(errorsFound, fmt.Sprintf(format, args...))
	}

	incDeletedRoles := func() {
		countMu.Lock()
		deletedRoles++
		countMu.Unlock()
	}

	if infraID == "" {
		log.Printf("Warning: infraID not available - skipping infraID-based IAM role and OIDC cleanup")
	} else {
		log.Printf("Performing targeted cleanup using infraID: %s", infraID)

		roleNames, err := h.findInfraRoles(ctx, iamClient, infraID)
		if err != nil {
			addErr("list roles for infraID %s: %v", infraID, err)
		} else if len(roleNames) > 0 {
			log.Printf("Found %d IAM roles matching infraID prefix %s-", len(roleNames), infraID)
			h.deleteIAMRolesConcurrently(ctx, iamClient, roleNames, incDeletedRoles, addErr)
		}

		identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			addErr("get AWS account ID: %v", err)
		} else {
			accountID := aws.ToString(identity.Account)
			oidcProviderArn := fmt.Sprintf(
				"arn:aws:iam::%s:oidc-provider/%s-oidc.s3.%s.amazonaws.com",
				accountID, infraID, cluster.Region,
			)

			log.Printf("Attempting to delete OIDC provider: %s", oidcProviderArn)
			err := retryAWS(ctx, "delete OIDC provider", func(callCtx context.Context) error {
				_, err := iamClient.DeleteOpenIDConnectProvider(callCtx, &iam.DeleteOpenIDConnectProviderInput{
					OpenIDConnectProviderArn: aws.String(oidcProviderArn),
				})
				return err
			})
			if err != nil {
				if isNoSuchEntityError(err) {
					log.Printf("OIDC provider does not exist (already deleted): %s", oidcProviderArn)
				} else {
					addErr("delete OIDC provider %s: %v", oidcProviderArn, err)
				}
			} else {
				deletedProviders++
				log.Printf("Deleted OIDC provider: %s", oidcProviderArn)
			}
		}
	}

	// Clean up Windows IRSA role if it exists
	windowsRoleName := fmt.Sprintf("ocpctl-win-s3-%s", cluster.ID)
	log.Printf("Checking for Windows IRSA role: %s", windowsRoleName)

	_, err = iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(windowsRoleName),
	})
	if err == nil {
		log.Printf("Windows IRSA role exists, deleting: %s", windowsRoleName)
		if err := h.deleteSingleIAMRole(ctx, iamClient, windowsRoleName); err != nil {
			addErr("delete Windows IRSA role %s: %v", windowsRoleName, err)
		} else {
			deletedRoles++
			log.Printf("Deleted Windows IRSA role: %s", windowsRoleName)
		}
	} else if !isNoSuchEntityError(err) {
		addErr("check Windows IRSA role %s: %v", windowsRoleName, err)
	}

	log.Printf("Cleanup summary: deleted %d IAM roles, %d OIDC providers", deletedRoles, deletedProviders)
	if len(errorsFound) > 0 {
		log.Printf("Encountered %d errors during cleanup:", len(errorsFound))
		for _, e := range errorsFound {
			log.Printf("  - %s", e)
		}
		return fmt.Errorf("cleanup completed with %d errors: %s", len(errorsFound), strings.Join(errorsFound, "; "))
	}

	return nil
}

func (h *DestroyHandler) findInfraRoles(ctx context.Context, iamClient *iam.Client, infraID string) ([]string, error) {
	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})

	var roleNames []string
	prefix := infraID + "-"

	for paginator.HasMorePages() {
		var page *iam.ListRolesOutput
		err := retryAWS(ctx, "list IAM roles", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}

		for _, role := range page.Roles {
			roleName := aws.ToString(role.RoleName)
			if strings.HasPrefix(roleName, prefix) {
				roleNames = append(roleNames, roleName)
			}
		}
	}

	return roleNames, nil
}

func (h *DestroyHandler) deleteIAMRolesConcurrently(
	ctx context.Context,
	iamClient *iam.Client,
	roleNames []string,
	onDeleted func(),
	onError func(format string, args ...any),
) {
	roleCh := make(chan string)
	var wg sync.WaitGroup

	workerCount := iamDeleteWorkers
	if len(roleNames) < workerCount {
		workerCount = len(roleNames)
	}
	if workerCount < 1 {
		return
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for roleName := range roleCh {
				if err := h.deleteSingleIAMRole(ctx, iamClient, roleName); err != nil {
					onError("delete role %s: %v", roleName, err)
					continue
				}
				onDeleted()
				log.Printf("Deleted IAM role: %s", roleName)
			}
		}()
	}

	for _, roleName := range roleNames {
		roleCh <- roleName
	}
	close(roleCh)
	wg.Wait()
}

func (h *DestroyHandler) deleteSingleIAMRole(ctx context.Context, iamClient *iam.Client, roleName string) error {
	if err := h.detachRolePolicies(ctx, iamClient, roleName); err != nil {
		return fmt.Errorf("detach policies from %s: %w", roleName, err)
	}

	return retryAWS(ctx, "delete IAM role", func(callCtx context.Context) error {
		_, err := iamClient.DeleteRole(callCtx, &iam.DeleteRoleInput{
			RoleName: aws.String(roleName),
		})
		return err
	})
}

// detachRolePolicies detaches all managed and inline policies from an IAM role.
// Uses paginators so it works reliably for roles with many attached policies.
func (h *DestroyHandler) detachRolePolicies(ctx context.Context, iamClient *iam.Client, roleName string) error {
	attachedPaginator := iam.NewListAttachedRolePoliciesPaginator(iamClient, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})

	for attachedPaginator.HasMorePages() {
		var page *iam.ListAttachedRolePoliciesOutput
		err := retryAWS(ctx, "list attached role policies", func(callCtx context.Context) error {
			var err error
			page, err = attachedPaginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return fmt.Errorf("list attached policies: %w", err)
		}

		for _, policy := range page.AttachedPolicies {
			policyArn := aws.ToString(policy.PolicyArn)
			policyName := aws.ToString(policy.PolicyName)

			err := retryAWS(ctx, "detach role policy", func(callCtx context.Context) error {
				_, err := iamClient.DetachRolePolicy(callCtx, &iam.DetachRolePolicyInput{
					RoleName:  aws.String(roleName),
					PolicyArn: policy.PolicyArn,
				})
				return err
			})
			if err != nil {
				return fmt.Errorf("detach policy %s: %w", policyArn, err)
			}

			log.Printf("Detached policy %s from role %s", policyName, roleName)
		}
	}

	inlinePaginator := iam.NewListRolePoliciesPaginator(iamClient, &iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})

	for inlinePaginator.HasMorePages() {
		var page *iam.ListRolePoliciesOutput
		err := retryAWS(ctx, "list inline role policies", func(callCtx context.Context) error {
			var err error
			page, err = inlinePaginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return fmt.Errorf("list inline policies: %w", err)
		}

		for _, policyName := range page.PolicyNames {
			name := policyName
			err := retryAWS(ctx, "delete inline role policy", func(callCtx context.Context) error {
				_, err := iamClient.DeleteRolePolicy(callCtx, &iam.DeleteRolePolicyInput{
					RoleName:   aws.String(roleName),
					PolicyName: aws.String(name),
				})
				return err
			})
			if err != nil {
				return fmt.Errorf("delete inline policy %s: %w", name, err)
			}

			log.Printf("Deleted inline policy %s from role %s", name, roleName)
		}
	}

	// Defensive cleanup for instance profiles that can block role deletion in some environments.
	if err := h.removeRoleFromInstanceProfiles(ctx, iamClient, roleName); err != nil {
		return err
	}

	return nil
}

func (h *DestroyHandler) removeRoleFromInstanceProfiles(ctx context.Context, iamClient *iam.Client, roleName string) error {
	paginator := iam.NewListInstanceProfilesForRolePaginator(iamClient, &iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String(roleName),
	})

	for paginator.HasMorePages() {
		var page *iam.ListInstanceProfilesForRoleOutput
		err := retryAWS(ctx, "list instance profiles for role", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			// Some roles will never have instance profiles; treat not found as fine.
			if isNoSuchEntityError(err) {
				return nil
			}
			return fmt.Errorf("list instance profiles for role %s: %w", roleName, err)
		}

		for _, profile := range page.InstanceProfiles {
			profileName := aws.ToString(profile.InstanceProfileName)
			err := retryAWS(ctx, "remove role from instance profile", func(callCtx context.Context) error {
				_, err := iamClient.RemoveRoleFromInstanceProfile(callCtx, &iam.RemoveRoleFromInstanceProfileInput{
					InstanceProfileName: aws.String(profileName),
					RoleName:            aws.String(roleName),
				})
				return err
			})
			if err != nil && !isNoSuchEntityError(err) {
				return fmt.Errorf("remove role %s from instance profile %s: %w", roleName, profileName, err)
			}
			log.Printf("Removed role %s from instance profile %s", roleName, profileName)

			// Optional: delete empty instance profile if it matches the role name.
			if profileName == roleName {
				err := retryAWS(ctx, "delete instance profile", func(callCtx context.Context) error {
					_, err := iamClient.DeleteInstanceProfile(callCtx, &iam.DeleteInstanceProfileInput{
						InstanceProfileName: aws.String(profileName),
					})
					return err
				})
				if err == nil {
					log.Printf("Deleted instance profile %s", profileName)
				}
			}
		}
	}

	return nil
}

func retryAWS(ctx context.Context, opName string, fn func(context.Context) error) error {
	backoff := awsRetryInitialBackoff
	var lastErr error

	for attempt := 1; attempt <= awsRetryAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		callErr := fn(ctx)
		if callErr == nil {
			return nil
		}

		lastErr = callErr
		if !isRetryableAWSError(callErr) || attempt == awsRetryAttempts {
			break
		}

		log.Printf("Retrying AWS operation %q after error (attempt %d/%d): %v", opName, attempt, awsRetryAttempts, callErr)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
	}

	return lastErr
}

func isRetryableAWSError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	retryableFragments := []string{
		"throttl",
		"rate exceeded",
		"too many requests",
		"request limit exceeded",
		"timeout",
		"temporarily unavailable",
		"connection reset",
		"internalerror",
		"service unavailable",
	}

	for _, frag := range retryableFragments {
		if strings.Contains(msg, frag) {
			return true
		}
	}

	return false
}

func isNoSuchEntityError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	if strings.Contains(msg, "NoSuchEntity") || strings.Contains(strings.ToLower(msg), "not found") {
		return true
	}

	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "NoSuchEntity"
	}

	return false
}

// Avoid unused import errors if iamtypes ends up needed by future edits in this file.
var _ iamtypes.Role
