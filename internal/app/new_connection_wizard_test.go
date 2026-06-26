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

// ---- DSN mode tests ----

// TestWizardStepModeToDSN verifies that selecting "DSN string" at StepMode
// advances to StepDSN.
func TestWizardStepModeToDSN(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	// DSN option is index 1; move down then Enter.
	model = pressKey(model, "ctrl+n") // select DSN (index 1)
	model = pressKey(model, "enter")

	w := currentWizard(t, model)
	if got, want := w.step, StepDSN; got != want {
		t.Fatalf("step = %q, want %q after selecting DSN mode", got, want)
	}
	// wizardMode should be "dsn".
	if got, want := w.wizardMode, "dsn"; got != want {
		t.Fatalf("wizardMode = %q, want %q", got, want)
	}
}

// TestWizardDSNValidParsesOnEnter verifies that a valid DSN parses on Enter
// and advances to StepSaveLocation with the parsed connection stored.
func TestWizardDSNValidParsesOnEnter(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	dsn := "postgres://alice:secret@db.example.com/mydb"
	model = typeText(model, dsn)
	model = pressKey(model, "enter") // parse + advance

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q after valid DSN", got, want)
	}
	if w.dsnError != "" {
		t.Fatalf("dsnError = %q, want empty after valid DSN", w.dsnError)
	}
	// Parsed connection should be stored.
	if got, want := w.dsnParsedConn.Type, "postgres"; got != want {
		t.Fatalf("dsnParsedConn.Type = %q, want %q", got, want)
	}
	if got, want := w.dsnParsedConn.Host, "db.example.com"; got != want {
		t.Fatalf("dsnParsedConn.Host = %q, want %q", got, want)
	}
	if got, want := w.dsnParsedConn.Database, "mydb"; got != want {
		t.Fatalf("dsnParsedConn.Database = %q, want %q", got, want)
	}
	// Derived name should be pre-filled: "<db>@<host>".
	if got, want := w.dsnName, "mydb@db.example.com"; got != want {
		t.Fatalf("dsnName = %q, want %q (derived default)", got, want)
	}
}

// TestWizardDSNInvalidShowsErrorStaysOnStep verifies that an invalid DSN
// shows an inline error and stays on StepDSN.
func TestWizardDSNInvalidShowsErrorStaysOnStep(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	model = typeText(model, "not-a-connection-string")
	model = pressKey(model, "enter") // attempt parse

	w := currentWizard(t, model)
	if got, want := w.step, StepDSN; got != want {
		t.Fatalf("step = %q, want %q (should stay on StepDSN after invalid DSN)", got, want)
	}
	if w.dsnError == "" {
		t.Fatal("dsnError should be set after invalid DSN")
	}
}

// TestWizardDSNNoErrorWhileTyping verifies that typing into the DSN field
// does NOT produce a live validation error — only Enter triggers parsing.
func TestWizardDSNNoErrorWhileTyping(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	// First enter an invalid DSN to set an error...
	model = typeText(model, "bad")
	model = pressKey(model, "enter") // sets dsnError

	w := currentWizard(t, model)
	if w.dsnError == "" {
		t.Fatal("expected dsnError to be set after invalid DSN enter")
	}

	// ...then type a character — error should clear immediately, not re-validate.
	model = typeText(model, "x")
	w = currentWizard(t, model)
	if w.dsnError != "" {
		t.Fatalf("dsnError = %q after typing, want empty (no live validation)", w.dsnError)
	}
}

// TestDerivedNameFromConnection is a table-driven test for the derivedNameFromConnection helper.
func TestDerivedNameFromConnection(t *testing.T) {
	cases := []struct {
		name string
		conn config.Connection
		want string
	}{
		{
			name: "postgres db@host",
			conn: config.Connection{Type: "postgres", Host: "localhost", Database: "appdb"},
			want: "appdb@localhost",
		},
		{
			name: "mysql db@host",
			conn: config.Connection{Type: "mysql", Host: "db.example.com", Database: "shop"},
			want: "shop@db.example.com",
		},
		{
			name: "sqlite basename strip ext",
			conn: config.Connection{Type: "sqlite", Database: "/var/db/app.sqlite"},
			want: "app",
		},
		{
			name: "sqlite :memory:",
			conn: config.Connection{Type: "sqlite", Database: ":memory:"},
			want: "memory",
		},
		{
			name: "sqlite no extension",
			conn: config.Connection{Type: "sqlite", Database: "/data/mydb"},
			want: "mydb",
		},
		{
			name: "sqlite nested path with extension",
			conn: config.Connection{Type: "sqlite", Database: "/path/to/test.db"},
			want: "test",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := derivedNameFromConnection(tc.conn)
			if got != tc.want {
				t.Fatalf("derivedNameFromConnection(%+v) = %q, want %q", tc.conn, got, tc.want)
			}
		})
	}
}

