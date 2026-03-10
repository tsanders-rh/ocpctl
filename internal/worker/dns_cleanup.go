package worker

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// DNSCleaner handles DNS record cleanup operations for cluster retries
type DNSCleaner struct {
	region string
}

// NewDNSCleaner creates a DNS cleaner for the specified region
func NewDNSCleaner(region string) *DNSCleaner {
	return &DNSCleaner{
		region: region,
	}
}

// CleanupClusterDNS removes DNS records for a cluster from Route53
// This is called during cleanup before retrying a failed CREATE job
// Returns nil on success or if records don't exist (graceful handling)
func (d *DNSCleaner) CleanupClusterDNS(ctx context.Context, clusterName, baseDomain string) error {
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(d.region))
	if err != nil {
		// Don't fail if AWS SDK can't be loaded - log and continue
		log.Printf("Warning: failed to load AWS config for DNS cleanup: %v", err)
		return nil
	}

	route53Client := route53.NewFromConfig(cfg)

	// Get hosted zone ID for the base domain
	zoneID, err := d.getHostedZoneID(ctx, route53Client, baseDomain)
	if err != nil {
		log.Printf("Warning: failed to find hosted zone for %s: %v", baseDomain, err)
		return nil // Zone might not exist, that's ok
	}

	log.Printf("Found hosted zone %s for base domain %s", zoneID, baseDomain)

	// List all records in the zone
	recordsResult, err := route53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
	})
	if err != nil {
		log.Printf("Warning: failed to list DNS records: %v", err)
		return nil
	}

	// Build expected DNS record names for this cluster
	// Note: Route53 record names include trailing dot
	// Note: Route53 returns wildcards as \052 (octal for *)
	apiRecord := fmt.Sprintf("api.%s.%s.", clusterName, baseDomain)
	appsRecord := fmt.Sprintf("*.apps.%s.%s.", clusterName, baseDomain)
	appsRecordEscaped := fmt.Sprintf("\\052.apps.%s.%s.", clusterName, baseDomain)

	// Find and delete matching records
	deletedCount := 0
	for _, record := range recordsResult.ResourceRecordSets {
		recordName := aws.ToString(record.Name)

		// Check if this record matches our cluster
		// Route53 may return wildcards as either * or \052 (octal escape)
		if recordName == apiRecord || recordName == appsRecord || recordName == appsRecordEscaped {
			log.Printf("Deleting DNS record: %s (type: %s)", recordName, record.Type)

			if err := d.deleteDNSRecord(ctx, route53Client, zoneID, record); err != nil {
				log.Printf("Warning: failed to delete DNS record %s: %v", recordName, err)
				// Continue trying to delete other records
				continue
			}

			deletedCount++
			log.Printf("Successfully deleted DNS record: %s", recordName)
		}
	}

	if deletedCount == 0 {
		log.Printf("No DNS records found for cluster %s.%s (already cleaned up)", clusterName, baseDomain)
	} else {
		log.Printf("Deleted %d DNS record(s) for cluster %s.%s", deletedCount, clusterName, baseDomain)
	}

	return nil
}

// getHostedZoneID looks up the Route53 hosted zone ID for a base domain
func (d *DNSCleaner) getHostedZoneID(ctx context.Context, client *route53.Client, baseDomain string) (string, error) {
	// List hosted zones filtered by DNS name
	result, err := client.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
		DNSName:  aws.String(baseDomain),
		MaxItems: aws.Int32(1),
	})
	if err != nil {
		return "", fmt.Errorf("list hosted zones: %w", err)
	}

	if len(result.HostedZones) == 0 {
		return "", fmt.Errorf("no hosted zone found for domain %s", baseDomain)
	}

	// Get the zone ID and strip the /hostedzone/ prefix if present
	zoneID := aws.ToString(result.HostedZones[0].Id)
	zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")

	return zoneID, nil
}

// deleteDNSRecord deletes a specific DNS record from Route53
// Note: To delete a record, we must provide its exact configuration (Type, TTL, ResourceRecords)
func (d *DNSCleaner) deleteDNSRecord(ctx context.Context, client *route53.Client, zoneID string, record route53types.ResourceRecordSet) error {
	// Build change batch with DELETE action
	// We must provide the exact record configuration to delete it
	changeBatch := &route53types.ChangeBatch{
		Changes: []route53types.Change{
			{
				Action:            route53types.ChangeActionDelete,
				ResourceRecordSet: &record,
			},
		},
	}

	// Execute the change
	_, err := client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
		ChangeBatch:  changeBatch,
	})

	if err != nil {
		// Check for specific error cases
		errStr := err.Error()

		// Record doesn't exist - that's fine, already cleaned
		if strings.Contains(errStr, "InvalidChangeBatch") || strings.Contains(errStr, "not found") {
			return nil
		}

		return fmt.Errorf("change resource record sets: %w", err)
	}

	return nil
}
