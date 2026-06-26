package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/tui"
)

// NewConnectionWizardStep identifies the active step in the New Connection Wizard.
type NewConnectionWizardStep string

const (
	StepMode         NewConnectionWizardStep = "mode"
	StepName         NewConnectionWizardStep = "name"
	StepType         NewConnectionWizardStep = "type"
	StepField        NewConnectionWizardStep = "field"
	StepDSN          NewConnectionWizardStep = "dsn"
	StepSaveLocation NewConnectionWizardStep = "save-location"
)

type newConnectionWizardMode struct {
	key   string
	label string
	desc  string
}

type newConnectionWizardType struct {
	key   string
	label string
}

type newConnectionWizardField struct {
	key          string
	label        string
	hint         string
	required     bool
	defaultValue string // pre-fill text shown when entering this field (e.g. "5432" for port)
	masked       bool   // true for Password — renders as • in the filter input
	isPort       bool   // true for Port — validates with strconv.Atoi + config.ValidatePort
}

// wizardModes lists the connection creation modes in display order.
var wizardModes = []newConnectionWizardMode{
	{
		key:   "step-by-step",
		label: "Step-by-step",
		desc:  "Enter fields one at a time",
	},
	{
		key:   "dsn",
		label: "DSN string",
		desc:  "Paste a connection string like postgres://user@host/db",
	},
}

// wizardTypes lists the supported database types in display order.
var wizardTypes = []newConnectionWizardType{
	{key: "sqlite", label: "SQLite"},
	{key: "postgres", label: "PostgreSQL"},
	{key: "mysql", label: "MySQL"},
}

// wizardFieldsByType maps a database type key to the ordered list of fields
// the wizard collects for that type.  The field machinery is data-driven:
// sqlite = [Database]; postgres/mysql = Host → Port → Database → Username → Password → SSHHost.
var wizardFieldsByType = map[string][]newConnectionWizardField{
	"sqlite": {
		{
			key:      "database",
			label:    "Database",
			hint:     "File path; ~ is expanded to home directory",
			required: true,
		},
	},
	"postgres": {
		{key: "host", label: "Host", hint: "Database server hostname or IP address", required: true},
		{key: "port", label: "Port", hint: "Database server port", required: true, defaultValue: "5432", isPort: true},
		{key: "database", label: "Database", hint: "Database name", required: true},
		{key: "username", label: "Username", hint: "Database username", required: true},
		{key: "password", label: "Password", hint: "Database password (leave empty to skip)", required: false, masked: true},
		{key: "ssh_host", label: "SSH Host", hint: "SSH tunnel host (leave empty to skip)", required: false},
	},
	"mysql": {
		{key: "host", label: "Host", hint: "Database server hostname or IP address", required: true},
		{key: "port", label: "Port", hint: "Database server port", required: true, defaultValue: "3306", isPort: true},
		{key: "database", label: "Database", hint: "Database name", required: true},
		{key: "username", label: "Username", hint: "Database username", required: true},
		{key: "password", label: "Password", hint: "Database password (leave empty to skip)", required: false, masked: true},
		{key: "ssh_host", label: "SSH Host", hint: "SSH tunnel host (leave empty to skip)", required: false},
	},
}

// newConnectionWizardModal implements Modal for the New Connection Wizard.
// It owns all step state; it never mutates *Model directly and returns
// ModalResult for every action.  Mirrors slashWizardModal's conventions.
//
// Step flows:
//
//	Step-by-step: StepMode → StepName → StepType → StepField → StepSaveLocation
//	DSN:          StepMode → StepDSN → StepSaveLocation
//
// Esc navigates backward; in filter steps a non-empty filter is cleared first.
// On StepSaveLocation submit the wizard returns modalResultForward{writeConnectionMsg}.
type newConnectionWizardModal struct {
	step NewConnectionWizardStep

	// wizardMode distinguishes "step-by-step" from "dsn".
	wizardMode string

	// StepMode — filterable two-option list
	modeFilter   string
	selectedMode int
	vpMode       int

	// StepName — text-input (filter box repurposed)
	name      string
	nameError string

	// StepType — filterable list
	typeFilter   string
	selectedType int
	vpType       int

	// StepField — text-input (filter box repurposed); one field per screen.
	// fieldIndex is the position within the type's wizardFieldsByType slice.
	// fieldValues holds committed raw (unmasked) values keyed by field.key.
	// fieldValue holds the in-progress value for the current field.
	fieldIndex  int
	fieldValues map[string]string
	fieldValue  string
	fieldError  string

	// StepDSN — text-input for the raw connection string.
	// dsnText is preserved when the user Esc-backs from StepSaveLocation.
	// dsnError is the inline parse error; cleared on next typing.
	dsnText  string
	dsnError string

	// dsnParsedConn holds the config.Connection produced by ParseConnectionString
	// on a successful StepDSN confirmation.
	dsnParsedConn config.Connection

	// StepSaveLocation in DSN mode — editable name field.
	dsnName      string
	dsnNameError string

	// StepSaveLocation — fixed two-option list (no filter)
	selectedLoc int // 0 = Global, 1 = Project
	locPaths    config.Paths

	// StepSaveLocation — inline write error surfaced when the async save fails.
	// Cleared when the user navigates, edits the DSN-mode name, or re-submits.
	writeError string

	// Injected context
	connections config.Connections // snapshot for name-uniqueness validation
	cwd         string
}