// TestWizardDSNNameCollisionRefused verifies that submitting a colliding name
// in DSN mode shows an inline error and stays on StepSaveLocation.
func TestWizardDSNNameCollisionRefused(t *testing.T) {
	connections := config.Connections{
		Connection: map[string]config.Connection{
			"existing": {Type: "sqlite", Database: ":memory:"},
		},
	}
	model := buildWizardModel(t, connections, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	model = typeText(model, "postgres://alice@db.example.com/mydb")
	model = pressKey(model, "enter") // → StepSaveLocation (derived name: "mydb@db.example.com")

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	// Clear the pre-filled name and type the colliding name.
	for range w.dsnName {
		model = pressKey(model, "backspace")
	}
	model = typeText(model, "existing")
	model = pressKey(model, "ctrl+n") // move to Project location (avoids real global write)
	model = pressKey(model, "enter")  // attempt submit

	w = currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q (should stay on StepSaveLocation after collision)", got, want)
	}
	if !containsString(w.dsnNameError, "already exists") {
		t.Fatalf("dsnNameError = %q, want to mention 'already exists'", w.dsnNameError)
	}

	// Editing to a unique name should allow submit.
	model = pressKey(model, "backspace") // remove last char to make it unique
	w = currentWizard(t, model)
	if w.dsnNameError != "" {
		t.Fatalf("dsnNameError = %q after edit, want empty (cleared on typing)", w.dsnNameError)
	}
}

// TestWizardDSNEscBackPreservesDSNText verifies that Esc from StepSaveLocation
// back to StepDSN preserves the previously entered DSN text.
func TestWizardDSNEscBackPreservesDSNText(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	dsn := "postgres://alice@db.example.com/mydb"
	model = typeText(model, dsn)
	model = pressKey(model, "enter") // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	// Esc back to StepDSN.
	model = pressKey(model, "esc")
	w = currentWizard(t, model)
	if got, want := w.step, StepDSN; got != want {
		t.Fatalf("step = %q, want %q after Esc from StepSaveLocation", got, want)
	}
	if got, want := w.dsnText, dsn; got != want {
		t.Fatalf("dsnText = %q, want %q (DSN text must be preserved on Esc-back)", got, want)
	}
}

// TestWizardDSNEscFromStepDSNGoesToMode verifies that Esc on StepDSN returns
// to StepMode (not StepSaveLocation or dismissing the wizard).
func TestWizardDSNEscFromStepDSNGoesToMode(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	model = pressKey(model, "esc")
	w := currentWizard(t, model)
	if got, want := w.step, StepMode; got != want {
		t.Fatalf("step = %q, want %q after Esc from StepDSN", got, want)
	}
	// Wizard should still be open.
	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatal("wizard should still be open after Esc from StepDSN")
	}
}

// TestWizardDSNSaveLocationShowsParsedSummary verifies that StepSaveLocation in
// DSN mode renders the parsed connection details (password masked) in the body.
func TestWizardDSNSaveLocationShowsParsedSummary(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "ctrl+n") // select DSN
	model = pressKey(model, "enter")  // → StepDSN

	model = typeText(model, "postgres://alice:secret@db.example.com/mydb")
	model = pressKey(model, "enter") // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	rendered := model.currentModal().Render(model.state.Interaction, 80)

	// Must show parsed fields.
	if !containsString(rendered, "postgres") {
		t.Fatalf("Render() = %q, want to contain 'postgres'", rendered)
	}
	if !containsString(rendered, "db.example.com") {
		t.Fatalf("Render() = %q, want to contain host 'db.example.com'", rendered)
	}
	if !containsString(rendered, "mydb") {
		t.Fatalf("Render() = %q, want to contain database 'mydb'", rendered)
	}
	// Password must be masked.
	if containsString(rendered, "secret") {
		t.Fatalf("Render() contains plaintext password 'secret'; want masked")
	}
	if !containsString(rendered, "****") {
		t.Fatalf("Render() = %q, want '****' as password mask", rendered)
	}
	// Name field is in the filter area, not the body — don't assert it here.
}

