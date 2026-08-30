package installer

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPermissiveImagePolicyManifests(t *testing.T) {
	manifests := PermissiveImagePolicyManifests()

	// Expect one MachineConfig per node role.
	for _, name := range []string{
		"99-master-ocpctl-permissive-image-policy.yaml",
		"99-worker-ocpctl-permissive-image-policy.yaml",
	} {
		content, ok := manifests[name]
		if !ok {
			t.Fatalf("missing manifest %q; got %v", name, keys(manifests))
		}
		if !strings.Contains(content, "kind: MachineConfig") {
			t.Errorf("%s: not a MachineConfig:\n%s", name, content)
		}
		if !strings.Contains(content, "path: /etc/containers/policy.json") {
			t.Errorf("%s: does not target policy.json:\n%s", name, content)
		}
		if !strings.Contains(content, "overwrite: true") {
			t.Errorf("%s: must overwrite the existing policy.json:\n%s", name, content)
		}

		// The embedded, base64-encoded policy must disable signature enforcement.
		src := manifestSourceB64(t, content)
		decoded, err := base64.StdEncoding.DecodeString(src)
		if err != nil {
			t.Fatalf("%s: decode source data: %v", name, err)
		}
		if !strings.Contains(string(decoded), "insecureAcceptAnything") {
			t.Errorf("%s: policy is not permissive:\n%s", name, decoded)
		}
		if strings.Contains(string(decoded), "sigstoreSigned") {
			t.Errorf("%s: policy still enforces signatures:\n%s", name, decoded)
		}
	}

	if len(manifests) != 2 {
		t.Errorf("expected 2 manifests, got %d", len(manifests))
	}
}

func TestAddOpenShiftManifest(t *testing.T) {
	i := &Installer{}
	if len(i.extraManifests) != 0 {
		t.Fatalf("new installer should have no extra manifests")
	}
	i.AddOpenShiftManifest("a.yaml", "content-a")
	i.AddOpenShiftManifest("b.yaml", "content-b")
	if got := i.extraManifests["a.yaml"]; got != "content-a" {
		t.Errorf("a.yaml = %q, want content-a", got)
	}
	if got := i.extraManifests["b.yaml"]; got != "content-b" {
		t.Errorf("b.yaml = %q, want content-b", got)
	}
}

// manifestSourceB64 extracts the base64 payload from the Ignition file's
// `source: data:...;base64,<PAYLOAD>` line.
func manifestSourceB64(t *testing.T, content string) string {
	t.Helper()
	const marker = ";base64,"
	idx := strings.Index(content, marker)
	if idx < 0 {
		t.Fatalf("no base64 data source in manifest:\n%s", content)
	}
	rest := content[idx+len(marker):]
	return strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
