package networking

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// GCPNetworkManager manages GCP networking resources
type GCPNetworkManager struct {
	project string
}

// NewGCPNetworkManager creates a new GCP network manager
func NewGCPNetworkManager() *GCPNetworkManager {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		log.Printf("[GCP Network Manager] GCP_PROJECT not set")
		return nil
	}

	return &GCPNetworkManager{
		project: project,
	}
}

// VPCConfig represents VPC configuration
type VPCConfig struct {
	Name              string
	Description       string
	AutoCreateSubnets bool
	Region            string
	SubnetCIDR        string
	Labels            map[string]string
}

// SubnetInfo represents a VPC subnet
type SubnetInfo struct {
	Name        string
	Network     string
	Region      string
	IPCIDRRange string
	Purpose     string
}

// FirewallRuleConfig represents a firewall rule
type FirewallRuleConfig struct {
	Name         string
	Network      string
	Direction    string // INGRESS or EGRESS
	Priority     int
	SourceRanges []string
	Allowed      []FirewallAllowRule
	TargetTags   []string
	Description  string
	Labels       map[string]string
}

// FirewallAllowRule represents an allowed protocol/port combination
type FirewallAllowRule struct {
	Protocol string   // tcp, udp, icmp, etc.
	Ports    []string // e.g., ["80", "443", "8080-8090"]
}

// DNSZoneConfig represents Cloud DNS zone configuration
type DNSZoneConfig struct {
	Name        string
	DNSName     string
	Description string
	Visibility  string // public or private
	Labels      map[string]string
}

// DNSRecordConfig represents a DNS record
type DNSRecordConfig struct {
	Zone    string
	Name    string
	Type    string   // A, AAAA, CNAME, TXT, etc.
	TTL     int      // in seconds
	RRDatas []string // record data
}

// CreateVPC creates or reuses a VPC network for a cluster
func (m *GCPNetworkManager) CreateVPC(ctx context.Context, config *VPCConfig) error {
	// Check if VPC already exists
	exists, err := m.VPCExists(ctx, config.Name)
	if err != nil {
		return fmt.Errorf("check VPC existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Network] VPC %s already exists, reusing", config.Name)
		return nil
	}

	// Build labels argument
	labelsArg := buildLabelsArg(config.Labels)

	// Create VPC
	args := []string{"compute", "networks", "create", config.Name,
		"--project", m.project,
		"--description", config.Description,
		"--subnet-mode", "custom", // Always use custom mode for better control
	}

	if labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create VPC: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Created VPC: %s", config.Name)
	return nil
}

