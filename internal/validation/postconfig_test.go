package validation

import (
	"strings"
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// hasFieldError reports whether errs contains a PostConfigValidationError whose
// Field matches (or is prefixed by) want.
func hasFieldError(errs []error, want string) bool {
	for _, e := range errs {
		if ve, ok := e.(*PostConfigValidationError); ok {
			if ve.Field == want || strings.HasPrefix(ve.Field, want) {
				return true
			}
		}
	}
	return false
}

func TestValidateCustomPostConfig_Nil(t *testing.T) {
	if errs := ValidateCustomPostConfig(nil); errs != nil {
		t.Fatalf("expected nil for nil config, got %v", errs)
	}
}

func TestValidateCustomPostConfig_ValidMinimal(t *testing.T) {
	cfg := &types.CustomPostConfig{
		Operators: []types.CustomOperatorConfig{
			{Name: "cnv", Namespace: "openshift-cnv", Channel: "stable"},
		},
		Scripts: []types.CustomScriptConfig{
			{Name: "hello", Content: "echo hi", Timeout: "10m", Env: map[string]string{"FOO_BAR": "baz"}},
		},
		Manifests: []types.CustomManifestConfig{
			{Name: "ns", Content: "kind: Namespace"},
		},
		HelmCharts: []types.CustomHelmChartConfig{
			{Name: "app", Repo: "https://charts.example.com", Chart: "app"},
		},
	}
	if errs := ValidateCustomPostConfig(cfg); len(errs) != 0 {
		t.Fatalf("expected no errors for valid config, got %v", errs)
	}
}

func TestValidateCustomPostConfig_ResourceLimits(t *testing.T) {
	cfg := &types.CustomPostConfig{}
	for i := 0; i <= MaxScriptsPerCluster; i++ {
		cfg.Scripts = append(cfg.Scripts, types.CustomScriptConfig{Name: "s", Content: "x"})
	}
	for i := 0; i <= MaxOperatorsPerCluster; i++ {
		cfg.Operators = append(cfg.Operators, types.CustomOperatorConfig{Name: "o", Namespace: "n", Channel: "c"})
	}
	for i := 0; i <= MaxManifestsPerCluster; i++ {
		cfg.Manifests = append(cfg.Manifests, types.CustomManifestConfig{Name: "m", Content: "x"})
	}
	for i := 0; i <= MaxHelmChartsPerCluster; i++ {
		cfg.HelmCharts = append(cfg.HelmCharts, types.CustomHelmChartConfig{Name: "h", Repo: "https://x.io", Chart: "c"})
	}

	errs := ValidateCustomPostConfig(cfg)
	for _, field := range []string{"scripts", "operators", "manifests", "helmCharts"} {
		if !hasFieldError(errs, field) {
			t.Errorf("expected a resource-limit error for %q, got %v", field, errs)
		}
	}
}

func TestValidateOperator(t *testing.T) {
	tests := []struct {
		name    string
		op      types.CustomOperatorConfig
		wantErr []string // field suffixes expected
	}{
		{"valid", types.CustomOperatorConfig{Name: "n", Namespace: "ns", Channel: "ch"}, nil},
		{"missing name", types.CustomOperatorConfig{Namespace: "ns", Channel: "ch"}, []string{"operators[0].name"}},
		{"missing namespace", types.CustomOperatorConfig{Name: "n", Channel: "ch"}, []string{"operators[0].namespace"}},
		{"missing channel", types.CustomOperatorConfig{Name: "n", Namespace: "ns"}, []string{"operators[0].channel"}},
		{"all missing", types.CustomOperatorConfig{}, []string{"operators[0].name", "operators[0].namespace", "operators[0].channel"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateOperator(tt.op, 0)
			if len(tt.wantErr) == 0 && len(errs) != 0 {
				t.Fatalf("expected no errors, got %v", errs)
			}
			for _, want := range tt.wantErr {
				if !hasFieldError(errs, want) {
					t.Errorf("expected error for %q, got %v", want, errs)
				}
			}
		})
	}
}

func TestValidateScript_SourceMutualExclusion(t *testing.T) {
	tests := []struct {
		name      string
		script    types.CustomScriptConfig
		wantError bool
	}{
		{"content only", types.CustomScriptConfig{Name: "s", Content: "x"}, false},
		{"url only", types.CustomScriptConfig{Name: "s", URL: "https://e.io/s.sh"}, false},
		{"path only", types.CustomScriptConfig{Name: "s", Path: "s.sh"}, false},
		{"none", types.CustomScriptConfig{Name: "s"}, true},
		{"content+url", types.CustomScriptConfig{Name: "s", Content: "x", URL: "https://e.io/s.sh"}, true},
		{"all three", types.CustomScriptConfig{Name: "s", Content: "x", URL: "https://e.io/s.sh", Path: "s.sh"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateScript(tt.script, 0)
			got := hasFieldError(errs, "scripts[0]")
			if got != tt.wantError {
				t.Fatalf("wantError=%v, got errs=%v", tt.wantError, errs)
			}
		})
	}
}

func TestValidateScript_ContentSize(t *testing.T) {
	big := strings.Repeat("a", MaxScriptSize+1)
	errs := validateScript(types.CustomScriptConfig{Name: "s", Content: big}, 0)
	if !hasFieldError(errs, "scripts[0].content") {
		t.Fatalf("expected content-size error, got %v", errs)
	}
}

func TestValidateScript_Timeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid", "10m", false},
		{"unparseable", "not-a-duration", true},
		{"over max", "7h", true},
		{"zero", "0s", true},
		{"negative", "-5m", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateScript(types.CustomScriptConfig{Name: "s", Content: "x", Timeout: tt.timeout}, 0)
			got := hasFieldError(errs, "scripts[0].timeout")
			if got != tt.wantErr {
				t.Fatalf("wantErr=%v, got errs=%v", tt.wantErr, errs)
			}
		})
	}
}

