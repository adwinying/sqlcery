package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/config"
)

// ---- Picker "Create a new connection" row tests ----

func TestPickerCreateRowAlwaysVisibleUnderAnyFilter(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
		},
	}
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	// Type a filter that matches nothing in the real candidates.
	for _, ch := range "zzz" {
		next, _ := model.Update(tea.KeyPressMsg{Text: string(ch)})
		model = next.(Model)
	}

	pm := startupPicker(t, model)
	filtered := pickerFilteredCandidates(pm.candidates, pm.filter)
	if len(filtered) != 0 {
		t.Fatalf("expected no real candidates to match filter %q, got %d", pm.filter, len(filtered))
	}

	// The effective list should still be 1 (just the create row).
	// The counter text should reflect "1 of 1".
	counter := pm.CounterText(model.state.Interaction)
	if !containsString(counter, "1 of 1") {
		t.Fatalf("CounterText() = %q, want '1 of 1' when no candidates match", counter)
	}
}

func TestPickerCreateRowIsSoleRowWhenNoCandidates(t *testing.T) {
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{}, nil
		},
	})

	next, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = next.(Model)

	pm := startupPicker(t, model)

	// No real candidates.
	if len(pm.candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(pm.candidates))
	}

	// Counter should be "1 of 1" (just the create row).
	counter := pm.CounterText(model.state.Interaction)
	if !containsString(counter, "1 of 1") {
		t.Fatalf("CounterText() = %q, want '1 of 1' for empty picker", counter)
	}

	// Render should contain the create row text.
	rendered := pm.Render(model.state.Interaction, 60)
	if !containsString(rendered, "Create a new connection") {
		t.Fatalf("Render() = %q, want to contain 'Create a new connection'", rendered)
	}
}

func TestPickerCreateRowOpensWizard(t *testing.T) {
	// With no candidates, the create row is at index 0 (effective list = [create]).
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{}, nil
		},
	})

	// Press Enter: should push the wizard.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Update(Enter) cmd = nil, want openNewConnectionWizardMsg")
	}

	msgs := collectCommandMessagesForTest(t, cmd)
	var found bool
	for _, m := range msgs {
		if _, ok := m.(openNewConnectionWizardMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no openNewConnectionWizardMsg in cmd messages %v", msgs)
	}

	// Drive the message through Update to push the wizard.
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	if model.currentModal() == nil {
		t.Fatal("currentModal() = nil after openNewConnectionWizardMsg, want newConnectionWizardModal")
	}
	if got, want := model.currentModal().Name(), ModalNewConnectionWizard; got != want {
		t.Fatalf("currentModal().Name() = %q, want %q", got, want)
	}
}

func TestPickerCreateRowVisibleAfterRealCandidates(t *testing.T) {
	// With 2 real candidates the effective list is 3 (alpha, beta, create).
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"alpha": {Type: "sqlite", Database: ":memory:"},
			"beta":  {Type: "sqlite", Database: ":memory:"},
		},
	}
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})

	pm := startupPicker(t, model)
	counter := pm.CounterText(model.state.Interaction)
	if !containsString(counter, "of 3") {
		t.Fatalf("CounterText() = %q, want '1 of 3' for 2 candidates + create row", counter)
	}
}

// ---- newConnectionWizardModal unit tests ----

// buildWizardModel constructs a wizard modal with the given connections
// already pushed on top of a startup picker.
func buildWizardModel(t *testing.T, connections config.Connections, cwd string) Model {
	t.Helper()
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return connections, nil },
	})
	paths, _ := config.DiscoverConnectionPaths(cwd)
	wizard := newConnectionWizardModalFor(connections, cwd, paths)
	model.pushModal(wizard)
	return model
}

func currentWizard(t *testing.T, model Model) *newConnectionWizardModal {
	t.Helper()
	w, ok := model.currentModal().(*newConnectionWizardModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *newConnectionWizardModal", model.currentModal())
	}
	return w
}

func pressKey(model Model, keyStr string) Model {
	var msg tea.KeyPressMsg
	switch keyStr {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+n":
		msg = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		msg = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Text: keyStr}
	}
	next, _ := model.Update(msg)
	return next.(Model)
}

func typeText(model Model, text string) Model {
	for _, ch := range text {
		model = pressKey(model, string(ch))
	}
	return model
}

func TestWizardStepModeToName(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	w := currentWizard(t, model)
	if got, want := w.step, StepMode; got != want {
		t.Fatalf("initial step = %q, want %q", got, want)
	}

	// Press Enter on "Step-by-step" (default selected = 0).
	model = pressKey(model, "enter")
	w = currentWizard(t, model)
	if got, want := w.step, StepName; got != want {
		t.Fatalf("step after Enter on Step-by-step = %q, want %q", got, want)
	}
}

func TestWizardStepNameEmptyRejected(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter") // → StepName
	model = pressKey(model, "enter") // confirm empty name

	w := currentWizard(t, model)
	if got, want := w.step, StepName; got != want {
		t.Fatalf("step after empty Enter = %q, want %q (should stay on Name)", got, want)
	}
	if w.nameError == "" {
		t.Fatal("nameError should be set after attempting to confirm empty name")
	}
}