// TestNewConnectionWizardEndToEndDSN drives the full DSN wizard flow:
//  1. Startup picker with no connections.
//  2. Open wizard, select DSN mode.
//  3. Enter a postgres DSN → StepSaveLocation; name is derived as "<db>@<host>".
//  4. Navigate to Project location, submit.
//  5. connections.toml written with a quoted TOML key (name contains @).
//  6. Picker rebuilt with the new connection; success notification shown.
func TestNewConnectionWizardEndToEndDSN(t *testing.T) {
	cwd := t.TempDir()

	cache := config.Connections{}
	reloadCalled := false
	connName := "testdb@pg.example.com" // derived name from DSN

	model := newModelWithDependencies(Session{WorkingDir: cwd}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return cache, nil },
		reloadConnections: func() error {
			if cache.Connection == nil {
				cache.Connection = make(map[string]config.Connection)
			}
			cache.Connection[connName] = config.Connection{
				Type:     "postgres",
				Host:     "pg.example.com",
				Port:     5432,
				Database: "testdb",
				Username: "alice",
			}
			reloadCalled = true
			return nil
		},
	})
	next, _ := model.Update(pickerInitMsg{})
	model = next.(Model)

	// ---- Open wizard ----
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("expected wizard modal, got %v", model.currentModal())
	}

	// ---- Select DSN mode ----
	model = pressKey(model, "ctrl+n") // DSN is index 1
	model = pressKey(model, "enter")  // → StepDSN

	// ---- Enter valid postgres DSN ----
	model = typeText(model, "postgres://alice@pg.example.com/testdb")
	model = pressKey(model, "enter") // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
	// Derived name should be "testdb@pg.example.com".
	if got, want := w.dsnName, "testdb@pg.example.com"; got != want {
		t.Fatalf("dsnName = %q, want %q", got, want)
	}

	// ---- Navigate to Project (index 1) and submit ----
	model = pressKey(model, "ctrl+n") // Project location
	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Enter on SaveLocation cmd = nil")
	}

	// ---- Drive async write ----
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

	writeMsgs := collectCommandMessagesForTest(t, writeCmd)
	for _, m := range writeMsgs {
		if f, ok := m.(writeConnectionFailedMsg); ok {
			t.Fatalf("write failed: %v (path: %q)", f.err, f.path)
		}
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	// ---- Verify connections.toml ----
	targetPath := filepath.Join(cwd, "connections.toml")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
	}
	tomlContent := string(data)

	// Name "testdb@pg.example.com" contains @ so it must be written as a quoted TOML key.
	if !strings.Contains(tomlContent, `"testdb@pg.example.com"`) {
		t.Fatalf("connections.toml = %q, want quoted TOML key for name with @", tomlContent)
	}
	if !strings.Contains(tomlContent, "postgres") {
		t.Fatalf("connections.toml missing type 'postgres': %q", tomlContent)
	}
	if !strings.Contains(tomlContent, "pg.example.com") {
		t.Fatalf("connections.toml missing host: %q", tomlContent)
	}
	if !strings.Contains(tomlContent, "testdb") {
		t.Fatalf("connections.toml missing database: %q", tomlContent)
	}

	// ---- Reload and picker rebuild ----
	if !reloadCalled {
		t.Fatal("reloadConnections was not called after successful write")
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConnectionPicker {
		t.Fatalf("expected connectionPickerModal after wizard close, got %v", model.currentModal())
	}
	pm, ok := model.currentModal().(*connectionPickerModal)
	if !ok {
		t.Fatalf("currentModal() = %T, want *connectionPickerModal", model.currentModal())
	}
	found := false
	for _, name := range pm.candidates {
		if name == connName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("picker candidates = %v, want to include %q", pm.candidates, connName)
	}

	// ---- Success notification ----
	if !containsString(model.state.Notification.Text, connName) {
		t.Fatalf("notification = %q, want to mention %q", model.state.Notification.Text, connName)
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

// ---- postgres/mysql field sequence tests ----

// advanceToFieldStep navigates the wizard to StepField for the given type index
// (0 = sqlite, 1 = postgres, 2 = mysql).  It selects "step-by-step" mode,
// enters name, selects the type, and returns the model at the first StepField screen.
func advanceToFieldStep(model Model, name string, typeIndex int) Model {
	model = pressKey(model, "enter") // Mode → StepName
	model = typeText(model, name)
	model = pressKey(model, "enter") // → StepType
	for i := 0; i < typeIndex; i++ {
		model = pressKey(model, "ctrl+n")
	}
	model = pressKey(model, "enter") // → StepField
	return model
}

// TestWizardPostgresFieldSequence verifies that selecting postgres walks through
// Host → Port → Database → Username → Password → SSHHost, one field per screen.
func TestWizardPostgresFieldSequence(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1) // postgres = index 1

	w := currentWizard(t, model)
	if got, want := w.step, StepField; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	// Field 0: Host
	if got, want := w.fieldIndex, 0; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Host)", got, want)
	}
	label := model.currentModal().FilterLabel()
	if !containsString(label, "Host") {
		t.Fatalf("FilterLabel() = %q, want to contain 'Host'", label)
	}

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter")

	// Field 1: Port — pre-filled with 5432
	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 1; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Port)", got, want)
	}
	if got, want := w.fieldValue, "5432"; got != want {
		t.Fatalf("fieldValue (port prefill) = %q, want %q", got, want)
	}
	model = pressKey(model, "enter") // accept prefill

	// Field 2: Database
	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 2; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Database)", got, want)
	}
	model = typeText(model, "mydb")
	model = pressKey(model, "enter")

	// Field 3: Username
	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 3; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Username)", got, want)
	}
	model = typeText(model, "alice")
	model = pressKey(model, "enter")

	// Field 4: Password (optional)
	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 4; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Password)", got, want)
	}
	model = pressKey(model, "enter") // skip (empty = optional)

	// Field 5: SSH Host (optional)
	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 5; got != want {
		t.Fatalf("fieldIndex = %d, want %d (SSHHost)", got, want)
	}
	model = pressKey(model, "enter") // skip

	// Should now be at StepSaveLocation.
	w = currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q after all postgres fields", got, want)
	}
}

