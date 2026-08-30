package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNightlyClassification(t *testing.T) {
	cases := []struct {
		version string
		alias   bool
		build   bool
	}{
		{"4.23.0-0.nightly", true, false},
		{"5.0.0-0.nightly", true, false},
		{"4.23.0-0.nightly-2026-08-25-215343", false, true},
		{"5.0.0-0.nightly-2026-01-02-030405", false, true},
		{"4.22.0-ec.5", false, false},
		{"4.22.0-rc.1", false, false},
		{"4.22.3", false, false},
		{"4.23", false, false},
		{"", false, false},
		// Malformed: partial timestamp must NOT be treated as a build.
		{"4.23.0-0.nightly-2026-08-25", false, false},
	}
	for _, c := range cases {
		if got := IsNightlyAlias(c.version); got != c.alias {
			t.Errorf("IsNightlyAlias(%q) = %v, want %v", c.version, got, c.alias)
		}
		if got := IsNightlyBuild(c.version); got != c.build {
			t.Errorf("IsNightlyBuild(%q) = %v, want %v", c.version, got, c.build)
		}
		if got := IsNightly(c.version); got != (c.alias || c.build) {
			t.Errorf("IsNightly(%q) = %v, want %v", c.version, got, c.alias || c.build)
		}
	}
}

func TestNightlyStream(t *testing.T) {
	cases := []struct {
		version string
		stream  string
		ok      bool
	}{
		{"4.23.0-0.nightly", "4.23.0-0.nightly", true},
		{"4.23.0-0.nightly-2026-08-25-215343", "4.23.0-0.nightly", true},
		{"5.0.0-0.nightly-2026-01-02-030405", "5.0.0-0.nightly", true},
		{"4.22.0-ec.5", "", false},
	}
	for _, c := range cases {
		stream, ok := nightlyStream(c.version)
		if ok != c.ok || stream != c.stream {
			t.Errorf("nightlyStream(%q) = (%q, %v), want (%q, %v)", c.version, stream, ok, c.stream, c.ok)
		}
	}
}

func TestRegistryCIPullSpec(t *testing.T) {
	tests := []struct {
		name  string
		build string
		want  string
	}{
		{
			name:  "4.x uses ocp/release",
			build: "4.23.0-0.nightly-2026-08-25-215343",
			want:  "registry.ci.openshift.org/ocp/release:4.23.0-0.nightly-2026-08-25-215343",
		},
		{
			// 5.x nightlies live under a per-major stream (ocp/release-5);
			// using ocp/release yields "manifest unknown" at extract time.
			name:  "5.x uses ocp/release-5",
			build: "5.0.0-0.nightly-2026-08-30-014421",
			want:  "registry.ci.openshift.org/ocp/release-5:5.0.0-0.nightly-2026-08-30-014421",
		},
		{
			name:  "6.x uses ocp/release-6",
			build: "6.0.0-0.nightly-2027-01-01-000000",
			want:  "registry.ci.openshift.org/ocp/release-6:6.0.0-0.nightly-2027-01-01-000000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RegistryCIPullSpec(tt.build); got != tt.want {
				t.Errorf("RegistryCIPullSpec(%q) = %q, want %q", tt.build, got, tt.want)
			}
		})
	}
}

// newTestResolver points ResolveNightly at a stub release-controller by
// swapping the package base URL for the test server and restoring it after.
func withReleaseControllerBase(t *testing.T, url string) {
	t.Helper()
	orig := releaseControllerBaseURL
	releaseControllerBaseURL = url
	t.Cleanup(func() { releaseControllerBaseURL = orig })
}

func TestResolveNightlyAlias(t *testing.T) {
	const build = "4.23.0-0.nightly-2026-08-25-215343"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/4.23.0-0.nightly/latest") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"` + build + `","phase":"Accepted","pullSpec":"registry.ci.openshift.org/ocp/release:` + build + `"}`))
	}))
	defer srv.Close()
	withReleaseControllerBase(t, srv.URL)

	rel, err := ResolveNightly(context.Background(), "4.23.0-0.nightly")
	if err != nil {
		t.Fatalf("ResolveNightly: %v", err)
	}
	if rel.Name != build {
		t.Errorf("Name = %q, want %q", rel.Name, build)
	}
	if rel.PullSpec != "registry.ci.openshift.org/ocp/release:"+build {
		t.Errorf("PullSpec = %q", rel.PullSpec)
	}
}

func TestResolveNightlyConcreteBuild(t *testing.T) {
	const build = "4.23.0-0.nightly-2026-08-25-215343"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/4.23.0-0.nightly/release/"+build) {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		// No pullSpec in the response: resolver must fall back to the
		// deterministic registry.ci pull spec.
		w.Write([]byte(`{"name":"` + build + `","phase":"Accepted"}`))
	}))
	defer srv.Close()
	withReleaseControllerBase(t, srv.URL)

	rel, err := ResolveNightly(context.Background(), build)
	if err != nil {
		t.Fatalf("ResolveNightly: %v", err)
	}
	if rel.PullSpec != RegistryCIPullSpec(build) {
		t.Errorf("PullSpec = %q, want fallback %q", rel.PullSpec, RegistryCIPullSpec(build))
	}
}

func TestResolveNightlyRejectsNonNightly(t *testing.T) {
	if _, err := ResolveNightly(context.Background(), "4.22.0-ec.5"); err == nil {
		t.Fatal("expected error for non-nightly version")
	}
}

func TestResolveNightlyHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	withReleaseControllerBase(t, srv.URL)

	if _, err := ResolveNightly(context.Background(), "4.23.0-0.nightly"); err == nil {
		t.Fatal("expected error on 404 from release-controller")
	}
}
