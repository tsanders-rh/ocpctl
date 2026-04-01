package types

// CustomPostConfig defines user-defined post-deployment configuration
// This extends profile-defined configuration and is specified at cluster creation time
type CustomPostConfig struct {
	Operators  []CustomOperatorConfig  `json:"operators,omitempty"`
	Scripts    []CustomScriptConfig    `json:"scripts,omitempty"`
	Manifests  []CustomManifestConfig  `json:"manifests,omitempty"`
	HelmCharts []CustomHelmChartConfig `json:"helmCharts,omitempty"`
}

// CustomOperatorConfig defines a user-specified operator to install
type CustomOperatorConfig struct {
	Name           string                       `json:"name" validate:"required"`
	Namespace      string                       `json:"namespace" validate:"required"`
	Source         string                       `json:"source" validate:"required"` // e.g. "redhat-operators", "community-operators"
	Channel        string                       `json:"channel" validate:"required"`
	CustomResource *CustomResourceConfig `json:"customResource,omitempty"`
	Condition      string                       `json:"condition,omitempty"` // Conditional execution (e.g. "clusterType == 'openshift'")
	DependsOn      []string                     `json:"dependsOn,omitempty"` // Task dependencies (names of other tasks)
}

// CustomResourceConfig defines a custom resource to create after operator installation
type CustomResourceConfig struct {
	APIVersion string                 `json:"apiVersion" validate:"required"`
	Kind       string                 `json:"kind" validate:"required"`
	Name       string                 `json:"name" validate:"required"`
	Namespace  string                 `json:"namespace,omitempty"`
	Spec       map[string]interface{} `json:"spec,omitempty"`
}

// CustomScriptConfig defines a user-specified script to execute
// Supports both inline content and URL-based scripts with template variable substitution
type CustomScriptConfig struct {
	Name        string            `json:"name" validate:"required"`
	Content     string            `json:"content,omitempty"` // Inline script content (supports {{.Variable}} templating)
	URL         string            `json:"url,omitempty"`     // URL to download script from
	Description string            `json:"description,omitempty"`
	Timeout     string            `json:"timeout,omitempty"` // Duration string, e.g. "10m" (max 30m)
	Env         map[string]string `json:"env,omitempty"`     // Environment variables (supports {{.Variable}} templating)
	Variables   map[string]string `json:"variables,omitempty"` // Custom variables for template rendering
	Condition   string            `json:"condition,omitempty"` // Conditional execution (e.g. "clusterType == 'openshift'")
	DependsOn   []string          `json:"dependsOn,omitempty"` // Task dependencies (names of other tasks)
}

// CustomManifestConfig defines a user-specified manifest to apply
// Supports both inline content and URL-based manifests with template variable substitution
type CustomManifestConfig struct {
	Name        string            `json:"name" validate:"required"`
	Content     string            `json:"content,omitempty"` // Inline YAML/JSON content (supports {{.Variable}} templating)
	URL         string            `json:"url,omitempty"`     // URL to download manifest from
	Description string            `json:"description,omitempty"`
	Namespace   string            `json:"namespace,omitempty"` // Target namespace (supports {{.Variable}} templating)
	Variables   map[string]string `json:"variables,omitempty"` // Custom variables for template rendering
	Condition   string            `json:"condition,omitempty"` // Conditional execution (e.g. "clusterType == 'openshift'")
	DependsOn   []string          `json:"dependsOn,omitempty"` // Task dependencies (names of other tasks)
}

// CustomHelmChartConfig defines a user-specified Helm chart to install
type CustomHelmChartConfig struct {
	Name      string                 `json:"name" validate:"required"`
	Repo      string                 `json:"repo" validate:"required"`
	Chart     string                 `json:"chart" validate:"required"`
	Version   string                 `json:"version,omitempty"`
	Namespace string                 `json:"namespace,omitempty"` // Target namespace (supports {{.Variable}} templating)
	Values    map[string]interface{} `json:"values,omitempty"`    // Helm values (supports {{.Variable}} templating in string values)
	Variables map[string]string      `json:"variables,omitempty"` // Custom variables for template rendering
	Condition string                 `json:"condition,omitempty"` // Conditional execution (e.g. "clusterType == 'openshift'")
	DependsOn []string               `json:"dependsOn,omitempty"` // Task dependencies (names of other tasks)
}