// TestWizardMysqlPortPrefill verifies mysql port is pre-filled with 3306.
func TestWizardMysqlPortPrefill(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "my", 2) // mysql = index 2

	w := currentWizard(t, model)
	if got, want := w.fieldIndex, 0; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Host)", got, want)
	}
	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port

	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 1; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Port)", got, want)
	}
	if got, want := w.fieldValue, "3306"; got != want {
		t.Fatalf("fieldValue (mysql port prefill) = %q, want %q", got, want)
	}
}

// TestWizardPortValidationNonNumeric verifies a non-numeric port is refused inline.
func TestWizardPortValidationNonNumeric(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1) // postgres

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port (prefilled "5432")

	// Clear the prefill and type a non-numeric value.
	for range "5432" {
		model = pressKey(model, "backspace")
	}
	model = typeText(model, "notaport")
	model = pressKey(model, "enter")

	w := currentWizard(t, model)
	if got, want := w.step, StepField; got != want {
		t.Fatalf("step = %q, want %q (should stay on Port)", got, want)
	}
	if got, want := w.fieldIndex, 1; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Port)", got, want)
	}
	if w.fieldError == "" {
		t.Fatal("fieldError should be set after non-numeric port")
	}
}

// TestWizardPortValidationOutOfRange verifies that a port outside [1,65535] is refused.
func TestWizardPortValidationOutOfRange(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1) // postgres

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port

	for range "5432" {
		model = pressKey(model, "backspace")
	}
	model = typeText(model, "99999")
	model = pressKey(model, "enter")

	w := currentWizard(t, model)
	if got, want := w.fieldIndex, 1; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Port)", got, want)
	}
	if w.fieldError == "" {
		t.Fatal("fieldError should be set after out-of-range port")
	}
}

// TestWizardPortValidationValidPort verifies a valid port advances to the next field.
func TestWizardPortValidationValidPort(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1) // postgres

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port

	for range "5432" {
		model = pressKey(model, "backspace")
	}
	model = typeText(model, "5433")
	model = pressKey(model, "enter")

	w := currentWizard(t, model)
	if got, want := w.fieldIndex, 2; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Database) after valid port", got, want)
	}
	if w.fieldError != "" {
		t.Fatalf("fieldError = %q, want empty after valid port", w.fieldError)
	}
}

// TestWizardRequiredFieldsRefuseEmpty verifies Host, Database, and Username refuse empty.
func TestWizardRequiredFieldsRefuseEmpty(t *testing.T) {
	for _, tc := range []struct {
		fieldName   string
		fieldIndex  int
		stepsToHere func(model Model) Model
	}{
		{
			fieldName:  "Host",
			fieldIndex: 0,
			stepsToHere: func(model Model) Model {
				return advanceToFieldStep(model, "pg", 1)
			},
		},
		{
			fieldName:  "Database",
			fieldIndex: 2,
			stepsToHere: func(model Model) Model {
				model = advanceToFieldStep(model, "pg", 1)
				model = typeText(model, "db.example.com")
				model = pressKey(model, "enter") // → Port
				model = pressKey(model, "enter") // accept prefill → Database
				return model
			},
		},
		{
			fieldName:  "Username",
			fieldIndex: 3,
			stepsToHere: func(model Model) Model {
				model = advanceToFieldStep(model, "pg", 1)
				model = typeText(model, "db.example.com")
				model = pressKey(model, "enter") // → Port
				model = pressKey(model, "enter") // → Database
				model = typeText(model, "mydb")
				model = pressKey(model, "enter") // → Username
				return model
			},
		},
	} {
		t.Run(tc.fieldName, func(t *testing.T) {
			model := buildWizardModel(t, config.Connections{}, t.TempDir())
			model = tc.stepsToHere(model)
			model = pressKey(model, "enter") // confirm empty

			w := currentWizard(t, model)
			if got, want := w.step, StepField; got != want {
				t.Fatalf("step = %q, want %q (%s should be refused)", got, want, tc.fieldName)
			}
			if got, want := w.fieldIndex, tc.fieldIndex; got != want {
				t.Fatalf("fieldIndex = %d, want %d (should stay on %s)", got, want, tc.fieldName)
			}
			if w.fieldError == "" {
				t.Fatalf("fieldError should be set after empty %s", tc.fieldName)
			}
		})
	}
}