// CreateSubnet creates a subnet in a VPC
func (m *GCPNetworkManager) CreateSubnet(ctx context.Context, info *SubnetInfo) error {
	// Check if subnet already exists
	exists, err := m.SubnetExists(ctx, info.Name, info.Region)
	if err != nil {
		return fmt.Errorf("check subnet existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Network] Subnet %s already exists in region %s", info.Name, info.Region)
		return nil
	}

	// Create subnet
	args := []string{"compute", "networks", "subnets", "create", info.Name,
		"--project", m.project,
		"--network", info.Network,
		"--region", info.Region,
		"--range", info.IPCIDRRange,
	}

	if info.Purpose != "" {
		args = append(args, "--purpose", info.Purpose)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Created subnet: %s in region %s (CIDR: %s)", info.Name, info.Region, info.IPCIDRRange)
	return nil
}

// CreateFirewallRule creates a firewall rule
func (m *GCPNetworkManager) CreateFirewallRule(ctx context.Context, config *FirewallRuleConfig) error {
	// Check if rule already exists
	exists, err := m.FirewallRuleExists(ctx, config.Name)
	if err != nil {
		return fmt.Errorf("check firewall rule existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Network] Firewall rule %s already exists", config.Name)
		return nil
	}

	// Build allow rules
	var allowRules []string
	for _, rule := range config.Allowed {
		if len(rule.Ports) > 0 {
			allowRules = append(allowRules, fmt.Sprintf("%s:%s", rule.Protocol, strings.Join(rule.Ports, ",")))
		} else {
			allowRules = append(allowRules, rule.Protocol)
		}
	}

	args := []string{"compute", "firewall-rules", "create", config.Name,
		"--project", m.project,
		"--network", config.Network,
		"--direction", config.Direction,
		"--priority", fmt.Sprintf("%d", config.Priority),
		"--allow", strings.Join(allowRules, ","),
	}

	if len(config.SourceRanges) > 0 {
		args = append(args, "--source-ranges", strings.Join(config.SourceRanges, ","))
	}

	if len(config.TargetTags) > 0 {
		args = append(args, "--target-tags", strings.Join(config.TargetTags, ","))
	}

	if config.Description != "" {
		args = append(args, "--description", config.Description)
	}

	if labelsArg := buildLabelsArg(config.Labels); labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create firewall rule: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Created firewall rule: %s", config.Name)
	return nil
}

// CreateDNSZone creates a Cloud DNS managed zone
func (m *GCPNetworkManager) CreateDNSZone(ctx context.Context, config *DNSZoneConfig) error {
	// Check if zone already exists
	exists, err := m.DNSZoneExists(ctx, config.Name)
	if err != nil {
		return fmt.Errorf("check DNS zone existence: %w", err)
	}

	if exists {
		log.Printf("[GCP Network] DNS zone %s already exists", config.Name)
		return nil
	}

	args := []string{"dns", "managed-zones", "create", config.Name,
		"--project", m.project,
		"--dns-name", config.DNSName,
		"--description", config.Description,
		"--visibility", config.Visibility,
	}

	if labelsArg := buildLabelsArg(config.Labels); labelsArg != "" {
		args = append(args, "--labels", labelsArg)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create DNS zone: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Created DNS zone: %s (%s)", config.Name, config.DNSName)
	return nil
}

// CreateDNSRecord creates or updates a DNS record
func (m *GCPNetworkManager) CreateDNSRecord(ctx context.Context, config *DNSRecordConfig) error {
	// Start a transaction
	cmd := exec.CommandContext(ctx, "gcloud", "dns", "record-sets", "transaction", "start",
		"--zone", config.Zone,
		"--project", m.project)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start DNS transaction: %w\nOutput: %s", err, output)
	}

	// Add the record
	args := []string{"dns", "record-sets", "transaction", "add",
		"--zone", config.Zone,
		"--project", m.project,
		"--name", config.Name,
		"--type", config.Type,
		"--ttl", fmt.Sprintf("%d", config.TTL),
	}

	// Add rrdatas
	for _, data := range config.RRDatas {
		args = append(args, data)
	}

	cmd = exec.CommandContext(ctx, "gcloud", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Abort transaction
		exec.CommandContext(ctx, "gcloud", "dns", "record-sets", "transaction", "abort",
			"--zone", config.Zone, "--project", m.project).Run()
		return fmt.Errorf("failed to add DNS record: %w\nOutput: %s", err, output)
	}

	// Execute the transaction
	cmd = exec.CommandContext(ctx, "gcloud", "dns", "record-sets", "transaction", "execute",
		"--zone", config.Zone,
		"--project", m.project)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to execute DNS transaction: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Created DNS record: %s %s -> %v", config.Name, config.Type, config.RRDatas)
	return nil
}

// DeleteVPC deletes a VPC network
func (m *GCPNetworkManager) DeleteVPC(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "networks", "delete", name,
		"--project", m.project,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete VPC: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Deleted VPC: %s", name)
	return nil
}

// DeleteSubnet deletes a subnet
func (m *GCPNetworkManager) DeleteSubnet(ctx context.Context, name, region string) error {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "networks", "subnets", "delete", name,
		"--project", m.project,
		"--region", region,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete subnet: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Deleted subnet: %s in region %s", name, region)
	return nil
}

// DeleteFirewallRule deletes a firewall rule
func (m *GCPNetworkManager) DeleteFirewallRule(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "firewall-rules", "delete", name,
		"--project", m.project,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete firewall rule: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Deleted firewall rule: %s", name)
	return nil
}

// DeleteDNSZone deletes a Cloud DNS managed zone
func (m *GCPNetworkManager) DeleteDNSZone(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "gcloud", "dns", "managed-zones", "delete", name,
		"--project", m.project,
		"--quiet")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete DNS zone: %w\nOutput: %s", err, output)
	}

	log.Printf("[GCP Network] Deleted DNS zone: %s", name)
	return nil
}

// VPCExists checks if a VPC network exists
func (m *GCPNetworkManager) VPCExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "networks", "describe", name,
		"--project", m.project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		// Check if error is "not found"
		if strings.Contains(err.Error(), "was not found") || strings.Contains(string(output), "was not found") {
			return false, nil
		}
		return false, fmt.Errorf("check VPC existence: %w", err)
	}

	return true, nil
}

// SubnetExists checks if a subnet exists
func (m *GCPNetworkManager) SubnetExists(ctx context.Context, name, region string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "networks", "subnets", "describe", name,
		"--project", m.project,
		"--region", region,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(string(output), "was not found") {
			return false, nil
		}
		return false, fmt.Errorf("check subnet existence: %w", err)
	}

	return true, nil
}

// FirewallRuleExists checks if a firewall rule exists
func (m *GCPNetworkManager) FirewallRuleExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "firewall-rules", "describe", name,
		"--project", m.project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(string(output), "was not found") {
			return false, nil
		}
		return false, fmt.Errorf("check firewall rule existence: %w", err)
	}

	return true, nil
}

// DNSZoneExists checks if a DNS zone exists
func (m *GCPNetworkManager) DNSZoneExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "dns", "managed-zones", "describe", name,
		"--project", m.project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "was not found") || strings.Contains(string(output), "was not found") {
			return false, nil
		}
		return false, fmt.Errorf("check DNS zone existence: %w", err)
	}

	return true, nil
}

