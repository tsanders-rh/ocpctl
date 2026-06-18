package ibmcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// CISInstance represents an IBM Cloud Internet Services instance
type CISInstance struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// CISDomain represents a domain in IBM Cloud Internet Services
type CISDomain struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ValidateCISDNSZone validates that a DNS zone exists in IBM Cloud CIS for the given base domain
func ValidateCISDNSZone(ctx context.Context, baseDomain string) error {
	log.Printf("Validating IBM Cloud CIS DNS zone for: %s", baseDomain)

	// Step 1: Check if logged into IBM Cloud
	if err := ensureIBMCloudLogin(ctx); err != nil {
		return fmt.Errorf("IBM Cloud login required: %w", err)
	}

	// Step 2: Get CIS instances
	instances, err := listCISInstances(ctx)
	if err != nil {
		return &CISPreflightError{
			Message: "Failed to list IBM Cloud Internet Services instances",
			Cause:   err,
			Remediation: `IBM Cloud OpenShift IPI requires an IBM Cloud Internet Services (CIS) instance.

To fix:
1. Create a CIS instance:
   ibmcloud resource service-instance-create <name> internet-svcs standard global

2. Add your domain to CIS:
   ibmcloud cis domain-add <domain> -i <cis-instance-name>

3. Delegate DNS from your domain registrar to CIS nameservers

For more info: https://cloud.ibm.com/docs/cis?topic=cis-getting-started`,
		}
	}

	if len(instances) == 0 {
		return &CISPreflightError{
			Message: "No IBM Cloud Internet Services instances found",
			Remediation: `IBM Cloud OpenShift IPI requires an IBM Cloud Internet Services (CIS) instance with an active DNS zone.

To fix:
1. Create a CIS instance:
   ibmcloud resource service-instance-create ocpctl-cis internet-svcs standard global

2. Add your domain to CIS:
   ibmcloud cis domain-add ` + baseDomain + ` -i ocpctl-cis

3. Delegate DNS from your domain registrar to CIS nameservers:
   - Update NS records at your registrar to point to CIS nameservers
   - Wait for DNS propagation (can take up to 24 hours)

4. Verify delegation:
   dig NS ` + baseDomain + `

For more info: https://cloud.ibm.com/docs/cis?topic=cis-getting-started`,
		}
	}

	log.Printf("Found %d CIS instance(s)", len(instances))

	// Step 3: Check each CIS instance for the domain
	var foundDomain *CISDomain
	var foundInstance *CISInstance

	for _, instance := range instances {
		log.Printf("Checking CIS instance: %s (%s)", instance.Name, instance.ID)

		domains, err := listCISDomainsForInstance(ctx, instance.ID)
		if err != nil {
			log.Printf("Warning: Failed to list domains for instance %s: %v", instance.Name, err)
			continue
		}

		for _, domain := range domains {
			if domain.Name == baseDomain {
				foundDomain = &domain
				foundInstance = &instance
				break
			}
		}

		if foundDomain != nil {
			break
		}
	}

	if foundDomain == nil {
		// Domain not found in any CIS instance
		instanceNames := make([]string, len(instances))
		for i, inst := range instances {
			instanceNames[i] = inst.Name
		}

		return &CISPreflightError{
			Message: fmt.Sprintf("DNS zone for '%s' not found in any CIS instance", baseDomain),
			Remediation: fmt.Sprintf(`IBM Cloud OpenShift IPI requires an active DNS zone in IBM Cloud Internet Services.

Found CIS instance(s): %s

To fix:
1. Add your domain to CIS:
   ibmcloud cis domain-add %s -i %s

2. Delegate DNS from your domain registrar to CIS nameservers:
   - Log into IBM Cloud console and view the domain
   - Copy the CIS nameservers provided
   - Update NS records at your registrar (e.g., Route53, CloudFlare)
   - Wait for DNS propagation

3. Verify delegation:
   dig NS %s
   # Should return CIS nameservers (ns*.cloud.ibm.com)

4. Verify CIS zone is active:
   ibmcloud cis domain %s -i %s

For more info: https://cloud.ibm.com/docs/cis?topic=cis-getting-started`,
				strings.Join(instanceNames, ", "),
				baseDomain,
				instanceNames[0],
				baseDomain,
				baseDomain,
				instanceNames[0],
			),
		}
	}

	// Step 4: Verify domain is active
	if foundDomain.Status != "active" {
		return &CISPreflightError{
			Message: fmt.Sprintf("DNS zone '%s' exists but is not active (status: %s)", baseDomain, foundDomain.Status),
			Remediation: fmt.Sprintf(`The DNS zone exists in CIS but is not yet active. This usually means DNS delegation is incomplete.

Current status: %s

To fix:
1. Check domain status:
   ibmcloud cis domain %s -i %s

2. Verify CIS nameservers:
   - Log into IBM Cloud console
   - Navigate to Internet Services > %s
   - View the nameservers for %s

3. Update DNS delegation at your registrar:
   - Point NS records to the CIS nameservers
   - Wait for DNS propagation (up to 24 hours)

4. Verify delegation:
   dig NS %s
   # Should return CIS nameservers

Once the domain status is 'active', retry cluster creation.`,
				foundDomain.Status,
				baseDomain,
				foundInstance.Name,
				foundInstance.Name,
				baseDomain,
				baseDomain,
			),
		}
	}

	log.Printf("✓ CIS DNS zone validated: %s (status: %s, instance: %s)", baseDomain, foundDomain.Status, foundInstance.Name)
	return nil
}