// TestWizardOptionalPasswordAdvancesEmpty verifies an empty password advances
// and is omitted from the assembled connection.
func TestWizardOptionalPasswordAdvancesEmpty(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1)

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port
	model = pressKey(model, "enter") // accept prefill → Database
	model = typeText(model, "mydb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "alice")
	model = pressKey(model, "enter") // → Password

	w := currentWizard(t, model)
	if got, want := w.fieldIndex, 4; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Password)", got, want)
	}

	model = pressKey(model, "enter") // skip empty password → SSHHost

	w = currentWizard(t, model)
	if got, want := w.fieldIndex, 5; got != want {
		t.Fatalf("fieldIndex = %d after empty password Enter, want %d (SSHHost)", got, want)
	}
	if w.fieldError != "" {
		t.Fatalf("fieldError = %q, want empty for optional password", w.fieldError)
	}

	// Walk through SSHHost (skip) → StepSaveLocation, then check assembled connection.
	model = pressKey(model, "enter") // skip ssh_host

	w = currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	conn := w.buildConnection()
	if conn.Password != "" {
		t.Fatalf("Password = %q, want empty (optional field omitted)", conn.Password)
	}
}

// TestWizardOptionalSSHHostAdvancesEmpty verifies an empty SSH host advances
// and is omitted from the assembled connection.
func TestWizardOptionalSSHHostAdvancesEmpty(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1)

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port
	model = pressKey(model, "enter") // → Database
	model = typeText(model, "mydb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "alice")
	model = pressKey(model, "enter") // → Password
	model = pressKey(model, "enter") // skip → SSHHost
	model = pressKey(model, "enter") // skip → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	conn := w.buildConnection()
	if conn.SSHHost != "" {
		t.Fatalf("SSHHost = %q, want empty (optional field omitted)", conn.SSHHost)
	}
}

// TestWizardPasswordMaskedInRender verifies that the Password field renders as
// bullet characters (•) in FilterText rather than the plaintext.
func TestWizardPasswordMaskedInRender(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1)

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port
	model = pressKey(model, "enter") // → Database
	model = typeText(model, "mydb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "alice")
	model = pressKey(model, "enter") // → Password

	// Type a password
	model = typeText(model, "s3cr3t")

	filterText := model.currentModal().FilterText()
	// Should show bullets, not plaintext.
	if strings.Contains(filterText, "s3cr3t") {
		t.Fatalf("FilterText() = %q contains plaintext password; want masked", filterText)
	}
	// Should contain one bullet per character plus cursor.
	wantBullets := strings.Repeat("•", len("s3cr3t"))
	if !strings.Contains(filterText, wantBullets) {
		t.Fatalf("FilterText() = %q, want %d bullet characters", filterText, len("s3cr3t"))
	}
}

// TestWizardSaveLocationSummaryMasksPassword verifies that the StepSaveLocation
// summary shows "**** " for the password rather than the plaintext.
func TestWizardSaveLocationSummaryMasksPassword(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1)

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → Port
	model = pressKey(model, "enter") // → Database
	model = typeText(model, "mydb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "alice")
	model = pressKey(model, "enter") // → Password
	model = typeText(model, "s3cr3t")
	model = pressKey(model, "enter") // → SSHHost
	model = pressKey(model, "enter") // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	rendered := model.currentModal().Render(model.state.Interaction, 60)
	if strings.Contains(rendered, "s3cr3t") {
		t.Fatalf("Render() contains plaintext password; want masked in summary")
	}
	if !strings.Contains(rendered, "****") {
		t.Fatalf("Render() = %q, want to contain '****' as password mask", rendered)
	}
}