// newConnectionWizardModalFor constructs a wizard seeded with the injected context.
func newConnectionWizardModalFor(connections config.Connections, cwd string, paths config.Paths) *newConnectionWizardModal {
	return &newConnectionWizardModal{
		step:        StepMode,
		connections: connections,
		cwd:         cwd,
		locPaths:    paths,
	}
}

func (w *newConnectionWizardModal) Name() AppModal { return ModalNewConnectionWizard }

// ---- Modal interface ----

func (w *newConnectionWizardModal) FilterText() string {
	switch w.step {
	case StepMode:
		return w.modeFilter + "█"
	case StepName:
		return w.name + "█"
	case StepType:
		return w.typeFilter + "█"
	case StepField:
		fields := w.fieldsForSelectedType()
		if w.fieldIndex < len(fields) && fields[w.fieldIndex].masked {
			return strings.Repeat("•", len(w.fieldValue)) + "█"
		}
		return w.fieldValue + "█"
	case StepDSN:
		return w.dsnText + "█"
	case StepSaveLocation:
		if w.wizardMode == "dsn" {
			return w.dsnName + "█"
		}
		return ""
	default:
		return ""
	}
}

func (w *newConnectionWizardModal) FilterLabel() string {
	switch w.step {
	case StepName:
		return "Name:"
	case StepField:
		fields := w.fieldsForSelectedType()
		if w.fieldIndex < len(fields) {
			return fields[w.fieldIndex].label + ":"
		}
		return "Value:"
	case StepDSN:
		return "DSN:"
	case StepSaveLocation:
		if w.wizardMode == "dsn" {
			return "Name:"
		}
		return "Filter:"
	default:
		return "Filter:"
	}
}

func (w *newConnectionWizardModal) Title() string {
	switch w.step {
	case StepMode:
		return "New Connection"
	case StepName:
		return "Connection Name"
	case StepType:
		return "Database Type"
	case StepField:
		fields := w.fieldsForSelectedType()
		if w.fieldIndex < len(fields) {
			return fields[w.fieldIndex].label
		}
		return "Field"
	case StepDSN:
		return "Connection String"
	case StepSaveLocation:
		return "Save Location"
	default:
		return "New Connection"
	}
}

func (w *newConnectionWizardModal) CounterText(_ InteractionState) string {
	switch w.step {
	case StepMode:
		filtered := w.filteredModes()
		if len(filtered) == 0 {
			return ""
		}
		sel := wrapSelection(w.selectedMode, len(filtered))
		return fmt.Sprintf("%d of %d", sel+1, len(filtered))
	case StepType:
		filtered := w.filteredTypes()
		if len(filtered) == 0 {
			return ""
		}
		sel := wrapSelection(w.selectedType, len(filtered))
		return fmt.Sprintf("%d of %d", sel+1, len(filtered))
	case StepSaveLocation:
		return fmt.Sprintf("%d of 2", w.selectedLoc+1)
	default:
		return ""
	}
}

func (w *newConnectionWizardModal) StatusBarHints(_ InteractionState) []string {
	switch w.step {
	case StepMode:
		escHint := "esc close"
		if strings.TrimSpace(w.modeFilter) != "" {
			escHint = "esc clear filter"
		}
		return []string{"enter select", escHint, "ctrl+p up", "ctrl+n down"}
	case StepName:
		return []string{"enter confirm", "esc back", "backspace delete"}
	case StepType:
		escHint := "esc back"
		if strings.TrimSpace(w.typeFilter) != "" {
			escHint = "esc clear filter"
		}
		return []string{"enter select", escHint, "ctrl+p up", "ctrl+n down"}
	case StepField:
		return []string{"enter confirm", "esc back", "backspace delete"}
	case StepDSN:
		return []string{"enter parse", "esc back", "backspace delete"}
	case StepSaveLocation:
		if w.wizardMode == "dsn" {
			return []string{"enter save", "esc back", "backspace delete", "ctrl+p up", "ctrl+n down"}
		}
		return []string{"enter save", "esc back", "ctrl+p up", "ctrl+n down"}
	default:
		return []string{"esc close"}
	}
}

