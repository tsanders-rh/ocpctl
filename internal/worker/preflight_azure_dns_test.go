package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func TestValidateAzureBaseDomainZone_NoopWithoutAzureConfig(t *testing.T) {
	// Non-Azure profiles (and Azure profiles that don't pin a base-domain
	// resource group) must not shell out to `az` at all — this runs on every
	// create, including on hosts with no Azure CLI.
	cases := []struct {
		name string
		prof *profile.Profile
	}{
		{"nil profile", nil},
		{"no azure platform config", &profile.Profile{Name: "aws-sno-ga"}},
		{"azure config without base-domain RG", &profile.Profile{
			Name:           "azure-x",
			PlatformConfig: profile.PlatformConfig{Azure: &profile.AzureConfig{}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAzureBaseDomainZone(context.Background(), tc.prof, "example.com"); err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

func TestValidateAzureBaseDomainZone_NoopWithoutBaseDomain(t *testing.T) {
	// An Azure resource group but no resolvable base domain: there is no zone
	// name to check, so there's nothing to assert.
	prof := &profile.Profile{
		Name:           "azure-x",
		PlatformConfig: profile.PlatformConfig{Azure: &profile.AzureConfig{BaseDomainResourceGroup: "dns-rg"}},
	}
	if err := ValidateAzureBaseDomainZone(context.Background(), prof, ""); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestIsAzureNotFound draws the line the pre-flight depends on: only Azure
// saying "this resource doesn't exist" may fail a create. Everything else
// (no CLI, no login, throttling) is an inconclusive check that must let the
// install proceed rather than block the whole fleet on a broken worker.
func TestIsAzureNotFound(t *testing.T) {
	notFound := []string{
		`az group show: exit status 3: ERROR: (ResourceGroupNotFound) Resource group 'azure-mg-dog8code-com-dns' could not be found.`,
		`ERROR: (ResourceNotFound) The Resource 'Microsoft.Network/dnsZones/azure.mg.dog8code.com' under resource group 'dns-rg' was not found.`,
		`ERROR: Resource group 'dns-rg' does not exist.`,
	}
	for _, msg := range notFound {
		if !isAzureNotFound(fmt.Errorf("%s", msg)) {
			t.Errorf("expected not-found for: %s", msg)
		}
	}

	inconclusive := []string{
		`az group show: exec: "az": executable file not found in $PATH`,
		`ERROR: Please run 'az login' to setup account.`,
		`ERROR: (TooManyRequests) The request is being throttled.`,
		`ERROR: AADSTS7000215: Invalid client secret provided.`,
		`context deadline exceeded`,
	}
	for _, msg := range inconclusive {
		if isAzureNotFound(fmt.Errorf("%s", msg)) {
			t.Errorf("expected NOT to be classified not-found (must not block creates): %s", msg)
		}
	}

	if isAzureNotFound(nil) {
		t.Error("nil error must not be not-found")
	}
}

func TestNormalizeHost(t *testing.T) {
	// Azure reports "ns1-02.azure-dns.com." while net.LookupNS answers may differ
	// in case; both must compare equal.
	for _, in := range []string{"ns1-02.azure-dns.com.", "NS1-02.Azure-DNS.com", " ns1-02.azure-dns.com. "} {
		if got := normalizeHost(in); got != "ns1-02.azure-dns.com" {
			t.Errorf("normalizeHost(%q) = %q", in, got)
		}
	}
}

// TestAzureDNSMissingMessage checks the failure text actually helps: the reader
// is staring at red CI and needs the names and the commands, not a diagnosis.
func TestAzureDNSMissingMessage(t *testing.T) {
	msg := azureDNSMissingMessage("Azure resource group \"dns-rg\" does not exist.", "dns-rg", "azure.example.com")

	for _, want := range []string{
		"dns-rg",
		"azure.example.com",
		"az group create",
		"az network dns zone create",
		"nameServers",
		"#186",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("failure message is missing %q:\n%s", want, msg)
		}
	}
}

// TestCheckAzureDNSDelegation_DoesNotPanic exercises the delegation comparison's
// edge cases. It only ever logs, so the assertion is that it stays a warning
// path: a resolver failure or an undelegated domain must never fail a create.
func TestCheckAzureDNSDelegation_DoesNotPanic(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		ns     []string
	}{
		{"empty nameservers", "azure.example.com", nil},
		{"unresolvable domain", "does-not-exist.invalid", []string{"ns1-02.azure-dns.com."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkAzureDNSDelegation(tc.domain, tc.ns)
		})
	}
}