// TestWizardFullPostgresFlowAssemblesConnection verifies the assembled
// config.Connection after walking all postgres fields.
func TestWizardFullPostgresFlowAssemblesConnection(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg-full", 1) // postgres

	model = typeText(model, "pg.example.com")
	model = pressKey(model, "enter") // → Port (prefilled 5432)
	// Clear and enter a custom port.
	for range "5432" {
		model = pressKey(model, "backspace")
	}
	model = typeText(model, "5433")
	model = pressKey(model, "enter") // → Database
	model = typeText(model, "appdb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "pguser")
	model = pressKey(model, "enter") // → Password
	model = typeText(model, "hunter2")
	model = pressKey(model, "enter") // → SSHHost
	model = typeText(model, "bastion.example.com")
	model = pressKey(model, "enter") // → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}

	conn := w.buildConnection()
	if got, want := conn.Type, "postgres"; got != want {
		t.Fatalf("Type = %q, want %q", got, want)
	}
	if got, want := conn.Host, "pg.example.com"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if got, want := conn.Port, 5433; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}
	if got, want := conn.Database, "appdb"; got != want {
		t.Fatalf("Database = %q, want %q", got, want)
	}
	if got, want := conn.Username, "pguser"; got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	if got, want := conn.Password, "hunter2"; got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
	if got, want := conn.SSHHost, "bastion.example.com"; got != want {
		t.Fatalf("SSHHost = %q, want %q", got, want)
	}
}

// TestWizardEscNavigatesBackThroughFields verifies that Esc from field N goes
// back to field N-1 (not all the way to StepType).
func TestWizardEscNavigatesBackThroughFields(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = advanceToFieldStep(model, "pg", 1) // starts at field 0 (Host)

	model = typeText(model, "db.example.com")
	model = pressKey(model, "enter") // → field 1 (Port)

	w := currentWizard(t, model)
	if got, want := w.fieldIndex, 1; got != want {
		t.Fatalf("fieldIndex = %d, want %d before Esc", got, want)
	}

	// Esc from Port → back to Host (field 0), restoring the committed value.
	model = pressKey(model, "esc")

	w = currentWizard(t, model)
	if got, want := w.step, StepField; got != want {
		t.Fatalf("step = %q, want %q (should stay in StepField)", got, want)
	}
	if got, want := w.fieldIndex, 0; got != want {
		t.Fatalf("fieldIndex = %d, want %d (Host) after Esc", got, want)
	}
	if got, want := w.fieldValue, "db.example.com"; got != want {
		t.Fatalf("fieldValue = %q, want %q (committed host restored)", got, want)
	}

	// Esc from field 0 (Host) → StepType.
	model = pressKey(model, "esc")

	w = currentWizard(t, model)
	if got, want := w.step, StepType; got != want {
		t.Fatalf("step = %q, want %q (Esc from first field should go to StepType)", got, want)
	}
}

// TestNewConnectionWizardEndToEndPostgres drives the full postgres wizard flow
// from the startup picker through writing to a project-scoped connections.toml.
// It writes to t.TempDir() (project location) — never to the global config.
func TestNewConnectionWizardEndToEndPostgres(t *testing.T) {
	cwd := t.TempDir()

	cache := config.Connections{}
	reloadCalled := false
	model := newModelWithDependencies(Session{WorkingDir: cwd}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return cache, nil },
		reloadConnections: func() error {
			if cache.Connection == nil {
				cache.Connection = make(map[string]config.Connection)
			}
			cache.Connection["pg-e2e"] = config.Connection{
				Type:     "postgres",
				Host:     "pg.example.com",
				Port:     5432,
				Database: "testdb",
				Username: "pguser",
			}
			reloadCalled = true
			return nil
		},
	})
	next, _ := model.Update(pickerInitMsg{})
	model = next.(Model)

	// Open wizard via the create row.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("expected wizard modal, got %v", model.currentModal())
	}

	// Walk the wizard.
	model = pressKey(model, "enter") // Mode → StepName
	model = typeText(model, "pg-e2e")
	model = pressKey(model, "enter")  // → StepType
	model = pressKey(model, "ctrl+n") // select postgres (index 1)
	model = pressKey(model, "enter")  // → StepField (Host)
	model = typeText(model, "pg.example.com")
	model = pressKey(model, "enter") // → Port (prefilled 5432)
	model = pressKey(model, "enter") // accept prefill → Database
	model = typeText(model, "testdb")
	model = pressKey(model, "enter") // → Username
	model = typeText(model, "pguser")
	model = pressKey(model, "enter") // → Password
	model = pressKey(model, "enter") // skip (empty) → SSHHost
	model = pressKey(model, "enter") // skip (empty) → StepSaveLocation

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
	model = pressKey(model, "ctrl+n") // move to Project (index 1)

	next, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)
	if cmd == nil {
		t.Fatal("Enter on SaveLocation cmd = nil")
	}

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
		t.Fatal("no writeConnectionMsg in cmd messages")
	}

	writeMsgs := collectCommandMessagesForTest(t, writeCmd)
	for _, m := range writeMsgs {
		if f, ok := m.(writeConnectionFailedMsg); ok {
			t.Fatalf("write failed: %v (path: %q)", f.err, f.path)
		}
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	// Verify connections.toml was written to the project dir.
	targetPath := filepath.Join(cwd, "connections.toml")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
	}
	tomlContent := string(data)
	if !strings.Contains(tomlContent, "pg-e2e") {
		t.Fatalf("connections.toml missing connection name 'pg-e2e': %q", tomlContent)
	}
	if !strings.Contains(tomlContent, "postgres") {
		t.Fatalf("connections.toml missing type 'postgres': %q", tomlContent)
	}
	if !strings.Contains(tomlContent, "pg.example.com") {
		t.Fatalf("connections.toml missing host: %q", tomlContent)
	}
	if !strings.Contains(tomlContent, "testdb") {
		t.Fatalf("connections.toml missing database: %q", tomlContent)
	}

	if !reloadCalled {
		t.Fatal("reloadConnections was not called after successful write")
	}
	if !containsString(model.state.Notification.Text, "pg-e2e") {
		t.Fatalf("notification = %q, want to mention 'pg-e2e'", model.state.Notification.Text)
	}
}