func (w *newConnectionWizardModal) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	if msg.String() == "ctrl+c" {
		// ctrl+c anywhere in the wizard pushes the discard confirm rather than
		// immediately dismissing — data-loss safety for in-progress connections.
		return modalResultForward{cmd: func() tea.Msg { return confirmDiscardWizardMsg{} }}
	}
	if key.Matches(msg, keys.Help) {
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	}
	if key.Matches(msg, keys.Cancel) {
		return w.handleEsc()
	}

	switch w.step {
	case StepMode:
		return w.handleModeKey(msg, keys)
	case StepName:
		return w.handleNameKey(msg)
	case StepType:
		return w.handleTypeKey(msg, keys)
	case StepField:
		return w.handleFieldKey(msg)
	case StepDSN:
		return w.handleDSNKey(msg)
	case StepSaveLocation:
		return w.handleSaveLocationKey(msg, keys)
	default:
		return modalResultNone{}
	}
}

func (w *newConnectionWizardModal) handleModeKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	filtered := w.filteredModes()
	switch {
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		if len(filtered) > 0 {
			w.selectedMode = wrapSelection(w.selectedMode-1, len(filtered))
			w.vpMode = lazyScroll(w.selectedMode, w.vpMode, tui.ModalSplitListRows)
		}
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		if len(filtered) > 0 {
			w.selectedMode = wrapSelection(w.selectedMode+1, len(filtered))
			w.vpMode = lazyScroll(w.selectedMode, w.vpMode, tui.ModalSplitListRows)
		}
	case msg.String() == "enter":
		return w.confirmModeSelection(filtered)
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.modeFilter = trimLastRune(w.modeFilter)
		w.selectedMode = 0
		w.vpMode = 0
	case msg.String() == "space":
		w.modeFilter += " "
		w.selectedMode = 0
		w.vpMode = 0
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.modeFilter += msg.Text
		w.selectedMode = 0
		w.vpMode = 0
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) confirmModeSelection(filtered []newConnectionWizardMode) ModalResult {
	if len(filtered) == 0 {
		return modalResultNone{}
	}
	sel := wrapSelection(w.selectedMode, len(filtered))
	mode := filtered[sel]
	if mode.key == "dsn" {
		w.wizardMode = "dsn"
		w.step = StepDSN
		w.dsnError = ""
		return modalResultNone{}
	}
	// step-by-step: advance to name step
	w.wizardMode = "step-by-step"
	w.step = StepName
	w.name = ""
	w.nameError = ""
	return modalResultNone{}
}