func TestWizardStepNameDuplicateRejected(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"existing": {Type: "sqlite", Database: "e.db"},
		},
	}
	model := buildWizardModel(t, connections, t.TempDir())
	model = pressKey(model, "enter") // → StepName
	model = typeText(model, "existing")
	model = pressKey(model, "enter") // confirm duplicate name

	w := currentWizard(t, model)
	if got, want := w.step, StepName; got != want {
		t.Fatalf("step after duplicate Enter = %q, want %q", got, want)
	}
	if !containsString(w.nameError, "already exists") {
		t.Fatalf("nameError = %q, want to mention 'already exists'", w.nameError)
	}
}

func TestWizardStepNameToType(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter")   // → StepName
	model = typeText(model, "my-conn") // type a valid name
	model = pressKey(model, "enter")   // → StepType

	w := currentWizard(t, model)
	if got, want := w.step, StepType; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
}

func TestWizardStepTypeToField(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter") // → StepName
	model = typeText(model, "mydb")  // name
	model = pressKey(model, "enter") // → StepType
	// SQLite is the first option (index 0), already selected.
	model = pressKey(model, "enter") // → StepField

	w := currentWizard(t, model)
	if got, want := w.step, StepField; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
}

func TestWizardStepFieldEmptyRejected(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter") // → StepName
	model = typeText(model, "mydb")  // name
	model = pressKey(model, "enter") // → StepType
	model = pressKey(model, "enter") // → StepField (SQLite)
	model = pressKey(model, "enter") // confirm empty field

	w := currentWizard(t, model)
	if got, want := w.step, StepField; got != want {
		t.Fatalf("step after empty field = %q, want %q", got, want)
	}
	if w.fieldError == "" {
		t.Fatal("fieldError should be set after empty field confirm")
	}
}

func TestWizardFullStepSequenceSqlite(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter")     // Mode → Step-by-step → StepName
	model = typeText(model, "test-conn") // StepName
	model = pressKey(model, "enter")     // → StepType
	model = pressKey(model, "enter")     // → StepField (SQLite)
	model = typeText(model, "~/test.db") // StepField
	model = pressKey(model, "enter")     // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q after completing all fields", got, want)
	}
	if got, want := strings.TrimSpace(w.name), "test-conn"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := w.fieldValue, "~/test.db"; got != want {
		t.Fatalf("fieldValue = %q, want %q", got, want)
	}
}

func TestWizardEscNavigatesBack(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter") // → StepName
	model = pressKey(model, "esc")   // → back to StepMode

	w := currentWizard(t, model)
	if got, want := w.step, StepMode; got != want {
		t.Fatalf("step after esc from StepName = %q, want %q", got, want)
	}
}

func TestWizardEscOnModeClosesWizard(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// Wizard is on top; Esc from StepMode should close the wizard.
	model = pressKey(model, "esc")

	if model.currentModal() != nil && model.currentModal().Name() == ModalNewConnectionWizard {
		t.Fatal("wizard should be dismissed after Esc on StepMode")
	}
}

func TestWizardEscClearsFilterBeforeGoingBack(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	// Type into the mode filter.
	model = typeText(model, "step")

	w := currentWizard(t, model)
	if w.modeFilter == "" {
		t.Fatal("modeFilter should be set after typing")
	}

	// First Esc clears the filter.
	model = pressKey(model, "esc")
	w = currentWizard(t, model)
	if w.modeFilter != "" {
		t.Fatalf("modeFilter = %q after Esc, want empty (filter cleared)", w.modeFilter)
	}
	if w.step != StepMode {
		t.Fatalf("step = %q, want StepMode (should not navigate back yet)", w.step)
	}

	// Second Esc dismisses.
	model = pressKey(model, "esc")
	if model.currentModal() != nil && model.currentModal().Name() == ModalNewConnectionWizard {
		t.Fatal("wizard should be dismissed after second Esc")
	}
}

func TestWizardDSNModeShowsNotImplemented(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	// Move to DSN (index 1) then press Enter.
	model = pressKey(model, "ctrl+n") // move to index 1 (DSN)
	model = pressKey(model, "enter")

	w := currentWizard(t, model)
	// Should stay on StepMode and not advance.
	if got, want := w.step, StepMode; got != want {
		t.Fatalf("step = %q, want %q (DSN is stubbed)", got, want)
	}
	// Notification should mention "not yet implemented".
	if !containsString(model.state.Notification.Text, "not yet implemented") {
		t.Fatalf("notification = %q, want to mention 'not yet implemented'", model.state.Notification.Text)
	}
}

// ---- End-to-end: picker → wizard → write → picker rebuilt ----