// ---- Write-failure inline error tests (#20) ----

// driveWizardSqliteToSaveLocation drives a step-by-step SQLite wizard from StepMode
// to StepSaveLocation, entering the given name and database path.
func driveWizardSqliteToSaveLocation(model Model, name, dbPath string) Model {
	model = pressKey(model, "enter") // StepMode → StepName
	model = typeText(model, name)
	model = pressKey(model, "enter") // → StepType
	model = pressKey(model, "enter") // SQLite (index 0) → StepField
	model = typeText(model, dbPath)
	model = pressKey(model, "enter") // → StepSaveLocation
	return model
}

// driveWriteAttempt submits on StepSaveLocation and drives the async write command
// to completion.  It returns the updated model, whether the write failed, and the
// failure message (if any).
func driveWriteAttempt(t *testing.T, model Model) (Model, bool, writeConnectionFailedMsg) {
	t.Helper()
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	msgs := collectCommandMessagesForTest(t, cmd)
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
		t.Fatal("no writeConnectionMsg in cmd")
	}

	writeMsgs := collectCommandMessagesForTest(t, writeCmd)
	var failMsg writeConnectionFailedMsg
	var failed bool
	for _, m := range writeMsgs {
		if f, ok := m.(writeConnectionFailedMsg); ok {
			failMsg = f
			failed = true
		}
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	return model, failed, failMsg
}

// TestWizardWriteFailureInlineError verifies that a write failure:
//   - surfaces writeError inline on StepSaveLocation containing the attempted path,
//   - keeps the wizard open (does not pop it),
//   - leaves the Connection Picker beneath untouched.
func TestWizardWriteFailureInlineError(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Chmod(cwd, 0o500); err != nil {
		t.Skip("cannot chmod temp dir:", err)
	}
	t.Cleanup(func() { os.Chmod(cwd, 0o700) })

	model := buildWizardModel(t, config.Connections{}, cwd)

	// Snapshot the picker state before the write attempt.
	pickerBefore, ok := model.modals[0].(*connectionPickerModal)
	if !ok {
		t.Fatalf("modals[0] = %T, want *connectionPickerModal", model.modals[0])
	}
	candidatesBefore := append([]string(nil), pickerBefore.candidates...)
	selectedBefore := pickerBefore.selected

	model = driveWizardSqliteToSaveLocation(model, "fail-conn", "fail.db")

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
	// Select Project (index 1) — points to the read-only cwd.
	model = pressKey(model, "ctrl+n")

	model, failed, failMsg := driveWriteAttempt(t, model)
	if !failed {
		t.Skip("write to read-only dir unexpectedly succeeded (filesystem may not enforce permissions)")
	}

	// Wizard must still be the current modal at StepSaveLocation.
	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("expected wizard still open, got %v", model.currentModal())
	}
	w = currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q (wizard must stay open after write failure)", got, want)
	}

	// writeError must be set and contain the attempted path and error text.
	if w.writeError == "" {
		t.Fatal("writeError must be non-empty after write failure")
	}
	if !containsString(w.writeError, failMsg.path) {
		t.Fatalf("writeError = %q, want to contain attempted path %q", w.writeError, failMsg.path)
	}

	// Render must include the error.
	rendered := model.currentModal().Render(model.state.Interaction, 80)
	if !containsString(rendered, "save failed") {
		t.Fatalf("Render() = %q, want to contain 'save failed'", rendered)
	}

	// Picker beneath must be completely untouched.
	pickerAfter, ok := model.modals[0].(*connectionPickerModal)
	if !ok {
		t.Fatalf("modals[0] after failure = %T, want *connectionPickerModal", model.modals[0])
	}
	if pickerAfter.selected != selectedBefore {
		t.Fatalf("picker selected changed after failure: before=%d after=%d", selectedBefore, pickerAfter.selected)
	}
	if len(pickerAfter.candidates) != len(candidatesBefore) {
		t.Fatalf("picker candidates changed after failure: before=%v after=%v", candidatesBefore, pickerAfter.candidates)
	}
}