func (w *newConnectionWizardModal) handleNameKey(msg tea.KeyPressMsg) ModalResult {
	switch {
	case msg.String() == "enter":
		return w.confirmName()
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.name = trimLastRune(w.name)
		w.nameError = ""
	case msg.String() == "space":
		w.name += " "
		w.nameError = ""
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.name += msg.Text
		w.nameError = ""
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) confirmName() ModalResult {
	if errMsg := w.validateName(); errMsg != "" {
		w.nameError = errMsg
		return modalResultNone{}
	}
	w.step = StepType
	w.typeFilter = ""
	w.selectedType = 0
	w.vpType = 0
	return modalResultNone{}
}

func (w *newConnectionWizardModal) handleTypeKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	filtered := w.filteredTypes()
	switch {
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		if len(filtered) > 0 {
			w.selectedType = wrapSelection(w.selectedType-1, len(filtered))
			w.vpType = lazyScroll(w.selectedType, w.vpType, tui.ModalSplitListRows)
		}
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		if len(filtered) > 0 {
			w.selectedType = wrapSelection(w.selectedType+1, len(filtered))
			w.vpType = lazyScroll(w.selectedType, w.vpType, tui.ModalSplitListRows)
		}
	case msg.String() == "enter":
		return w.confirmTypeSelection(filtered)
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.typeFilter = trimLastRune(w.typeFilter)
		w.selectedType = 0
		w.vpType = 0
	case msg.String() == "space":
		w.typeFilter += " "
		w.selectedType = 0
		w.vpType = 0
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.typeFilter += msg.Text
		w.selectedType = 0
		w.vpType = 0
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) confirmTypeSelection(filtered []newConnectionWizardType) ModalResult {
	if len(filtered) == 0 {
		return modalResultNone{}
	}
	// Advance step; if the type has no configured fields, skip StepField.
	fields := w.fieldsForSelectedType()
	if len(fields) == 0 {
		w.step = StepSaveLocation
	} else {
		w.step = StepField
		w.fieldIndex = 0
		w.fieldValues = make(map[string]string)
		w.fieldValue = fields[0].defaultValue
		w.fieldError = ""
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) handleFieldKey(msg tea.KeyPressMsg) ModalResult {
	switch {
	case msg.String() == "enter":
		return w.confirmField()
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.fieldValue = trimLastRune(w.fieldValue)
		w.fieldError = ""
	case msg.String() == "space":
		w.fieldValue += " "
		w.fieldError = ""
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.fieldValue += msg.Text
		w.fieldError = ""
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) confirmField() ModalResult {
	fields := w.fieldsForSelectedType()
	if w.fieldIndex >= len(fields) {
		w.step = StepSaveLocation
		return modalResultNone{}
	}
	field := fields[w.fieldIndex]

	// Validate the current field value.
	if field.isPort {
		portStr := strings.TrimSpace(w.fieldValue)
		port, err := strconv.Atoi(portStr)
		if err != nil {
			w.fieldError = field.label + " must be a valid number."
			return modalResultNone{}
		}
		if err := config.ValidatePort(port); err != nil {
			w.fieldError = field.label + ": " + err.Error() + "."
			return modalResultNone{}
		}
		w.fieldValue = portStr // store normalised
	} else if field.required && strings.TrimSpace(w.fieldValue) == "" {
		w.fieldError = field.label + " must not be empty."
		return modalResultNone{}
	}

	// Commit the value, then advance.
	if w.fieldValues == nil {
		w.fieldValues = make(map[string]string)
	}
	w.fieldValues[field.key] = w.fieldValue
	w.fieldIndex++
	if w.fieldIndex >= len(fields) {
		w.step = StepSaveLocation
	} else {
		w.fieldValue = fields[w.fieldIndex].defaultValue
		w.fieldError = ""
	}
	return modalResultNone{}
}

// handleDSNKey handles key events on the StepDSN screen.
// Typing appends to dsnText; Enter parses and validates the DSN on submit.
// No live validation — errors only appear after Enter.
func (w *newConnectionWizardModal) handleDSNKey(msg tea.KeyPressMsg) ModalResult {
	switch {
	case msg.String() == "enter":
		return w.confirmDSN()
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.dsnText = trimLastRune(w.dsnText)
		w.dsnError = ""
	case msg.String() == "space":
		w.dsnText += " "
		w.dsnError = ""
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.dsnText += msg.Text
		w.dsnError = ""
	}
	return modalResultNone{}
}

// confirmDSN parses the entered DSN with ParseConnectionString.
// On success it stores the parsed connection and advances to StepSaveLocation
// with a pre-filled name. On failure it shows an inline error and stays.
func (w *newConnectionWizardModal) confirmDSN() ModalResult {
	resolved, ok, err := config.ParseConnectionString(w.dsnText)
	if err != nil {
		w.dsnError = err.Error()
		return modalResultNone{}
	}
	if !ok {
		w.dsnError = "Not a valid connection string. Expected format: postgres://user:pass@host/db"
		return modalResultNone{}
	}
	w.dsnParsedConn = resolved.Connection
	w.dsnName = derivedNameFromConnection(resolved.Connection)
	w.dsnNameError = ""
	w.step = StepSaveLocation
	return modalResultNone{}
}

func (w *newConnectionWizardModal) handleSaveLocationKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	if w.wizardMode == "dsn" {
		return w.handleSaveLocationKeyDSN(msg, keys)
	}
	switch {
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		w.selectedLoc = wrapSelection(w.selectedLoc-1, 2)
		w.writeError = ""
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		w.selectedLoc = wrapSelection(w.selectedLoc+1, 2)
		w.writeError = ""
	case msg.String() == "enter":
		return w.submitSaveLocation()
	}
	return modalResultNone{}
}

// handleSaveLocationKeyDSN handles keys for StepSaveLocation in DSN mode.
// Typing edits the name; ctrl+p/ctrl+n navigate the save location; Enter submits.
func (w *newConnectionWizardModal) handleSaveLocationKeyDSN(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	switch {
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		w.selectedLoc = wrapSelection(w.selectedLoc-1, 2)
		w.writeError = ""
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		w.selectedLoc = wrapSelection(w.selectedLoc+1, 2)
		w.writeError = ""
	case msg.String() == "enter":
		return w.submitSaveLocation()
	case msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete":
		w.dsnName = trimLastRune(w.dsnName)
		w.dsnNameError = ""
		w.writeError = ""
	case msg.String() == "space":
		w.dsnName += " "
		w.dsnNameError = ""
		w.writeError = ""
	case len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl):
		w.dsnName += msg.Text
		w.dsnNameError = ""
		w.writeError = ""
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) submitSaveLocation() ModalResult {
	w.writeError = "" // clear any prior inline write error before retrying
	loc := "global"
	if w.selectedLoc == 1 {
		loc = "project"
	}
	paths := w.locPaths

	var name string
	var conn config.Connection
	if w.wizardMode == "dsn" {
		if errMsg := w.validateDSNName(); errMsg != "" {
			w.dsnNameError = errMsg
			return modalResultNone{}
		}
		name = strings.TrimSpace(w.dsnName)
		conn = w.dsnParsedConn
	} else {
		name = strings.TrimSpace(w.name)
		conn = w.buildConnection()
	}

	return modalResultForward{cmd: func() tea.Msg {
		return writeConnectionMsg{
			name:     name,
			conn:     conn,
			location: loc,
			paths:    paths,
		}
	}}
}

func (w *newConnectionWizardModal) handleEsc() ModalResult {
	switch w.step {
	case StepMode:
		if strings.TrimSpace(w.modeFilter) != "" {
			w.modeFilter = ""
			w.selectedMode = 0
			w.vpMode = 0
			return modalResultNone{}
		}
		// At the first step with an empty filter, push the discard confirm
		// rather than immediately closing — data-loss safety.
		return modalResultForward{cmd: func() tea.Msg { return confirmDiscardWizardMsg{} }}
	case StepName:
		w.step = StepMode
		return modalResultNone{}
	case StepType:
		if strings.TrimSpace(w.typeFilter) != "" {
			w.typeFilter = ""
			w.selectedType = 0
			w.vpType = 0
			return modalResultNone{}
		}
		w.step = StepName
		return modalResultNone{}
	case StepField:
		if w.fieldIndex > 0 {
			// Go back to the previous field, restoring its committed value.
			w.fieldIndex--
			fields := w.fieldsForSelectedType()
			if w.fieldIndex < len(fields) {
				w.fieldValue = w.fieldValues[fields[w.fieldIndex].key]
			}
			w.fieldError = ""
			return modalResultNone{}
		}
		// At the first field — go back to StepType.
		w.step = StepType
		w.fieldError = ""
		return modalResultNone{}
	case StepDSN:
		// Go back to StepMode; dsnText is preserved so the user can return and tweak.
		w.step = StepMode
		w.dsnError = ""
		return modalResultNone{}
	case StepSaveLocation:
		if w.wizardMode == "dsn" {
			// Go back to StepDSN; dsnText is still intact.
			w.step = StepDSN
			w.dsnNameError = ""
			return modalResultNone{}
		}
		// Go back to StepField if the selected type has fields, else to StepType.
		if len(w.fieldsForSelectedType()) > 0 {
			w.step = StepField
		} else {
			w.step = StepType
		}
		return modalResultNone{}
	default:
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}
}

// ---- Render ----

func (w *newConnectionWizardModal) Render(_ InteractionState, innerWidth int) string {
	switch w.step {
	case StepMode:
		return w.renderModeStep(innerWidth)
	case StepName:
		return w.renderNameStep()
	case StepType:
		return w.renderTypeStep(innerWidth)
	case StepField:
		return w.renderFieldStep()
	case StepDSN:
		return w.renderDSNStep()
	case StepSaveLocation:
		if w.wizardMode == "dsn" {
			return w.renderSaveLocationStepDSN(innerWidth)
		}
		return w.renderSaveLocationStep(innerWidth)
	default:
		return ""
	}
}

func (w *newConnectionWizardModal) renderModeStep(innerWidth int) string {
	filtered := w.filteredModes()
	if len(filtered) == 0 {
		return tui.AppTheme.PanelMuted.Render("No matches.")
	}
	sel := wrapSelection(w.selectedMode, len(filtered))
	vpStart := lazyScroll(sel, w.vpMode, tui.ModalSplitListRows)
	vpEnd := min(vpStart+tui.ModalSplitListRows, len(filtered))

	availWidth := innerWidth
	if availWidth < 20 {
		availWidth = 20
	}

	lines := make([]string, 0, vpEnd-vpStart)
	for i := vpStart; i < vpEnd; i++ {
		row := w.renderModeRow(filtered[i], availWidth)
		if i == sel {
			plain := ansi.Strip(row)
			if pad := availWidth - ansi.StringWidth(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(plain))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

func (w *newConnectionWizardModal) renderModeRow(m newConnectionWizardMode, width int) string {
	label := m.label
	desc := m.desc
	labelWidth := ansi.StringWidth(label)
	const minPad = 2
	descWidth := ansi.StringWidth(desc)
	available := width - labelWidth - minPad
	if available < descWidth {
		if available <= 0 {
			return label
		}
		desc = ansi.Truncate(desc, available, "…")
		descWidth = ansi.StringWidth(desc)
	}
	pad := width - labelWidth - descWidth
	if pad < minPad {
		pad = minPad
	}
	return label + strings.Repeat(" ", pad) + tui.AppTheme.PanelMuted.Render(desc)
}

func (w *newConnectionWizardModal) renderTypeStep(innerWidth int) string {
	filtered := w.filteredTypes()
	if len(filtered) == 0 {
		return tui.AppTheme.PanelMuted.Render("No matches.")
	}
	sel := wrapSelection(w.selectedType, len(filtered))
	vpStart := lazyScroll(sel, w.vpType, tui.ModalSplitListRows)
	vpEnd := min(vpStart+tui.ModalSplitListRows, len(filtered))

	_ = innerWidth // type labels are short; no truncation needed

	lines := make([]string, 0, vpEnd-vpStart)
	for i := vpStart; i < vpEnd; i++ {
		row := filtered[i].label
		if i == sel {
			plain := row
			if pad := innerWidth - ansi.StringWidth(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(plain))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

func (w *newConnectionWizardModal) renderNameStep() string {
	if w.nameError != "" {
		return tui.AppTheme.ErrorNotice.Render(w.nameError)
	}
	return tui.AppTheme.PanelMuted.Render("Enter a unique name for this connection.")
}

func (w *newConnectionWizardModal) renderFieldStep() string {
	if w.fieldError != "" {
		return tui.AppTheme.ErrorNotice.Render(w.fieldError)
	}
	fields := w.fieldsForSelectedType()
	if w.fieldIndex < len(fields) {
		return tui.AppTheme.PanelMuted.Render(fields[w.fieldIndex].hint)
	}
	return tui.AppTheme.PanelMuted.Render("Enter a value.")
}

// renderDSNStep renders the StepDSN body: hint text or inline parse error.
func (w *newConnectionWizardModal) renderDSNStep() string {
	if w.dsnError != "" {
		return tui.AppTheme.ErrorNotice.Render(w.dsnError)
	}
	return tui.AppTheme.PanelMuted.Render("Enter a connection string, e.g. postgres://user:pass@host/db")
}

func (w *newConnectionWizardModal) renderSaveLocationStep(innerWidth int) string {
	availWidth := innerWidth
	if availWidth < 20 {
		availWidth = 20
	}

	var lines []string

	// Summary of the assembled connection.
	typeName := w.selectedTypeName()
	lines = append(lines, tui.AppTheme.PanelText.Render("Connection:"))
	lines = append(lines, tui.AppTheme.PanelMuted.Render("  name:     "+strings.TrimSpace(w.name)))
	lines = append(lines, tui.AppTheme.PanelMuted.Render("  type:     "+typeName))
	switch typeName {
	case "sqlite":
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  database: "+w.fieldValues["database"]))
	default: // postgres, mysql
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  host:     "+w.fieldValues["host"]))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  port:     "+w.fieldValues["port"]))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  database: "+w.fieldValues["database"]))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  username: "+w.fieldValues["username"]))
		if w.fieldValues["password"] != "" {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("  password: ****"))
		}
		if w.fieldValues["ssh_host"] != "" {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("  ssh_host: "+w.fieldValues["ssh_host"]))
		}
	}
	lines = append(lines, "")
	lines = append(lines, tui.AppTheme.PanelText.Render("Save to:"))

	// Location options.
	locationLabels := []string{
		"Global   " + w.locPaths.Global,
		"Project  " + w.locPaths.Local,
	}
	for i, label := range locationLabels {
		if i == w.selectedLoc {
			plain := label
			if pad := availWidth - ansi.StringWidth(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(plain))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(label))
		}
	}

	// Inline write error (surfaced after a failed async save).
	if w.writeError != "" {
		lines = append(lines, "")
		lines = append(lines, tui.AppTheme.ErrorNotice.Render(w.writeError))
	}

	return strings.Join(lines, "\n")
}

// renderSaveLocationStepDSN renders the StepSaveLocation body in DSN mode.
// It shows the parsed connection summary (password masked) and location options.
// The editable name is shown in the filter input area above the body.
func (w *newConnectionWizardModal) renderSaveLocationStepDSN(innerWidth int) string {
	availWidth := innerWidth
	if availWidth < 20 {
		availWidth = 20
	}

	var lines []string

	// Inline name-collision error (shown before the summary).
	if w.dsnNameError != "" {
		lines = append(lines, tui.AppTheme.ErrorNotice.Render(w.dsnNameError))
	}

	// Parsed connection summary.
	conn := w.dsnParsedConn
	lines = append(lines, tui.AppTheme.PanelText.Render("Connection:"))
	lines = append(lines, tui.AppTheme.PanelMuted.Render("  type:     "+conn.Type))
	switch conn.Type {
	case "sqlite":
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  database: "+conn.Database))
	default: // postgres, mysql
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  host:     "+conn.Host))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  port:     "+strconv.Itoa(conn.Port)))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  database: "+conn.Database))
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  username: "+conn.Username))
		if conn.Password != "" {
			lines = append(lines, tui.AppTheme.PanelMuted.Render("  password: ****"))
		}
	}
	lines = append(lines, "")
	lines = append(lines, tui.AppTheme.PanelText.Render("Save to:"))

	// Location options.
	locationLabels := []string{
		"Global   " + w.locPaths.Global,
		"Project  " + w.locPaths.Local,
	}
	for i, label := range locationLabels {
		if i == w.selectedLoc {
			plain := label
			if pad := availWidth - ansi.StringWidth(plain); pad > 0 {
				plain += strings.Repeat(" ", pad)
			}
			lines = append(lines, tui.AppTheme.PanelSelected.Render(plain))
		} else {
			lines = append(lines, tui.AppTheme.PanelText.Render(label))
		}
	}

	// Inline write error (surfaced after a failed async save).
	if w.writeError != "" {
		lines = append(lines, "")
		lines = append(lines, tui.AppTheme.ErrorNotice.Render(w.writeError))
	}

	return strings.Join(lines, "\n")
}