// TestNewConnectionWizardEndToEndSqlite drives the full flow:
//  1. Startup picker with no connections.
//  2. Select "Create a new connection" (create row is at index 0).
//  3. Walk through the wizard: Mode → Name → Type (sqlite) → Field → SaveLocation.
//  4. Submit → connection written to connections.toml.
//  5. connections.toml is readable and contains the new connection.
//  6. Picker is rebuilt with the new connection selected.
func TestNewConnectionWizardEndToEndSqlite(t *testing.T) {
	cwd := t.TempDir()
	dbPath := filepath.Join(cwd, "mytest.db")

	// Mutable cache cell shared between loader and reloader.
	cache := config.Connections{}

	// reloadCalled tracks that handleWriteConnectionSuccess called reloadConnections.
	// The closure deliberately does NOT call config.LoadConnections: that would read
	// the user's real global config file, which may be invalid or contain leftovers
	// from prior test runs. File-write correctness is verified by os.ReadFile below.
	// We update the shared cache manually so the picker rebuild after the wizard
	// closes can find the new connection.
	reloadCalled := false
	model := newModelWithDependencies(Session{WorkingDir: cwd}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return cache, nil },
		reloadConnections: func() error {
			if cache.Connection == nil {
				cache.Connection = make(map[string]config.Connection)
			}
			cache.Connection["e2e-conn"] = config.Connection{Type: "sqlite", Database: dbPath}
			reloadCalled = true
			return nil
		},
	})
	// Drive pickerInitMsg to push the startup picker.
	next, _ := model.Update(pickerInitMsg{})
	model = next.(Model)

	// ---- Step 1: select the create row (only row, index 0) ----
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Enter on create row cmd = nil")
	}
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("expected wizard modal, got %v", model.currentModal())
	}

	// ---- Step 2: Mode → Step-by-step ----
	model = pressKey(model, "enter") // → StepName

	// ---- Step 3: Name ----
	model = typeText(model, "e2e-conn")
	model = pressKey(model, "enter") // → StepType

	// ---- Step 4: Type → SQLite (index 0) ----
	model = pressKey(model, "enter") // → StepField

	// ---- Step 5: Field → database path ----
	model = typeText(model, dbPath)
	model = pressKey(model, "enter") // → StepSaveLocation

	// ---- Step 6: SaveLocation → navigate to "Project" (index 1) ----
	// This writes to cwd/connections.toml (the temp dir) rather than the
	// user's actual global config dir, keeping the test self-contained.
	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("expected StepSaveLocation, got %q", got)
	}
	model = pressKey(model, "ctrl+n") // move from Global (0) to Project (1)

	// Press Enter to submit.
	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Enter on SaveLocation cmd = nil")
	}

	// ---- Step 7: drive the async write ----
	msgs = collectCommandMessagesForTest(t, cmd)
	var writeCmd tea.Cmd
	for _, m := range msgs {
		if _, ok := m.(writeConnectionMsg); ok {
			next2, c := model.Update(m)
			model = next2.(Model)
			writeCmd = c
			break
		}
	}
	if writeCmd == nil {
		t.Fatal("no writeConnectionMsg found in cmd messages")
	}

	// Execute the write cmd (returns writeConnectionSuccessMsg or writeConnectionFailedMsg).
	writeMsgs := collectCommandMessagesForTest(t, writeCmd)
	for _, m := range writeMsgs {
		if f, ok := m.(writeConnectionFailedMsg); ok {
			t.Fatalf("write failed: %v (path: %q)", f.err, f.path)
		}
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	// ---- Step 8: verify connections.toml was written to cwd (project path) ----
	targetPath := filepath.Join(cwd, "connections.toml")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
	}
	tomlContent := string(data)
	if !strings.Contains(tomlContent, "e2e-conn") {
		t.Fatalf("connections.toml = %q, want to contain connection name 'e2e-conn'", tomlContent)
	}
	if !strings.Contains(tomlContent, dbPath) {
		t.Fatalf("connections.toml = %q, want to contain database path %q", tomlContent, dbPath)
	}

	// ---- Step 9: reloadConnections was called ----
	if !reloadCalled {
		t.Fatal("reloadConnections was not called after successful write")
	}

	// ---- Step 10: picker was rebuilt and new connection is selected ----
	// Wizard should have been popped; picker is the current modal.
	if model.currentModal() == nil || model.currentModal().Name() != ModalConnectionPicker {
		t.Fatalf("expected connectionPickerModal after wizard close, got %v", model.currentModal())
	}
	pm, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}
	found := false
	for _, name := range pm.candidates {
		if name == "e2e-conn" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("picker candidates = %v, want to include 'e2e-conn'", pm.candidates)
	}
	// The selected row should point at "e2e-conn".
	filtered := pickerFilteredCandidates(pm.candidates, "")
	if pm.selected >= len(filtered) || filtered[pm.selected] != "e2e-conn" {
		selectedName := "(create row)"
		if pm.selected < len(filtered) {
			selectedName = filtered[pm.selected]
		}
		t.Fatalf("picker selected = %q (%d), want 'e2e-conn'", selectedName, pm.selected)
	}

	// ---- Step 11: success notification is set ----
	if !containsString(model.state.Notification.Text, "e2e-conn") {
		t.Fatalf("notification = %q, want to mention 'e2e-conn'", model.state.Notification.Text)
	}
}