// TestWizardWriteErrorClearsOnLocationToggle verifies that writeError is cleared
// when the user navigates between save locations.
func TestWizardWriteErrorClearsOnLocationToggle(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = driveWizardSqliteToSaveLocation(model, "clear-conn", "clear.db")

	// Inject a writeError directly (simulates a prior failed write).
	w := currentWizard(t, model)
	w.writeError = "save failed: /some/path: permission denied"

	// Toggle location: Global (0) → Project (1).
	model = pressKey(model, "ctrl+n")

	w = currentWizard(t, model)
	if w.writeError != "" {
		t.Fatalf("writeError = %q after location toggle, want empty (must be cleared)", w.writeError)
	}
}

// TestWizardWriteFailureRetrySuccess verifies that after a write failure, fixing
// the underlying cause and re-submitting reaches the normal success path:
// wizard popped, picker rebuilt with the new connection, success notification.
func TestWizardWriteFailureRetrySuccess(t *testing.T) {
	cwd := t.TempDir()

	cache := config.Connections{}
	reloadCalled := false
	model := newModelWithDependencies(Session{WorkingDir: cwd}, modelDependencies{
		connectionsLoader: func() (config.Connections, error) { return cache, nil },
		reloadConnections: func() error {
			if cache.Connection == nil {
				cache.Connection = make(map[string]config.Connection)
			}
			cache.Connection["retry-conn"] = config.Connection{Type: "sqlite", Database: "retry.db"}
			reloadCalled = true
			return nil
		},
	})
	next, _ := model.Update(pickerInitMsg{})
	model = next.(Model)

	// Push the wizard with the project paths pointing at cwd.
	paths, _ := config.DiscoverConnectionPaths(cwd)
	wizard := newConnectionWizardModalFor(cache, cwd, paths)
	model.pushModal(wizard)

	model = driveWizardSqliteToSaveLocation(model, "retry-conn", "retry.db")

	w := currentWizard(t, model)
	if got, want := w.step, StepSaveLocation; got != want {
		t.Fatalf("step = %q, want %q", got, want)
	}
	// Select Project (index 1).
	model = pressKey(model, "ctrl+n")

	// Make cwd read-only so the first write fails.
	if err := os.Chmod(cwd, 0o500); err != nil {
		t.Skip("cannot chmod temp dir:", err)
	}
	t.Cleanup(func() { os.Chmod(cwd, 0o700) })

	model, failed, _ := driveWriteAttempt(t, model)
	if !failed {
		t.Skip("write to read-only dir unexpectedly succeeded (filesystem may not enforce permissions)")
	}

	// Verify wizard is open with writeError set.
	w = currentWizard(t, model)
	if w.writeError == "" {
		t.Fatal("writeError should be set after first write failure")
	}

	// Restore write permissions.
	if err := os.Chmod(cwd, 0o700); err != nil {
		t.Fatalf("cannot restore cwd permissions: %v", err)
	}

	// Retry: press Enter again on StepSaveLocation.
	model, failed, failMsg := driveWriteAttempt(t, model)
	if failed {
		t.Fatalf("retry write failed: %v (path: %q)", failMsg.err, failMsg.path)
	}

	// Wizard must have been popped; picker is now the current modal.
	if model.currentModal() == nil || model.currentModal().Name() != ModalConnectionPicker {
		t.Fatalf("expected connectionPickerModal after successful retry, got %v", model.currentModal())
	}

	// reloadConnections must have been called.
	if !reloadCalled {
		t.Fatal("reloadConnections not called after successful retry")
	}

	// Success notification must mention the connection name.
	if !containsString(model.state.Notification.Text, "retry-conn") {
		t.Fatalf("notification = %q, want to mention 'retry-conn'", model.state.Notification.Text)
	}

	// connections.toml must have been written to the project path.
	targetPath := filepath.Join(cwd, "connections.toml")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("connections.toml not found at %q after successful retry: %v", targetPath, err)
	}
}