func TestValidateScript_EnvVars(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"valid", map[string]string{"MY_VAR": "value"}, false},
		{"invalid name", map[string]string{"1BAD": "value"}, true},
		{"dangerous LD_PRELOAD", map[string]string{"LD_PRELOAD": "/tmp/evil.so"}, true},
		{"dangerous path lowercase", map[string]string{"path": "/evil"}, true},
		{"value too large", map[string]string{"OK": strings.Repeat("x", 4097)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateScript(types.CustomScriptConfig{Name: "s", Content: "x", Env: tt.env}, 0)
			got := hasFieldError(errs, "scripts[0].env")
			if got != tt.wantErr {
				t.Fatalf("wantErr=%v, got errs=%v", tt.wantErr, errs)
			}
		})
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name      string
		manifest  types.CustomManifestConfig
		wantField string // "" means no error expected
	}{
		{"valid content", types.CustomManifestConfig{Name: "m", Content: "x"}, ""},
		{"valid url", types.CustomManifestConfig{Name: "m", URL: "https://e.io/m.yaml"}, ""},
		{"missing name", types.CustomManifestConfig{Content: "x"}, "manifests[0].name"},
		{"neither content nor url", types.CustomManifestConfig{Name: "m"}, "manifests[0]"},
		{"both content and url", types.CustomManifestConfig{Name: "m", Content: "x", URL: "https://e.io/m.yaml"}, "manifests[0]"},
		{"bad url", types.CustomManifestConfig{Name: "m", URL: "ftp://e.io/m.yaml"}, "manifests[0].url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateManifest(tt.manifest, 0)
			if tt.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("expected no errors, got %v", errs)
				}
				return
			}
			if !hasFieldError(errs, tt.wantField) {
				t.Fatalf("expected error for %q, got %v", tt.wantField, errs)
			}
		})
	}
}

func TestValidateManifest_ContentSize(t *testing.T) {
	big := strings.Repeat("a", MaxManifestSize+1)
	errs := validateManifest(types.CustomManifestConfig{Name: "m", Content: big}, 0)
	if !hasFieldError(errs, "manifests[0].content") {
		t.Fatalf("expected content-size error, got %v", errs)
	}
}

func TestValidateHelmChart(t *testing.T) {
	tests := []struct {
		name      string
		chart     types.CustomHelmChartConfig
		wantField string
	}{
		{"valid", types.CustomHelmChartConfig{Name: "h", Repo: "https://charts.io", Chart: "c"}, ""},
		{"missing name", types.CustomHelmChartConfig{Repo: "https://charts.io", Chart: "c"}, "helmCharts[0].name"},
		{"missing repo", types.CustomHelmChartConfig{Name: "h", Chart: "c"}, "helmCharts[0].repo"},
		{"bad repo url", types.CustomHelmChartConfig{Name: "h", Repo: "not a url", Chart: "c"}, "helmCharts[0].repo"},
		{"missing chart", types.CustomHelmChartConfig{Name: "h", Repo: "https://charts.io"}, "helmCharts[0].chart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateHelmChart(tt.chart, 0)
			if tt.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("expected no errors, got %v", errs)
				}
				return
			}
			if !hasFieldError(errs, tt.wantField) {
				t.Fatalf("expected error for %q, got %v", tt.wantField, errs)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/x", false},
		{"http://example.com", false},
		{"ftp://example.com", true},  // wrong scheme
		{"file:///etc/passwd", true}, // wrong scheme
		{"https://", true},           // no host
		{"://missing-scheme", true},  // parse/scheme failure
		{"", true},                   // empty
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := validateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateURL(%q) err=%v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestIsValidEnvVarName(t *testing.T) {
	cases := map[string]bool{
		"FOO":       true,
		"_foo":      true,
		"foo_BAR_9": true,
		"a":         true,
		"":          false,
		"1FOO":      false,
		"FOO-BAR":   false,
		"FOO BAR":   false,
		"FOO.BAR":   false,
	}
	for name, want := range cases {
		if got := isValidEnvVarName(name); got != want {
			t.Errorf("isValidEnvVarName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsDangerousEnvVar(t *testing.T) {
	// Case-insensitive blocklist.
	dangerous := []string{"LD_PRELOAD", "ld_preload", "PATH", "path", "BASH_ENV", "IFS", "PYTHONPATH"}
	for _, name := range dangerous {
		if !isDangerousEnvVar(name) {
			t.Errorf("expected %q to be flagged dangerous", name)
		}
	}
	safe := []string{"MY_VAR", "HOME_DIR", "APP_CONFIG", "FOO"}
	for _, name := range safe {
		if isDangerousEnvVar(name) {
			t.Errorf("expected %q to be safe", name)
		}
	}
}
