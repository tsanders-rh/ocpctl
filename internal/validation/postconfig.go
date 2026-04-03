package validation

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

const (
	// MaxScriptSize is the maximum size of an inline script (10KB)
	MaxScriptSize = 10 * 1024

	// MaxManifestSize is the maximum size of an inline manifest (100KB)
	MaxManifestSize = 100 * 1024

	// MaxScriptTimeout is the maximum timeout for a single script (30 minutes)
	MaxScriptTimeout = 30 * time.Minute

	// MaxScriptsPerCluster is the maximum number of custom scripts per cluster
	MaxScriptsPerCluster = 10

	// MaxOperatorsPerCluster is the maximum number of custom operators per cluster
	MaxOperatorsPerCluster = 5

	// MaxManifestsPerCluster is the maximum number of custom manifests per cluster
	MaxManifestsPerCluster = 10

	// MaxHelmChartsPerCluster is the maximum number of custom Helm charts per cluster
	MaxHelmChartsPerCluster = 5
)

// PostConfigValidationError represents a validation error for custom post-config
type PostConfigValidationError struct {
	Field   string
	Message string
}

func (e *PostConfigValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateCustomPostConfig validates user-defined post-deployment configuration
func ValidateCustomPostConfig(config *types.CustomPostConfig) []error {
	if config == nil {
		return nil
	}

	var errors []error

	// Validate resource limits
	if len(config.Scripts) > MaxScriptsPerCluster {
		errors = append(errors, &PostConfigValidationError{
			Field:   "scripts",
			Message: fmt.Sprintf("maximum %d scripts allowed per cluster", MaxScriptsPerCluster),
		})
	}

	if len(config.Operators) > MaxOperatorsPerCluster {
		errors = append(errors, &PostConfigValidationError{
			Field:   "operators",
			Message: fmt.Sprintf("maximum %d operators allowed per cluster", MaxOperatorsPerCluster),
		})
	}

	if len(config.Manifests) > MaxManifestsPerCluster {
		errors = append(errors, &PostConfigValidationError{
			Field:   "manifests",
			Message: fmt.Sprintf("maximum %d manifests allowed per cluster", MaxManifestsPerCluster),
		})
	}

	if len(config.HelmCharts) > MaxHelmChartsPerCluster {
		errors = append(errors, &PostConfigValidationError{
			Field:   "helmCharts",
			Message: fmt.Sprintf("maximum %d Helm charts allowed per cluster", MaxHelmChartsPerCluster),
		})
	}

	// Validate operators
	for i, op := range config.Operators {
		if err := validateOperator(op, i); err != nil {
			errors = append(errors, err...)
		}
	}

	// Validate scripts
	for i, script := range config.Scripts {
		if err := validateScript(script, i); err != nil {
			errors = append(errors, err...)
		}
	}

	// Validate manifests
	for i, manifest := range config.Manifests {
		if err := validateManifest(manifest, i); err != nil {
			errors = append(errors, err...)
		}
	}

	// Validate Helm charts
	for i, chart := range config.HelmCharts {
		if err := validateHelmChart(chart, i); err != nil {
			errors = append(errors, err...)
		}
	}

	return errors
}

func validateOperator(op types.CustomOperatorConfig, index int) []error {
	var errors []error
	prefix := fmt.Sprintf("operators[%d]", index)

	if op.Name == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".name",
			Message: "operator name is required",
		})
	}

	if op.Namespace == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".namespace",
			Message: "operator namespace is required",
		})
	}

	// Source is optional - OLM will search all catalogs if not specified

	if op.Channel == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".channel",
			Message: "operator channel is required",
		})
	}

	return errors
}