// GetVPCInfo retrieves VPC network information
func (m *GCPNetworkManager) GetVPCInfo(ctx context.Context, name string) (*VPCInfo, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "networks", "describe", name,
		"--project", m.project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get VPC info: %w", err)
	}

	var info VPCInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("parse VPC info: %w", err)
	}

	return &info, nil
}

// VPCInfo represents VPC network information
type VPCInfo struct {
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	AutoCreateSubnets bool              `json:"autoCreateSubnets"`
	Subnets           []string          `json:"subnetworks"`
	SelfLink          string            `json:"selfLink"`
}

// SetupClusterNetworking creates all networking resources for a cluster
func (m *GCPNetworkManager) SetupClusterNetworking(ctx context.Context, cluster *types.Cluster, vpcName, subnetCIDR string) error {
	if m == nil {
		return fmt.Errorf("GCP network manager not initialized (GCP_PROJECT not set)")
	}

	// Build labels for resources
	labels := map[string]string{
		"managed-by":   "ocpctl",
		"cluster-id":   sanitizeGCPLabel(cluster.ID),
		"cluster-name": sanitizeGCPLabel(cluster.Name),
		"profile":      sanitizeGCPLabel(cluster.Profile),
	}

	// 1. Create or reuse VPC
	vpcConfig := &VPCConfig{
		Name:              vpcName,
		Description:       fmt.Sprintf("VPC for cluster %s", cluster.Name),
		AutoCreateSubnets: false,
		Labels:            labels,
	}

	if err := m.CreateVPC(ctx, vpcConfig); err != nil {
		return fmt.Errorf("create VPC: %w", err)
	}

	// 2. Create subnet
	subnetName := fmt.Sprintf("%s-subnet", cluster.Name)
	subnetInfo := &SubnetInfo{
		Name:        subnetName,
		Network:     vpcName,
		Region:      cluster.Region,
		IPCIDRRange: subnetCIDR,
	}

	if err := m.CreateSubnet(ctx, subnetInfo); err != nil {
		return fmt.Errorf("create subnet: %w", err)
	}

	// 3. Create firewall rules for cluster communication
	if err := m.createClusterFirewallRules(ctx, cluster, vpcName, labels); err != nil {
		return fmt.Errorf("create firewall rules: %w", err)
	}

	log.Printf("[GCP Network] Completed networking setup for cluster %s", cluster.Name)
	return nil
}

// createClusterFirewallRules creates standard firewall rules for a cluster
func (m *GCPNetworkManager) createClusterFirewallRules(ctx context.Context, cluster *types.Cluster, vpcName string, labels map[string]string) error {
	// Allow internal traffic within the VPC
	internalRule := &FirewallRuleConfig{
		Name:      fmt.Sprintf("%s-allow-internal", cluster.Name),
		Network:   vpcName,
		Direction: "INGRESS",
		Priority:  1000,
		SourceRanges: []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
		},
		Allowed: []FirewallAllowRule{
			{Protocol: "tcp", Ports: []string{"0-65535"}},
			{Protocol: "udp", Ports: []string{"0-65535"}},
			{Protocol: "icmp"},
		},
		Description: fmt.Sprintf("Allow internal traffic for cluster %s", cluster.Name),
		Labels:      labels,
	}

	if err := m.CreateFirewallRule(ctx, internalRule); err != nil {
		return err
	}

	// Allow SSH from anywhere (adjust based on security requirements)
	sshRule := &FirewallRuleConfig{
		Name:         fmt.Sprintf("%s-allow-ssh", cluster.Name),
		Network:      vpcName,
		Direction:    "INGRESS",
		Priority:     1000,
		SourceRanges: []string{"0.0.0.0/0"},
		Allowed: []FirewallAllowRule{
			{Protocol: "tcp", Ports: []string{"22"}},
		},
		Description: fmt.Sprintf("Allow SSH for cluster %s", cluster.Name),
		Labels:      labels,
	}

	if err := m.CreateFirewallRule(ctx, sshRule); err != nil {
		return err
	}

	// Allow HTTPS/API access
	httpsRule := &FirewallRuleConfig{
		Name:         fmt.Sprintf("%s-allow-https", cluster.Name),
		Network:      vpcName,
		Direction:    "INGRESS",
		Priority:     1000,
		SourceRanges: []string{"0.0.0.0/0"},
		Allowed: []FirewallAllowRule{
			{Protocol: "tcp", Ports: []string{"443", "6443"}}, // HTTPS and Kubernetes API
		},
		Description: fmt.Sprintf("Allow HTTPS/API for cluster %s", cluster.Name),
		Labels:      labels,
	}

	return m.CreateFirewallRule(ctx, httpsRule)
}

// buildLabelsArg converts labels map to gcloud CLI format
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

// sanitizeGCPLabel converts a string to valid GCP label format
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
