package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// releaseControllerBaseURL is the amd64 release-controller API used to resolve
// registry.ci nightly streams to concrete builds. Nightlies live ONLY here —
// they are never published to the customer mirror or quay ocp-release — so this
// is the only way to turn the aspirational profile allowlist entry
// "4.23.0-0.nightly" into something installable. It is a var (not a const) so
// tests can point it at a stub server.
var releaseControllerBaseURL = "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream"

// registryCIReleaseRepo is the registry.ci image repository that hosts nightly
// release payloads. A concrete build's pull spec is this repo tagged with the
// build name.
const registryCIReleaseRepo = "registry.ci.openshift.org/ocp/release"

// nightlyAliasRE matches a nightly *stream alias* with no build timestamp,
// e.g. "4.23.0-0.nightly" or "5.0.0-0.nightly". An alias is not a concrete
// build; it must be resolved to a dated build before install.
var nightlyAliasRE = regexp.MustCompile(`^(\d+\.\d+\.\d+)-0\.nightly$`)

// nightlyBuildRE matches a concrete dated nightly build,
// e.g. "4.23.0-0.nightly-2026-08-25-215343".
var nightlyBuildRE = regexp.MustCompile(`^(\d+\.\d+\.\d+)-0\.nightly-\d{4}-\d{2}-\d{2}-\d{6}$`)

// IsNightlyAlias reports whether version is an unresolved nightly stream alias
// (no build timestamp), which must be resolved via the release-controller.
func IsNightlyAlias(version string) bool {
	return nightlyAliasRE.MatchString(version)
}

// IsNightlyBuild reports whether version is a concrete dated nightly build.
func IsNightlyBuild(version string) bool {
	return nightlyBuildRE.MatchString(version)
}

// IsNightly reports whether version is any registry.ci nightly — either an
// unresolved stream alias or a concrete dated build.
func IsNightly(version string) bool {
	return IsNightlyAlias(version) || IsNightlyBuild(version)
}

// NightlyRelease is a resolved concrete nightly build.
type NightlyRelease struct {
	// Name is the concrete build, e.g. "4.23.0-0.nightly-2026-08-25-215343".
	Name string
	// PullSpec is the registry.ci release image, e.g.
	// "registry.ci.openshift.org/ocp/release:4.23.0-0.nightly-2026-08-25-215343".
	PullSpec string
}

// nightlyStream returns the release-controller stream name for a nightly
// version (alias or concrete build). For an alias it is the version itself; for
// a concrete build it is the build with its timestamp suffix stripped
// ("4.23.0-0.nightly-2026-08-25-215343" -> "4.23.0-0.nightly").
func nightlyStream(version string) (string, bool) {
	if IsNightlyAlias(version) {
		return version, true
	}
	if m := nightlyBuildRE.FindStringSubmatch(version); m != nil {
		return m[1] + "-0.nightly", true
	}
	return "", false
}

// RegistryCIPullSpec returns the deterministic registry.ci pull spec for a
// concrete nightly build name.
func RegistryCIPullSpec(buildName string) string {
	return registryCIReleaseRepo + ":" + buildName
}

// releaseControllerResp is the subset of the release-controller JSON we read.
type releaseControllerResp struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	PullSpec string `json:"pullSpec"`
}

// ResolveNightly resolves a nightly version to a concrete build + pull spec via
// the release-controller:
//
//   - alias ("4.23.0-0.nightly") -> newest accepted build on that stream
//     (GET /releasestream/<stream>/latest)
//   - concrete build             -> validated against the release-controller
//     (GET /releasestream/<stream>/release/<build>)
//
// It follows the same release-controller pattern as version_checker.go. The
// caller is expected to persist the returned concrete Name so retries do not
// drift to a newer nightly and destroy targets the exact build.
func ResolveNightly(ctx context.Context, version string) (*NightlyRelease, error) {
	stream, ok := nightlyStream(version)
	if !ok {
		return nil, fmt.Errorf("not a nightly version: %q", version)
	}

	var url string
	if IsNightlyAlias(version) {
		url = fmt.Sprintf("%s/%s/latest", releaseControllerBaseURL, stream)
	} else {
		url = fmt.Sprintf("%s/%s/release/%s", releaseControllerBaseURL, stream, version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build release-controller request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query release-controller for %q: %w", version, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release-controller returned %d for %q: %s", resp.StatusCode, version, string(body))
	}

	var rc releaseControllerResp
	if err := json.Unmarshal(body, &rc); err != nil {
		return nil, fmt.Errorf("parse release-controller response for %q: %w", version, err)
	}
	if rc.Name == "" {
		return nil, fmt.Errorf("release-controller returned no build name for %q", version)
	}

	pullSpec := rc.PullSpec
	if pullSpec == "" {
		// Fall back to the deterministic registry.ci pull spec if the
		// release-controller omits it.
		pullSpec = RegistryCIPullSpec(rc.Name)
	}

	return &NightlyRelease{Name: rc.Name, PullSpec: pullSpec}, nil
}