// ---- HandleMouse ----

func (w *newConnectionWizardModal) HandleMouse(_ tea.MouseClickMsg, ctx ModalContext) ModalResult {
	if ctx.MouseListOffset < 0 {
		return modalResultNone{}
	}
	switch w.step {
	case StepMode:
		filtered := w.filteredModes()
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		vpStart := w.vpMode
		idx := vpStart + ctx.MouseListOffset
		if idx < 0 || idx >= len(filtered) {
			return modalResultNone{}
		}
		w.selectedMode = idx
		if ctx.MouseDoubleClick {
			return w.confirmModeSelection(filtered)
		}
	case StepType:
		filtered := w.filteredTypes()
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		vpStart := w.vpType
		idx := vpStart + ctx.MouseListOffset
		if idx < 0 || idx >= len(filtered) {
			return modalResultNone{}
		}
		w.selectedType = idx
		if ctx.MouseDoubleClick {
			return w.confirmTypeSelection(filtered)
		}
	case StepSaveLocation:
		// The summary takes lines 0..N-1 before the location options.
		// Location options are always the last 2 rendered lines; we map offset
		// by the count of non-option lines rendered in renderSaveLocationStep.
		summaryLines := w.saveLocationSummaryLineCount()
		locIdx := ctx.MouseListOffset - summaryLines
		if locIdx < 0 || locIdx > 1 {
			return modalResultNone{}
		}
		w.selectedLoc = locIdx
		if ctx.MouseDoubleClick {
			return w.submitSaveLocation()
		}
	}
	return modalResultNone{}
}

