package postconfig

import (
	"fmt"
	"strings"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TaskNode represents a task in the execution DAG
type TaskNode struct {
	Name         string
	Type         string // "operator", "script", "manifest", "helmChart"
	Dependencies []string
	Config       interface{} // Original config (CustomScriptConfig, CustomManifestConfig, etc.)
}

// ExecutionDAG represents a directed acyclic graph of tasks
type ExecutionDAG struct {
	Nodes           []*TaskNode
	AdjacencyList   map[string][]string // task name -> dependencies
	ExecutionOrder  []string            // Topologically sorted task names
}

// BuildExecutionDAG builds a DAG from custom post-config and returns execution order
func BuildExecutionDAG(config *types.CustomPostConfig) (*ExecutionDAG, error) {
	dag := &ExecutionDAG{
		Nodes:         make([]*TaskNode, 0),
		AdjacencyList: make(map[string][]string),
	}

	// Add all tasks as nodes
	for _, op := range config.Operators {
		node := &TaskNode{
			Name:         op.Name,
			Type:         "operator",
			Dependencies: op.DependsOn,
			Config:       op,
		}
		dag.Nodes = append(dag.Nodes, node)
		dag.AdjacencyList[op.Name] = op.DependsOn
	}

	for _, script := range config.Scripts {
		node := &TaskNode{
			Name:         script.Name,
			Type:         "script",
			Dependencies: script.DependsOn,
			Config:       script,
		}
		dag.Nodes = append(dag.Nodes, node)
		dag.AdjacencyList[script.Name] = script.DependsOn
	}

	for _, manifest := range config.Manifests {
		node := &TaskNode{
			Name:         manifest.Name,
			Type:         "manifest",
			Dependencies: manifest.DependsOn,
			Config:       manifest,
		}
		dag.Nodes = append(dag.Nodes, node)
		dag.AdjacencyList[manifest.Name] = manifest.DependsOn
	}

	for _, chart := range config.HelmCharts {
		node := &TaskNode{
			Name:         chart.Name,
			Type:         "helmChart",
			Dependencies: chart.DependsOn,
			Config:       chart,
		}
		dag.Nodes = append(dag.Nodes, node)
		dag.AdjacencyList[chart.Name] = chart.DependsOn
	}

	// Validate dependencies exist
	taskNames := make(map[string]bool)
	for _, node := range dag.Nodes {
		taskNames[node.Name] = true
	}

	for _, node := range dag.Nodes {
		for _, dep := range node.Dependencies {
			if !taskNames[dep] {
				return nil, fmt.Errorf("task '%s' depends on non-existent task '%s'", node.Name, dep)
			}
		}
	}

	// Perform topological sort to detect cycles and determine execution order
	executionOrder, err := dag.topologicalSort()
	if err != nil {
		return nil, err
	}

	dag.ExecutionOrder = executionOrder
	return dag, nil
}

// topologicalSort performs Kahn's algorithm for topological sorting
// Returns error if cycle is detected
func (dag *ExecutionDAG) topologicalSort() ([]string, error) {
	// Calculate in-degree for each node
	// In-degree = number of dependencies (tasks that must run before this task)
	inDegree := make(map[string]int)
	for taskName, deps := range dag.AdjacencyList {
		inDegree[taskName] = len(deps)
	}

	// Queue of nodes with no dependencies
	queue := make([]string, 0)
	for _, node := range dag.Nodes {
		if inDegree[node.Name] == 0 {
			queue = append(queue, node.Name)
		}
	}

	// Process queue
	result := make([]string, 0)
	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Reduce in-degree of dependent nodes
		for _, node := range dag.Nodes {
			for _, dep := range node.Dependencies {
				if dep == current {
					inDegree[node.Name]--
					if inDegree[node.Name] == 0 {
						queue = append(queue, node.Name)
					}
				}
			}
		}
	}

	// Check if all nodes were processed (if not, there's a cycle)
	if len(result) != len(dag.Nodes) {
		// Find cycle for error message
		cycleNodes := make([]string, 0)
		for _, node := range dag.Nodes {
			found := false
			for _, name := range result {
				if name == node.Name {
					found = true
					break
				}
			}
			if !found {
				cycleNodes = append(cycleNodes, node.Name)
			}
		}
		return nil, fmt.Errorf("circular dependency detected in tasks: %s", strings.Join(cycleNodes, ", "))
	}

	return result, nil
}

// GetTasksByExecutionOrder returns tasks ordered by execution dependencies
func (dag *ExecutionDAG) GetTasksByExecutionOrder() []*TaskNode {
	ordered := make([]*TaskNode, 0, len(dag.ExecutionOrder))
	for _, name := range dag.ExecutionOrder {
		for _, node := range dag.Nodes {
			if node.Name == name {
				ordered = append(ordered, node)
				break
			}
		}
	}
	return ordered
}
