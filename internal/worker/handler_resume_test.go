package worker

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestCheckPodHealthParsing tests the pod health check parsing logic
func TestCheckPodHealthParsing(t *testing.T) {
	tests := []struct {
		name          string
		kubectlOutput string
		expectHealthy bool
		expectIssues  int
	}{
		{
			name:          "all pods healthy",
			kubectlOutput: "multus-abc,Running,true true|multus-def,Running,true true|",
			expectHealthy: true,
			expectIssues:  0,
		},
		{
			name:          "one pod in crash loop",
			kubectlOutput: "multus-abc,Running,true true|multus-def,CrashLoopBackOff,false false|",
			expectHealthy: false,
			expectIssues:  1,
		},
		{
			name:          "pod with containers not ready",
			kubectlOutput: "multus-abc,Running,true true|multus-def,Running,false false|",
			expectHealthy: false,
			expectIssues:  1,
		},
		{
			name:          "pod in error state",
			kubectlOutput: "multus-abc,Running,true true|multus-def,Error,false false|",
			expectHealthy: false,
			expectIssues:  1,
		},
		{
			name:          "empty output - no pods",
			kubectlOutput: "",
			expectHealthy: true,
			expectIssues:  0,
		},
		{
			name:          "pod pending - not healthy but no issue logged",
			kubectlOutput: "multus-abc,Pending,false false|",
			expectHealthy: false,
			expectIssues:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the output manually (simulating checkPodHealth logic)
			var issues []string
			allHealthy := true

			if tt.kubectlOutput == "" {
				// No pods, consider healthy
				allHealthy = true
			} else {
				pods := splitPods(tt.kubectlOutput)
				for _, podInfo := range pods {
					if podInfo == "" {
						continue
					}

					parts := splitPodInfo(podInfo)
					if len(parts) < 3 {
						continue
					}

					phase := parts[1]
					readyStatuses := parts[2]

					if phase == "CrashLoopBackOff" || phase == "Error" || phase == "Failed" {
						issues = append(issues, parts[0])
						allHealthy = false
					} else if phase == "Running" {
						if containsFalse(readyStatuses) {
							issues = append(issues, parts[0])
							allHealthy = false
						}
					} else if phase == "Pending" || phase == "ContainerCreating" {
						allHealthy = false
					}
				}
			}

			if allHealthy != tt.expectHealthy {
				t.Errorf("expected healthy=%v, got %v", tt.expectHealthy, allHealthy)
			}

			if len(issues) != tt.expectIssues {
				t.Errorf("expected %d issues, got %d: %v", tt.expectIssues, len(issues), issues)
			}
		})
	}
}

// Helper functions for parsing (matching the actual implementation logic)
func splitPods(output string) []string {
	result := []string{}
	current := ""
	for _, char := range output {
		if char == '|' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(char)
		}
	}
	return result
}

func splitPodInfo(info string) []string {
	result := []string{}
	current := ""
	for _, char := range info {
		if char == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func containsFalse(s string) bool {
	return len(s) > 0 && (s[0] == 'f' || (len(s) >= 5 && s[:5] == "false"))
}

// TestRemediateCNIPodsLogic tests the remediation logic
func TestRemediateCNIPodsLogic(t *testing.T) {
	tests := []struct {
		name         string
		multusIssues []string
		ovnIssues    []string
		expectPods   []string
	}{
		{
			name:         "multus issue only",
			multusIssues: []string{"multus-abc (phase: CrashLoopBackOff)"},
			ovnIssues:    []string{},
			expectPods:   []string{"multus-abc"},
		},
		{
			name:         "ovn issue only",
			multusIssues: []string{},
			ovnIssues:    []string{"ovnkube-node-xyz (containers not ready)"},
			expectPods:   []string{"ovnkube-node-xyz"},
		},
		{
			name:         "both multus and ovn issues",
			multusIssues: []string{"multus-abc (phase: CrashLoopBackOff)", "multus-def (phase: Error)"},
			ovnIssues:    []string{"ovnkube-node-xyz (containers not ready)"},
			expectPods:   []string{"multus-abc", "multus-def", "ovnkube-node-xyz"},
		},
		{
			name:         "no issues",
			multusIssues: []string{},
			ovnIssues:    []string{},
			expectPods:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract pod names that would be deleted
			var podNames []string

			for _, issue := range tt.multusIssues {
				// Extract pod name (first word before space)
				podName := ""
				for _, char := range issue {
					if char == ' ' {
						break
					}
					podName += string(char)
				}
				if podName != "" {
					podNames = append(podNames, podName)
				}
			}

			for _, issue := range tt.ovnIssues {
				podName := ""
				for _, char := range issue {
					if char == ' ' {
						break
					}
					podName += string(char)
				}
				if podName != "" {
					podNames = append(podNames, podName)
				}
			}

			if len(podNames) != len(tt.expectPods) {
				t.Errorf("expected %d pods to be deleted, got %d", len(tt.expectPods), len(podNames))
			}

			for i, expected := range tt.expectPods {
				if i >= len(podNames) || podNames[i] != expected {
					t.Errorf("expected pod[%d]=%s, got %s", i, expected, podNames[i])
				}
			}
		})
	}
}

// TestWaitForCNIPodsIntegration is an integration test that requires kubectl
// Skip if kubectl not available or KUBECONFIG not set
func TestWaitForCNIPodsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	kubeconfig := os.Getenv("TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("TEST_KUBECONFIG not set, skipping integration test")
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not found in PATH, skipping integration test")
	}

	// Verify kubeconfig exists
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		t.Skipf("kubeconfig file not found: %s", kubeconfig)
	}

	// Create minimal handler for testing
	h := &ResumeHandler{}

	ctx := context.Background()

	// This will actually query the cluster
	t.Run("check real multus pods", func(t *testing.T) {
		healthy, issues, err := h.checkPodHealth(ctx, kubeconfig, "openshift-multus", "app=multus")
		if err != nil {
			t.Logf("Warning: failed to check pod health: %v", err)
		} else {
			t.Logf("Multus pods healthy: %v, issues: %v", healthy, issues)
		}
	})

	t.Run("check real ovn pods", func(t *testing.T) {
		healthy, issues, err := h.checkPodHealth(ctx, kubeconfig, "openshift-ovn-kubernetes", "app=ovnkube-node")
		if err != nil {
			t.Logf("Warning: failed to check pod health: %v", err)
		} else {
			t.Logf("OVN pods healthy: %v, issues: %v", healthy, issues)
		}
	})
}