// ---- HandleMouseWheel ----

func (w *newConnectionWizardModal) HandleMouseWheel(_ ModalContext, msg tea.MouseWheelMsg) ModalResult {
	var delta int
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return modalResultNone{}
	}

	switch w.step {
	case StepMode:
		filtered := w.filteredModes()
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		w.selectedMode = min(max(w.selectedMode+delta, 0), len(filtered)-1)
		w.vpMode = lazyScroll(w.selectedMode, w.vpMode, tui.ModalSplitListRows)
	case StepType:
		filtered := w.filteredTypes()
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		w.selectedType = min(max(w.selectedType+delta, 0), len(filtered)-1)
		w.vpType = lazyScroll(w.selectedType, w.vpType, tui.ModalSplitListRows)
	case StepSaveLocation:
		w.selectedLoc = min(max(w.selectedLoc+delta, 0), 1)
	}
	return modalResultNone{}
}

// ---- Helpers ----

// saveLocationSummaryLineCount returns the number of summary lines rendered
// in renderSaveLocationStep before the "Save to:" section and location options.
// Used by HandleMouse to map click offsets to the correct location row.
func (w *newConnectionWizardModal) saveLocationSummaryLineCount() int {
	if w.wizardMode == "dsn" {
		return w.saveLocationSummaryLineCountDSN()
	}
	typeName := w.selectedTypeName()
	switch typeName {
	case "sqlite":
		// "Connection:", name, type, database, "", "Save to:" = 6 lines
		return 6
	default: // postgres, mysql
		// "Connection:", name, type, host, port, database, username, "", "Save to:" = 9 lines
		// plus one line each for optional password and ssh_host if non-empty.
		count := 9
		if w.fieldValues["password"] != "" {
			count++
		}
		if w.fieldValues["ssh_host"] != "" {
			count++
		}
		return count
	}
}