// ensureIBMCloudLogin checks if logged into IBM Cloud CLI
func ensureIBMCloudLogin(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "ibmcloud", "target")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("not logged in (run: ibmcloud login): %w", err)
	}

	if strings.Contains(string(output), "Not logged in") {
		return fmt.Errorf("IBM Cloud CLI not logged in")
	}

	return nil
}

// listCISInstances returns all CIS instances in the account
func listCISInstances(ctx context.Context) ([]CISInstance, error) {
	cmd := exec.CommandContext(ctx, "ibmcloud", "resource", "service-instances", "--service-name", "internet-svcs", "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ibmcloud command failed: %w (stderr: %s)", err, stderr.String())
	}

	var instances []struct {
		Name  string `json:"name"`
		ID    string `json:"id"`
		CRN   string `json:"crn"`
		State string `json:"state"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &instances); err != nil {
		return nil, fmt.Errorf("parse CIS instances JSON: %w", err)
	}

	result := make([]CISInstance, len(instances))
	for i, inst := range instances {
		// Extract instance ID from CRN (last component)
		parts := strings.Split(inst.CRN, ":")
		instanceID := parts[len(parts)-2] // Second to last component is the instance ID

		result[i] = CISInstance{
			ID:    instanceID,
			Name:  inst.Name,
			State: inst.State,
		}
	}

	return result, nil
}

// listCISDomainsForInstance returns all domains in a CIS instance
func listCISDomainsForInstance(ctx context.Context, instanceID string) ([]CISDomain, error) {
	cmd := exec.CommandContext(ctx, "ibmcloud", "cis", "domains", "-i", instanceID, "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Some instances might not have domains, which returns 404
		if strings.Contains(stderr.String(), "404") || strings.Contains(stderr.String(), "not valid") {
			return []CISDomain{}, nil
		}
		return nil, fmt.Errorf("ibmcloud cis domains failed: %w (stderr: %s)", err, stderr.String())
	}

	var domains []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	// Handle empty response
	if stdout.Len() == 0 {
		return []CISDomain{}, nil
	}

	if err := json.Unmarshal(stdout.Bytes(), &domains); err != nil {
		// If JSON parsing fails, it might be empty or invalid
		return []CISDomain{}, nil
	}

	result := make([]CISDomain, len(domains))
	for i, dom := range domains {
		result[i] = CISDomain{
			ID:     dom.ID,
			Name:   dom.Name,
			Status: dom.Status,
		}
	}

	return result, nil
}

// CISPreflightError represents a CIS DNS zone validation error with remediation steps
type CISPreflightError struct {
	Message     string
	Cause       error
	Remediation string
}

func (e *CISPreflightError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("IBM Cloud CIS DNS zone validation failed: %s: %v\n\n%s", e.Message, e.Cause, e.Remediation)
	}
	return fmt.Sprintf("IBM Cloud CIS DNS zone validation failed: %s\n\n%s", e.Message, e.Remediation)
}

func (e *CISPreflightError) Unwrap() error {
	return e.Cause
}
