package postconfig

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TemplateContext holds all available variables for template rendering
type TemplateContext struct {
	// Cluster information
	ClusterID   string
	ClusterName string
	ClusterType string
	Platform    string
	Region      string
	BaseDomain  string
	Profile     string

	// Infrastructure details
	InfraID string

	// Namespace (for context)
	Namespace string

	// Custom variables from config
	Variables map[string]string
}

// BuildTemplateContext creates a template context from cluster information
func BuildTemplateContext(cluster *types.Cluster, infraID string, customVars map[string]string) *TemplateContext {
	ctx := &TemplateContext{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		ClusterType: string(cluster.ClusterType),
		Platform:    string(cluster.Platform),
		Region:      cluster.Region,
		Profile:     cluster.Profile,
		InfraID:     infraID,
		Variables:   make(map[string]string),
	}

	// Add base domain if available
	if cluster.BaseDomain != nil {
		ctx.BaseDomain = *cluster.BaseDomain
	}

	// Merge custom variables
	for k, v := range customVars {
		ctx.Variables[k] = v
	}

	return ctx
}

// RenderTemplate renders a template string with the given context
// Supports Go template syntax: {{.Variable}}
func RenderTemplate(templateStr string, ctx *TemplateContext) (string, error) {
	if templateStr == "" {
		return "", nil
	}

	// Define helper functions for templates
	funcMap := template.FuncMap{
		"contains": strings.Contains,
		"eq":       func(a, b string) bool { return a == b },
		"ne":       func(a, b string) bool { return a != b },
	}

	// Create template with function map
	tmpl, err := template.New("config").Funcs(funcMap).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	// Render template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// RenderMapValues renders all string values in a map (for env vars, helm values, etc.)
func RenderMapValues(m map[string]string, ctx *TemplateContext) (map[string]string, error) {
	if m == nil {
		return nil, nil
	}

	result := make(map[string]string)
	for k, v := range m {
		rendered, err := RenderTemplate(v, ctx)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", k, err)
		}
		result[k] = rendered
	}

	return result, nil
}

// EvaluateCondition evaluates a conditional expression
// Supports simple comparisons: ==, !=, contains
// Examples:
//   - "clusterType == 'openshift'"
//   - "platform == 'aws'"
//   - "region contains 'us-'"
func EvaluateCondition(condition string, ctx *TemplateContext) (bool, error) {
	if condition == "" {
		return true, nil // Empty condition always passes
	}

	// Parse condition (simple implementation for Phase 4)
	// Format: "variable operator value"
	parts := strings.Fields(condition)
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid condition format: %s (expected 'variable operator value')", condition)
	}

	variable := strings.TrimSpace(parts[0])
	operator := strings.TrimSpace(parts[1])
	expectedValue := strings.Trim(strings.TrimSpace(parts[2]), "'\"")

	// Get actual value from context
	var actualValue string
	switch variable {
	case "clusterType":
		actualValue = ctx.ClusterType
	case "platform":
		actualValue = ctx.Platform
	case "region":
		actualValue = ctx.Region
	case "profile":
		actualValue = ctx.Profile
	case "baseDomain":
		actualValue = ctx.BaseDomain
	default:
		// Check custom variables
		if val, ok := ctx.Variables[variable]; ok {
			actualValue = val
		} else {
			return false, fmt.Errorf("unknown variable: %s", variable)
		}
	}

	// Evaluate operator
	switch operator {
	case "==":
		return actualValue == expectedValue, nil
	case "!=":
		return actualValue != expectedValue, nil
	case "contains":
		return strings.Contains(actualValue, expectedValue), nil
	default:
		return false, fmt.Errorf("unknown operator: %s", operator)
	}
}