// saveLocationSummaryLineCountDSN returns the summary line count for DSN mode.
func (w *newConnectionWizardModal) saveLocationSummaryLineCountDSN() int {
	count := 0
	if w.dsnNameError != "" {
		count++ // inline error line
	}
	count++ // "Connection:"
	count++ // "  type: ..."
	switch w.dsnParsedConn.Type {
	case "sqlite":
		count++ // "  database: ..."
	default: // postgres, mysql
		count += 4 // host, port, database, username
		if w.dsnParsedConn.Password != "" {
			count++ // "  password: ****"
		}
	}
	count++ // blank line
	count++ // "Save to:"
	return count
}

func (w *newConnectionWizardModal) filteredModes() []newConnectionWizardMode {
	trimmed := strings.TrimSpace(w.modeFilter)
	if trimmed == "" {
		return wizardModes
	}
	var result []newConnectionWizardMode
	for _, m := range wizardModes {
		combined := strings.ToLower(m.label + " " + m.desc)
		if _, ok := fuzzyMatch(trimmed, combined); ok {
			result = append(result, m)
		}
	}
	return result
}

func (w *newConnectionWizardModal) filteredTypes() []newConnectionWizardType {
	trimmed := strings.TrimSpace(w.typeFilter)
	if trimmed == "" {
		return wizardTypes
	}
	var result []newConnectionWizardType
	for _, t := range wizardTypes {
		if _, ok := fuzzyMatch(trimmed, strings.ToLower(t.label)); ok {
			result = append(result, t)
		}
	}
	return result
}

