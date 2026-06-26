package app

import (
	"fmt"
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
	key      string
	label    string
	hint     string
	required bool
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
// the wizard collects for that type.  PostgreSQL and MySQL entries are added
// by issue #18; only sqlite is implemented in this slice.
var wizardFieldsByType = map[string][]newConnectionWizardField{
	"sqlite": {
		{
			key:      "database",
			label:    "Database",
			hint:     "File path; ~ is expanded to home directory",
			required: true,
		},
	},
	// "postgres" and "mysql" field lists added by #18
}

// newConnectionWizardModal implements Modal for the New Connection Wizard.
// It owns all step state; it never mutates *Model directly and returns
// ModalResult for every action.  Mirrors slashWizardModal's conventions.
//
// Step flow (this slice — sqlite + step-by-step only):
//
//	StepMode → StepName → StepType → StepField → StepSaveLocation
//
// Esc navigates backward; in filter steps a non-empty filter is cleared first.
// On StepSaveLocation submit the wizard returns modalResultForward{writeConnectionMsg}.
type newConnectionWizardModal struct {
	step NewConnectionWizardStep

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

	// StepField — text-input (filter box repurposed)
	// For #18: extend to fieldIndex int + fieldValues map[string]string.
	fieldValue string
	fieldError string

	// StepSaveLocation — fixed two-option list (no filter)
	selectedLoc int // 0 = Global, 1 = Project
	locPaths    config.Paths

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
		return w.fieldValue + "█"
	default: // StepSaveLocation has no filter
		return ""
	}
}

func (w *newConnectionWizardModal) FilterLabel() string {
	switch w.step {
	case StepName:
		return "Name:"
	case StepField:
		fields := w.fieldsForSelectedType()
		if len(fields) > 0 {
			return fields[0].label + ":"
		}
		return "Value:"
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
		if len(fields) > 0 {
			return fields[0].label + " Path"
		}
		return "Field"
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
	case StepSaveLocation:
		return []string{"enter save", "esc back", "ctrl+p up", "ctrl+n down"}
	default:
		return []string{"esc close"}
	}
}

func (w *newConnectionWizardModal) HandleKey(msg tea.KeyPressMsg, _ ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	if msg.String() == "ctrl+c" {
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
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
		// TODO(#19): implement DSN mode — stubs the option as a visible-but-inert choice
		return modalResultReady{
			status: "DSN mode is not yet implemented. See issue #19.",
			level:  NotificationInfo,
		}
	}
	// step-by-step: advance to name step
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
		w.fieldValue = ""
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
	if len(fields) > 0 && fields[0].required && strings.TrimSpace(w.fieldValue) == "" {
		w.fieldError = fields[0].label + " must not be empty."
		return modalResultNone{}
	}
	w.step = StepSaveLocation
	return modalResultNone{}
}

func (w *newConnectionWizardModal) handleSaveLocationKey(msg tea.KeyPressMsg, keys commandModeKeyMap) ModalResult {
	switch {
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "up":
		w.selectedLoc = wrapSelection(w.selectedLoc-1, 2)
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "down":
		w.selectedLoc = wrapSelection(w.selectedLoc+1, 2)
	case msg.String() == "enter":
		return w.submitSaveLocation()
	}
	return modalResultNone{}
}

func (w *newConnectionWizardModal) submitSaveLocation() ModalResult {
	loc := "global"
	if w.selectedLoc == 1 {
		loc = "project"
	}
	conn := w.buildConnection()
	paths := w.locPaths
	name := strings.TrimSpace(w.name)
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
		// At the first step, Esc closes the wizard and returns to the picker.
		// TODO(#21): add discard-confirm before closing.
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
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
		w.step = StepType
		w.fieldError = ""
		return modalResultNone{}
	case StepSaveLocation:
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
	case StepSaveLocation:
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
	if len(fields) > 0 {
		return tui.AppTheme.PanelMuted.Render(fields[0].hint)
	}
	return tui.AppTheme.PanelMuted.Render("Enter a value.")
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
	fields := w.fieldsForSelectedType()
	if len(fields) > 0 {
		lines = append(lines, tui.AppTheme.PanelMuted.Render("  database: "+w.fieldValue))
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
		// Summary: "Connection:", name, type, (optionally database), "", "Save to:" = 5 or 6 lines
		summaryLines := 6 // "Connection:" + name + type + database + "" + "Save to:"
		if len(w.fieldsForSelectedType()) == 0 {
			summaryLines = 5 // no database line
		}
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

// buildConnection assembles the config.Connection from the wizard state.
func (w *newConnectionWizardModal) buildConnection() config.Connection {
	typeName := w.selectedTypeName()
	switch typeName {
	case "sqlite":
		return config.Connection{
			Type:     "sqlite",
			Database: w.fieldValue,
		}
	default:
		// postgres and mysql fields are added by #18; for now just store type.
		return config.Connection{Type: typeName}
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