func validateScript(script types.CustomScriptConfig, index int) []error {
	var errors []error
	prefix := fmt.Sprintf("scripts[%d]", index)

	if script.Name == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".name",
			Message: "script name is required",
		})
	}

	// Must have either content or URL, but not both
	hasContent := script.Content != ""
	hasURL := script.URL != ""

	if !hasContent && !hasURL {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix,
			Message: "script must have either 'content' or 'url'",
		})
	}

	if hasContent && hasURL {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix,
			Message: "script cannot have both 'content' and 'url'",
		})
	}

	// Validate content size
	if hasContent && len(script.Content) > MaxScriptSize {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".content",
			Message: fmt.Sprintf("script content exceeds maximum size of %d bytes", MaxScriptSize),
		})
	}

	// Validate URL format
	if hasURL {
		if err := validateURL(script.URL); err != nil {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".url",
				Message: fmt.Sprintf("invalid URL: %v", err),
			})
		}
	}

	// Validate timeout
	if script.Timeout != "" {
		duration, err := time.ParseDuration(script.Timeout)
		if err != nil {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".timeout",
				Message: fmt.Sprintf("invalid timeout format: %v (use duration string like '10m')", err),
			})
		} else if duration > MaxScriptTimeout {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".timeout",
				Message: fmt.Sprintf("timeout exceeds maximum of %v", MaxScriptTimeout),
			})
		} else if duration <= 0 {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".timeout",
				Message: "timeout must be positive",
			})
		}
	}

	// Validate environment variables
	for key, value := range script.Env {
		if !isValidEnvVarName(key) {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".env." + key,
				Message: "invalid environment variable name (must match ^[A-Za-z_][A-Za-z0-9_]*$)",
			})
		}

		if isDangerousEnvVar(key) {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".env." + key,
				Message: "dangerous environment variable blocked (security risk)",
			})
		}

		if len(value) > 4096 {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".env." + key,
				Message: "environment variable value exceeds 4KB limit",
			})
		}
	}

	return errors
}

func validateManifest(manifest types.CustomManifestConfig, index int) []error {
	var errors []error
	prefix := fmt.Sprintf("manifests[%d]", index)

	if manifest.Name == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".name",
			Message: "manifest name is required",
		})
	}

	// Must have either content or URL, but not both
	hasContent := manifest.Content != ""
	hasURL := manifest.URL != ""

	if !hasContent && !hasURL {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix,
			Message: "manifest must have either 'content' or 'url'",
		})
	}

	if hasContent && hasURL {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix,
			Message: "manifest cannot have both 'content' and 'url'",
		})
	}

	// Validate content size
	if hasContent && len(manifest.Content) > MaxManifestSize {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".content",
			Message: fmt.Sprintf("manifest content exceeds maximum size of %d bytes", MaxManifestSize),
		})
	}

	// Validate URL format
	if hasURL {
		if err := validateURL(manifest.URL); err != nil {
			errors = append(errors, &PostConfigValidationError{
				Field:   prefix + ".url",
				Message: fmt.Sprintf("invalid URL: %v", err),
			})
		}
	}

	return errors
}

func validateHelmChart(chart types.CustomHelmChartConfig, index int) []error {
	var errors []error
	prefix := fmt.Sprintf("helmCharts[%d]", index)

	if chart.Name == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".name",
			Message: "Helm chart name is required",
		})
	}

	if chart.Repo == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".repo",
			Message: "Helm chart repo is required",
		})
	} else if err := validateURL(chart.Repo); err != nil {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".repo",
			Message: fmt.Sprintf("invalid repo URL: %v", err),
		})
	}

	if chart.Chart == "" {
		errors = append(errors, &PostConfigValidationError{
			Field:   prefix + ".chart",
			Message: "Helm chart name is required",
		})
	}

	return errors
}

func validateURL(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	// Must be HTTP or HTTPS
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}

	// Must have a host
	if parsedURL.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}

func isValidEnvVarName(name string) bool {
	// Environment variable names must match: [A-Za-z_][A-Za-z0-9_]*
	if len(name) == 0 {
		return false
	}

	// First character must be letter or underscore
	first := name[0]
	if !((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || first == '_') {
		return false
	}

	// Remaining characters must be alphanumeric or underscore
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}

	return true
}

func isDangerousEnvVar(name string) bool {
	// Blocklist of dangerous environment variables that can lead to
	// privilege escalation or code injection
	dangerousVars := map[string]bool{
		"LD_PRELOAD":         true, // Can hijack dynamic linker
		"LD_LIBRARY_PATH":    true, // Can override library loading
		"PYTHONPATH":         true, // Can inject Python modules
		"PATH":               true, // Can prepend malicious binaries
		"PERL5LIB":           true, // Can inject Perl modules
		"RUBYLIB":            true, // Can inject Ruby modules
		"NODE_PATH":          true, // Can inject Node.js modules
		"CLASSPATH":          true, // Can inject Java classes
		"JAVA_TOOL_OPTIONS": true, // Can inject Java agents
		"BASH_ENV":           true, // Executes file on Bash startup
		"ENV":                true, // Executes on shell startup
		"IFS":                true, // Can break command parsing
		"SHELL":              true, // Can change shell interpreter
	}

	return dangerousVars[strings.ToUpper(name)]
}
