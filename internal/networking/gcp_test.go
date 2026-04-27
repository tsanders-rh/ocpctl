package networking

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGCPNetworkManager(t *testing.T) {
	t.Run("returns nil when GCP_PROJECT not set", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "")
		manager := NewGCPNetworkManager()
		assert.Nil(t, manager)
	})

	t.Run("creates manager when GCP_PROJECT is set", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "test-project")
		manager := NewGCPNetworkManager()
		require.NotNil(t, manager)
		assert.Equal(t, "test-project", manager.project)
	})
}

func TestBuildLabelsArg(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected string
	}{
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: "",
		},
		{
			name:     "nil labels",
			labels:   nil,
			expected: "",
		},
		{
			name: "single label",
			labels: map[string]string{
				"env": "development",
			},
			expected: "env=development",
		},
		{
			name: "multiple labels",
			labels: map[string]string{
				"env":        "production",
				"managed-by": "ocpctl",
				"team":       "platform",
			},
			// Note: map iteration order is not guaranteed, so we check contains
			expected: "", // Will check differently below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildLabelsArg(tt.labels)

			if len(tt.labels) == 0 {
				assert.Empty(t, result)
			} else if len(tt.labels) == 1 {
				assert.Equal(t, tt.expected, result)
			} else {
				// For multiple labels, just verify format
				assert.Contains(t, result, "=")
				assert.Contains(t, result, ",")
				// Verify all labels are present
				for k, v := range tt.labels {
					assert.Contains(t, result, k+"="+v)
				}
			}
		})
	}
}

func TestSanitizeGCPLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already valid label",
			input:    "my-cluster",
			expected: "my-cluster",
		},
		{
			name:     "uppercase letters",
			input:    "My-Cluster",
			expected: "my-cluster",
		},
		{
			name:     "spaces to hyphens",
			input:    "my cluster",
			expected: "my-cluster",
		},
		{
			name:     "special characters",
			input:    "my@cluster!name",
			expected: "my-cluster-name",
		},
		{
			name:     "underscores allowed",
			input:    "my_cluster_name",
			expected: "my_cluster_name",
		},
		{
			name:     "starts with number",
			input:    "123cluster",
			expected: "x123cluster",
		},
		{
			name:     "too long (over 63 chars)",
			input:    "this-is-a-very-long-label-name-that-exceeds-sixty-three-characters-limit",
			expected: "this-is-a-very-long-label-name-that-exceeds-sixty-three-charact", // Truncates to 63, no trailing hyphen to trim
		},
		{
			name:     "ends with hyphen",
			input:    "cluster-name-",
			expected: "cluster-name",
		},
		{
			name:     "ends with underscore",
			input:    "cluster_name_",
			expected: "cluster_name",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "!@#$%",
			expected: "x", // Becomes "x-----" then TrimRight removes trailing hyphens
		},
		{
			name:     "mixed case with numbers",
			input:    "MyCluster123",
			expected: "mycluster123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGCPLabel(tt.input)
			assert.Equal(t, tt.expected, result)

			// Verify result meets GCP label requirements
			if result != "" {
				assert.LessOrEqual(t, len(result), 63)
				assert.Regexp(t, "^[a-z][a-z0-9_-]*[a-z0-9]$|^[a-z]$", result)
			}
		})
	}
}

func TestVPCConfig_Structure(t *testing.T) {
	config := &VPCConfig{
		Name:              "my-vpc",
		Description:       "Test VPC",
		AutoCreateSubnets: false,
		Region:            "us-central1",
		SubnetCIDR:        "10.0.0.0/20",
		Labels: map[string]string{
			"env": "test",
		},
	}

	assert.NotEmpty(t, config.Name)
	assert.NotEmpty(t, config.SubnetCIDR)
	assert.NotNil(t, config.Labels)
}

func TestFirewallRuleConfig_BuildAllowString(t *testing.T) {
	tests := []struct {
		name     string
		rules    []FirewallAllowRule
		expected []string
	}{
		{
			name: "TCP with single port",
			rules: []FirewallAllowRule{
				{Protocol: "tcp", Ports: []string{"80"}},
			},
			expected: []string{"tcp:80"},
		},
		{
			name: "TCP with multiple ports",
			rules: []FirewallAllowRule{
				{Protocol: "tcp", Ports: []string{"80", "443"}},
			},
			expected: []string{"tcp:80,443"},
		},
		{
			name: "TCP with port range",
			rules: []FirewallAllowRule{
				{Protocol: "tcp", Ports: []string{"8080-8090"}},
			},
			expected: []string{"tcp:8080-8090"},
		},
		{
			name: "Protocol without ports (ICMP)",
			rules: []FirewallAllowRule{
				{Protocol: "icmp", Ports: []string{}},
			},
			expected: []string{"icmp"},
		},
		{
			name: "Multiple protocols",
			rules: []FirewallAllowRule{
				{Protocol: "tcp", Ports: []string{"22"}},
				{Protocol: "udp", Ports: []string{"53"}},
				{Protocol: "icmp", Ports: []string{}},
			},
			expected: []string{"tcp:22", "udp:53", "icmp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFirewallAllowStrings(tt.rules)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubnetInfo_Structure(t *testing.T) {
	subnet := &SubnetInfo{
		Name:        "my-subnet",
		Network:     "my-vpc",
		Region:      "us-central1",
		IPCIDRRange: "10.0.0.0/20",
	}

	assert.NotEmpty(t, subnet.Name)
	assert.NotEmpty(t, subnet.Network)
	assert.NotEmpty(t, subnet.Region)
	assert.NotEmpty(t, subnet.IPCIDRRange)
	assert.Contains(t, subnet.IPCIDRRange, "/")
}

func buildFirewallAllowStrings(rules []FirewallAllowRule) []string {
	var result []string
	for _, rule := range rules {
		if len(rule.Ports) > 0 {
			ports := ""
			for i, port := range rule.Ports {
				if i > 0 {
					ports += ","
				}
				ports += port
			}
			result = append(result, rule.Protocol+":"+ports)
		} else {
			result = append(result, rule.Protocol)
		}
	}
	return result
}
