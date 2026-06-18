package main

import "testing"

// TestWizardTelemetryToggle verifies the install-only telemetry toggle is added
// when showTelemetry is set, round-trips through collectChoices, and stays
// absent for the plain init wizard.
func TestWizardTelemetryToggle(t *testing.T) {
	// Install path: showTelemetry adds a 4th toggle, seeded from defaults.
	m := newInitWizardModel("x", nil, nil, initDefaults{hooks: true, showTelemetry: true, telemetry: false})
	if len(m.options.Toggles) != 4 {
		t.Fatalf("showTelemetry should add a 4th toggle, got %d", len(m.options.Toggles))
	}
	if got := m.options.Toggles[3].Label; got != "Anonymous telemetry" {
		t.Errorf("4th toggle label = %q, want \"Anonymous telemetry\"", got)
	}
	// The hooks/analyze/skills positional read-back is unaffected.
	m.options.Toggles[0].On = true
	m.options.Toggles[3].On = true // user opts in
	m.collectChoices()
	if !m.hooks {
		t.Error("hooks toggle read-back regressed")
	}
	if !m.telemetry {
		t.Error("collectChoices did not read the telemetry toggle")
	}

	// Init path: no showTelemetry, so no telemetry toggle and it stays off.
	m2 := newInitWizardModel("x", nil, nil, initDefaults{hooks: true})
	if len(m2.options.Toggles) != 3 {
		t.Fatalf("init wizard should have exactly 3 toggles, got %d", len(m2.options.Toggles))
	}
	m2.collectChoices()
	if m2.telemetry {
		t.Error("telemetry must stay false when no toggle is shown")
	}
}