// fieldsForSelectedType returns the field list for the currently selected type.
func (w *newConnectionWizardModal) fieldsForSelectedType() []newConnectionWizardField {
	typeName := w.selectedTypeName()
	return wizardFieldsByType[typeName]
}

// selectedTypeName returns the key of the currently selected database type.
func (w *newConnectionWizardModal) selectedTypeName() string {
	filtered := w.filteredTypes()
	if len(filtered) == 0 {
		return ""
	}
	sel := wrapSelection(w.selectedType, len(filtered))
	return filtered[sel].key
}

// validateName returns an error message if the name is invalid, or "" if valid.
func (w *newConnectionWizardModal) validateName() string {
	name := strings.TrimSpace(w.name)
	if name == "" {
		return "Name must not be empty."
	}
	if w.connections.Connection != nil {
		if _, exists := w.connections.Connection[name]; exists {
			return fmt.Sprintf("A connection named %q already exists.", name)
		}
	}
	return ""
}

// validateDSNName returns an error message if the DSN-mode name is invalid, or "" if valid.
func (w *newConnectionWizardModal) validateDSNName() string {
	name := strings.TrimSpace(w.dsnName)
	if name == "" {
		return "Name must not be empty."
	}
	if w.connections.Connection != nil {
		if _, exists := w.connections.Connection[name]; exists {
			return fmt.Sprintf("A connection named %q already exists.", name)
		}
	}
	return ""
}

// buildConnection assembles the config.Connection from the wizard state.
func (w *newConnectionWizardModal) buildConnection() config.Connection {
	typeName := w.selectedTypeName()
	fv := w.fieldValues
	if fv == nil {
		fv = map[string]string{}
	}
	switch typeName {
	case "sqlite":
		return config.Connection{
			Type:     "sqlite",
			Database: fv["database"],
		}
	case "postgres", "mysql":
		port, _ := strconv.Atoi(fv["port"])
		conn := config.Connection{
			Type:     typeName,
			Host:     fv["host"],
			Port:     port,
			Database: fv["database"],
			Username: fv["username"],
		}
		if pw := fv["password"]; pw != "" {
			conn.Password = pw
		}
		if ssh := fv["ssh_host"]; ssh != "" {
			conn.SSHHost = ssh
		}
		return conn
	default:
		return config.Connection{Type: typeName}
	}
}

// derivedNameFromConnection returns a type-aware default connection name
// derived from the parsed connection:
//   - postgres/mysql: "<database>@<host>"
//   - sqlite: basename of the path with extension stripped; ":memory:" → "memory"
func derivedNameFromConnection(conn config.Connection) string {
	switch conn.Type {
	case "postgres", "mysql":
		return conn.Database + "@" + conn.Host
	case "sqlite":
		db := conn.Database
		if db == ":memory:" {
			return "memory"
		}
		base := filepath.Base(db)
		if ext := filepath.Ext(base); ext != "" {
			base = base[:len(base)-len(ext)]
		}
		return base
	default:
		if conn.Database != "" {
			return conn.Database
		}
		return conn.Type
	}
}

// ---- Model-level constructor ----

// handleOpenNewConnectionWizard pushes a new wizard onto the modal stack.
// Called from Model.Update when openNewConnectionWizardMsg arrives.
func (m *Model) handleOpenNewConnectionWizard() (Model, tea.Cmd) {
	var connections config.Connections
	if m.connectionsLoader != nil {
		if c, err := m.connectionsLoader(); err == nil {
			connections = c
		}
	}
	paths, _ := config.DiscoverConnectionPaths(m.session.WorkingDir)
	wizard := newConnectionWizardModalFor(connections, m.session.WorkingDir, paths)
	m.pushModal(wizard)
	return *m, nil
}
