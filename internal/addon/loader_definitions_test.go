package addon

import "testing"

// TestShippedDefinitionsLoad guards the YAML files in definitions/ against typos
// and schema drift: every shipped addon must parse and pass validation.
func TestShippedDefinitionsLoad(t *testing.T) {
	loader := NewLoader("definitions")

	addons, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() failed: %v", err)
	}

	seen := make(map[string]bool, len(addons))
	for _, a := range addons {
		if seen[a.ID] {
			t.Errorf("duplicate addon id %q", a.ID)
		}
		seen[a.ID] = true
	}

	for _, id := range []string{"rhoai", "odf", "rhwa", "cnv"} {
		if !seen[id] {
			t.Errorf("expected addon %q to be present in definitions/", id)
		}
	}
}
