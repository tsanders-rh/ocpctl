package postconfig

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestTopologicalSort_CNVNightly(t *testing.T) {
	// Test the CNV nightly addon dependency chain:
	// kubevirt-hyperconverged depends on cnv-nightly-catalogsource
	// cnv-nightly-catalogsource depends on setup-registry-access
	// setup-registry-access has no dependencies
	config := &types.CustomPostConfig{
		Scripts: []types.CustomScriptConfig{
			{
				Name:      "setup-registry-access",
				DependsOn: []string{},
			},
		},
		Manifests: []types.CustomManifestConfig{
			{
				Name:      "cnv-nightly-catalogsource",
				DependsOn: []string{"setup-registry-access"},
			},
		},
		Operators: []types.CustomOperatorConfig{
			{
				Name:      "kubevirt-hyperconverged",
				DependsOn: []string{"cnv-nightly-catalogsource"},
			},
		},
	}

	dag, err := BuildExecutionDAG(config)
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	// Expected order: setup-registry-access, cnv-nightly-catalogsource, kubevirt-hyperconverged
	expected := []string{"setup-registry-access", "cnv-nightly-catalogsource", "kubevirt-hyperconverged"}

	if len(dag.ExecutionOrder) != len(expected) {
		t.Fatalf("Expected %d tasks, got %d", len(expected), len(dag.ExecutionOrder))
	}

	for i, task := range expected {
		if dag.ExecutionOrder[i] != task {
			t.Errorf("Expected %s at position %d, got %s", task, i, dag.ExecutionOrder[i])
		}
	}
}

func TestTopologicalSort_NoDependencies(t *testing.T) {
	// Test tasks with no dependencies - order doesn't matter
	config := &types.CustomPostConfig{
		Scripts: []types.CustomScriptConfig{
			{Name: "task-a", DependsOn: []string{}},
			{Name: "task-b", DependsOn: []string{}},
			{Name: "task-c", DependsOn: []string{}},
		},
	}

	dag, err := BuildExecutionDAG(config)
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	if len(dag.ExecutionOrder) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(dag.ExecutionOrder))
	}
}

func TestTopologicalSort_CircularDependency(t *testing.T) {
	// Test cycle detection: A -> B -> C -> A
	config := &types.CustomPostConfig{
		Scripts: []types.CustomScriptConfig{
			{Name: "task-a", DependsOn: []string{"task-c"}},
			{Name: "task-b", DependsOn: []string{"task-a"}},
			{Name: "task-c", DependsOn: []string{"task-b"}},
		},
	}

	_, err := BuildExecutionDAG(config)
	if err == nil {
		t.Fatal("Expected error for circular dependency, got nil")
	}

	if err.Error() != "circular dependency detected in tasks: task-a, task-b, task-c" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestTopologicalSort_ComplexDependencies(t *testing.T) {
	// Test complex dependency graph:
	//   A (no deps)
	//   B (no deps)
	//   C depends on A
	//   D depends on A and B
	//   E depends on C and D
	config := &types.CustomPostConfig{
		Scripts: []types.CustomScriptConfig{
			{Name: "task-a", DependsOn: []string{}},
			{Name: "task-b", DependsOn: []string{}},
			{Name: "task-c", DependsOn: []string{"task-a"}},
			{Name: "task-d", DependsOn: []string{"task-a", "task-b"}},
			{Name: "task-e", DependsOn: []string{"task-c", "task-d"}},
		},
	}

	dag, err := BuildExecutionDAG(config)
	if err != nil {
		t.Fatalf("Failed to build DAG: %v", err)
	}

	if len(dag.ExecutionOrder) != 5 {
		t.Fatalf("Expected 5 tasks, got %d", len(dag.ExecutionOrder))
	}

	// Verify that dependencies are executed before dependent tasks
	taskIndex := make(map[string]int)
	for i, task := range dag.ExecutionOrder {
		taskIndex[task] = i
	}

	// A and B should come before C, D, E
	if taskIndex["task-c"] <= taskIndex["task-a"] {
		t.Error("task-c should come after task-a")
	}
	if taskIndex["task-d"] <= taskIndex["task-a"] || taskIndex["task-d"] <= taskIndex["task-b"] {
		t.Error("task-d should come after task-a and task-b")
	}
	if taskIndex["task-e"] <= taskIndex["task-c"] || taskIndex["task-e"] <= taskIndex["task-d"] {
		t.Error("task-e should come after task-c and task-d")
	}
}
