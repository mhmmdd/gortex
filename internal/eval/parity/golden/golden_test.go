package golden

import "testing"

// TestPortedExtractionCapabilities runs every registered ported-capability
// golden. Each sub-test names the capability so a regression report reads like
// a feature checklist.
func TestPortedExtractionCapabilities(t *testing.T) {
	if len(capabilities) == 0 {
		t.Fatal("no ported capabilities registered — the golden fence would pass vacuously")
	}
	seen := map[string]bool{}
	for _, c := range capabilities {
		c := c
		if seen[c.Name] {
			t.Errorf("duplicate capability name %q", c.Name)
		}
		seen[c.Name] = true
		t.Run(c.Name, func(t *testing.T) {
			missingNodes, missingEdges, err := c.check()
			if err != nil {
				t.Fatalf("extract failed: %v", err)
			}
			for _, n := range missingNodes {
				t.Errorf("capability regressed — missing %s", n)
			}
			for _, e := range missingEdges {
				t.Errorf("capability regressed — missing %s", e)
			}
		})
	}
}
