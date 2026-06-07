package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

type Model struct {
	session         ConnectionInfo
	adapter         *db.SQLAdapter
	history         *apphistory.History
	executionCancel context.CancelFunc
	command         commandModeModel
	resultsPane     resultsPaneModeModel
	state           SharedAppState
	cache           *autocompleteSchemaCache
	loader          autocompleteSchemaLoader
	width           int
	height          int
	splitRatio      float64
	pendingQuit     bool
}

type autocompleteSchemaLoader func(context.Context, *db.SQLAdapter) (*AutocompleteSchemaContext, error)

type modelDependencies struct {
	cache   *autocompleteSchemaCache
	loader  autocompleteSchemaLoader
	history *apphistory.History
}

// nopCmd is a non-nil tea.Cmd that produces no message. Use it when a key
// event has been fully handled but no further action is needed — returning a
// non-nil cmd prevents the event from falling through to the command pane.
var nopCmd tea.Cmd = func() tea.Msg { return nil }

type submitIntentMsg struct{}

type cancelRunningIntentMsg struct{}

type slashWizardMoveIntentMsg struct {
	Delta int
}

type slashWizardBackIntentMsg struct{}

type slashWizardCloseIntentMsg struct{}

type historyIntentMsg struct{}

type toggleHelpIntentMsg struct{}

type toggleZoomIntentMsg struct{}

type switchModeIntentMsg struct{}

type switchLayoutIntentMsg struct {
	Layout AppLayout
}

type PaneTarget string

const (
	PaneResults PaneTarget = "results"
	PaneCommand PaneTarget = "command"
)

type focusPaneIntentMsg struct {
	Pane PaneTarget
}

type clearInputIntentMsg struct{}

type statementExecutedMsg struct {
	Statement     string
	Result        *db.StatementResult
	ResultSummary string
	Err           error
}

type slashCommandExecutedMsg struct {
	Command       slashCommand
	Result        slashCommandResult
	ResultSummary string
	Err           error
}

type startupCompleteMsg struct{}

type autocompleteSchemaLoadedMsg struct {
	Schema *AutocompleteSchemaContext
	Err    error
}

type reconnectStateMsg struct {
	Context ReconnectContext
	Status  string
}

type appErrorMsg struct {
	Err    error
	Status string
}

type runningTickMsg struct {
	StartedAt time.Time
	Now       time.Time
}

const defaultInteractiveExecutionTimeout = 30 * time.Second

func NewModel(session ConnectionInfo, adapter *db.SQLAdapter) Model {
	return newModelWithDependencies(session, adapter, modelDependencies{})
}

func newModelWithDependencies(session ConnectionInfo, adapter *db.SQLAdapter, deps modelDependencies) Model {
	cache := deps.cache
	if cache == nil {
		cache = newAutocompleteSchemaCache()
	}

	loader := deps.loader
	if loader == nil {
		loader = loadAutocompleteSchema
	}

	sessionHistory := deps.history
	if sessionHistory == nil {
		sessionHistory = apphistory.NewHistory()
	}

	model := Model{
		session:    session,
		adapter:    adapter,
		history:    sessionHistory,
		command:    newCommandModeModel(),
		resultsPane: newResultsPaneModeModel(),
		state:      NewSharedAppState(),
		cache:      cache,
		loader:     loader,
		splitRatio: 0.65,
	}
	model.syncAutocompleteSchemaSnapshot()
	model.syncSessionHistorySnapshot()

	return model
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return startupCompleteMsg{} }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.state.App.Current != StateReady {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}

		if m.state.Interaction.ActiveMode == ModeHistorySearch {
			if msg.String() != "ctrl+c" {
				m.pendingQuit = false
			}
			if cmd := m.handleLayoutKey(msg); cmd != nil {
				return m, cmd
			}
			return m, m.handleHistorySearchKey(msg)
		}

		if msg.String() != "ctrl+c" {
			m.pendingQuit = false
		}

		if m.handleResultsPaneNavigationKey(msg) {
			return m, nil
		}

		if m.handleResultsPaneWriteKey(msg) {
			return m, nil
		}

		if m.handleResultsPaneSelectionKey(msg) {
			return m, nil
		}

		if m.handleResultsPaneComposeKey(msg) {
			return m, nil
		}

		if m.handleResultsPanePagingKey(msg) {
			return m, nil
		}

		if cmd := m.handleKey(msg); cmd != nil {
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c":
			// If a popup/overlay is open, close it instead of quitting
			if m.state.Interaction.SlashWizard != nil {
				m.pendingQuit = false
				return m, func() tea.Msg { return slashWizardCloseIntentMsg{} }
			}
			// If in command mode with text, clear the input
			if m.state.App.Current == StateReady && m.command.Focused() && strings.TrimSpace(m.command.Value()) != "" {
				m.pendingQuit = false
				return m, func() tea.Msg { return clearInputIntentMsg{} }
			}
			// Double ctrl-c to quit: first press shows hint, second press exits
			if m.pendingQuit {
				return m, tea.Quit
			}
			m.pendingQuit = true
			m.state.SetPendingIntent(IntentNone, "quit", "Press ctrl+c again to exit.")
			return m, nil
		case "q":
			if !m.command.Focused() {
				return m, tea.Quit
			}
		}
	case submitIntentMsg:
		if running := m.state.Interaction.Running; running != nil {
			m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("%s is still running. Press esc to cancel; timeout after %s.", runningLabel(running), formatExecutionTimeout(defaultInteractiveExecutionTimeout)))
			return m, nil
		}

		if wizard := m.state.Interaction.SlashWizard; wizard != nil {
			return m.submitSlashWizard(wizard)
		}

		m.syncCurrentSQL()
		submittedSQL := m.state.Interaction.CurrentSQL
		if strings.TrimSpace(submittedSQL) == "" {
			m.state.SetRunningStatementContext(nil)
			m.state.SetReady("")
			m.state.SetPendingIntent(IntentSubmit, "submit", "Submit requested with empty input.")
			return m, nil
		}

		parsedSlash, err := parseSlashCommand(submittedSQL)
		if err != nil {
			m.state.SetRunningStatementContext(nil)
			m.state.SetReady("")
			m.state.SetPendingIntent(IntentNone, "submit", fmt.Sprintf("Slash command parse failed: %v", err))
			m.state.SetLatestResultContext(nil)
			m.state.SetPendingModeSwitch(nil)
			return m, nil
		}
		if parsedSlash != nil {
			if spec, ok := defaultSlashCommandRegistry.byName[parsedSlash.Name]; ok && spec.NeedsTarget && len(parsedSlash.Args) == 0 {
				return m.openTableSelectionForCommand(parsedSlash)
			}
			if parsedSlash.Name == "commands" {
				return m.openCommandWizard()
			}
			return m, m.startExecution(parsedSlash.DisplayName, fmt.Sprintf("Dispatching %s.", parsedSlash.DisplayName), executeSlashCommandCmd(slashCommandContext{
				Session: m.session,
				Adapter: m.adapter,
				Dialect: m.adapterDialect(),
				Query:   m.state.Interaction.snapshot(),
			}, *parsedSlash))
		}

		if !isCompleteSQLStatement(submittedSQL) {
			m.state.SetRunningStatementContext(nil)
			m.state.SetPendingIntent(IntentNone, "submit", "SQL is incomplete. End the statement with ';' to run it.")
			return m, nil
		}

		m.state.SetLastSubmittedSQL(submittedSQL)
		return m, m.startExecution("SQL", fmt.Sprintf("Executing %d characters of SQL.", len(submittedSQL)), executeStatementCmd(m.adapter, submittedSQL))
	case cancelRunningIntentMsg:
		if running := m.state.Interaction.Running; running != nil {
			if m.executionCancel != nil {
				m.executionCancel()
			}
			m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Cancelling %s...", runningLabel(running)))
		}
		return m, nil
	case statementExecutedMsg:
		running := m.state.Interaction.Running
		m.clearExecution()
		historyErr := m.appendSessionHistory(msg.Statement, msg.ResultSummary)
		m.state.SetRunningStatementContext(nil)
		m.state.Interaction.PendingIntent = IntentNone
		m.state.Interaction.LastAction = "submit"
		m.state.SetPendingModeSwitch(nil)
		if msg.Err != nil {
			if status, ok := executionInterruptedStatus(running, msg.Err); ok {
			m.command.AppendReplEntry("> ", msg.Statement, "ERROR: "+strings.TrimSpace(msg.Err.Error()))
			m.command.Clear()
			m.state.SetReady(withHistoryWarning(status, historyErr))
			m.state.SetLatestResultContext(nil)
			return m, nil
		}
		m.command.AppendReplEntry("> ", msg.Statement, "ERROR: "+strings.TrimSpace(msg.Err.Error()))
		m.command.Clear()
		m.state.SetReady(withHistoryWarning(formatOperationFailure("Execution failed.", msg.Err), historyErr))
		m.state.SetLatestResultContext(nil)
		return m, nil
	}

	m.command.AppendReplEntry("> ", msg.Statement, "OK: "+formatReplStatementOutput(msg.Result, nil))
	m.command.Clear()
	m.state.SetReady(withHistoryWarning(describeStatementStatus(msg.Result), historyErr))
	m.state.SetLatestResultContext(buildLatestResultContext(msg.Statement, m.resultOriginMode(), msg.Result))
		return m, nil
	case slashCommandExecutedMsg:
		running := m.state.Interaction.Running
		m.clearExecution()
		shouldRecordSlashCommand := msg.Err != nil || !msg.Result.ShouldReplace
		var historyErr error
		if shouldRecordSlashCommand {
			historyErr = m.appendSessionHistory(msg.Command.RawInput, msg.ResultSummary)
			m.state.SetLastSubmittedSQL(msg.Command.RawInput)
		}
		m.state.SetRunningStatementContext(nil)
		m.state.Interaction.PendingIntent = IntentNone
		m.state.Interaction.LastAction = "slash:" + msg.Command.DisplayName
		m.state.SetPendingModeSwitch(nil)
		if msg.Err != nil {
			m.state.SetSlashWizardContext(nil)
			m.command.AppendReplEntry("> ", msg.Command.RawInput, "ERROR: "+strings.TrimSpace(msg.Err.Error()))
			m.command.Clear()
			if status, ok := executionInterruptedStatus(running, msg.Err); ok {
				m.state.SetReady(withHistoryWarning(status, historyErr))
				m.state.SetLatestResultContext(nil)
				return m, nil
			}
			m.state.SetReady(withHistoryWarning(formatOperationFailure(msg.Command.DisplayName+" failed", msg.Err), historyErr))
			m.state.SetLatestResultContext(nil)
			return m, nil
		}

		m.state.SetSlashWizardContext(msg.Result.Wizard)

		if msg.Result.ShouldReplace {
			m.command.SetEditorValue(msg.Result.ReplaceEditor)
			m.syncCurrentSQL()
			m.state.SetLatestResultContext(nil)
		} else {
			// Not a replace — add to transcript and clear
			m.command.AppendReplEntry("> ", msg.Command.RawInput, "OK: "+formatReplSlashOutput(msg))
			m.command.Clear()
		}

		if msg.Result.Statement != nil {
			m.state.SetLatestResultContext(buildLatestResultContext(msg.Command.RawInput, m.resultOriginMode(), msg.Result.Statement))
		} else if !msg.Result.PreserveResult && !msg.Result.ShouldReplace {
			m.state.SetLatestResultContext(nil)
		}

		m.state.SetReady(withHistoryWarning(defaultStatus(msg.Result.Status, fmt.Sprintf("%s completed.", msg.Command.DisplayName)), historyErr))
		return m, nil
	case slashWizardMoveIntentMsg:
		m.moveSlashWizardSelection(msg.Delta)
		return m, nil
	case slashWizardBackIntentMsg:
		m.stepBackSlashWizard()
		return m, nil
	case slashWizardCloseIntentMsg:
		m.state.SetSlashWizardContext(nil)
		m.state.SetReady("Closed the slash command wizard.")
		return m, nil
	case toggleHelpIntentMsg:
		visible := !m.state.Interaction.HelpVisible
		m.state.SetHelpVisible(visible)
		if visible {
			m.state.SetPendingIntent(IntentNone, "help", "Opened help for keybindings and slash commands.")
		} else {
			m.state.SetPendingIntent(IntentNone, "help", "Closed help.")
		}
		return m, nil
	case clearInputIntentMsg:
		if m.state.Interaction.SlashWizard != nil {
			m.state.SetSlashWizardContext(nil)
		}
		m.command.Clear()
		m.syncCurrentSQL()
		m.state.SetReady("")
		m.state.SetPendingIntent(IntentClearCommandPane, "clear", "Cleared current input.")
		return m, nil
	case historyIntentMsg:
		m.syncCurrentSQL()
		m.state.SetReady("")
		m.openHistorySearch()
		return m, nil
	case switchModeIntentMsg:
		m.syncCurrentSQL()
		switchContext := buildModeSwitchContext(m.state.Interaction.Layout, nextLayoutForModeIntent(m.state.Interaction.Layout, m.state.Interaction.ActiveMode), m.state.Interaction.ActiveMode, nextModeForIntent(m.state.Interaction.ActiveMode), m.state.Interaction.LatestResult)
		m.applyModeSwitch(switchContext)
		m.syncPaneSizes()
		return m, nil
	case switchLayoutIntentMsg:
		m.syncCurrentSQL()
		m.applyLayoutSwitch(msg.Layout)
		m.syncPaneSizes()
		return m, nil
	case focusPaneIntentMsg:
		m.syncCurrentSQL()
		m.handleFocusPane(msg.Pane)
		m.syncPaneSizes()
		return m, nil
	case toggleZoomIntentMsg:
		m.syncCurrentSQL()
		m.handleToggleZoom()
		m.syncPaneSizes()
		return m, nil
	case startupCompleteMsg:
		m.state.SetReady("")
		return m, tea.Batch(m.command.Init(), m.refreshAutocompleteSchemaCmd())
	case autocompleteSchemaLoadedMsg:
		if msg.Err == nil {
			m.cache.Replace(msg.Schema)
			m.syncAutocompleteSchemaSnapshot()
		}
		return m, nil
	case reconnectStateMsg:
		m.state.SetReconnect(msg.Status, &msg.Context)
		return m, nil
	case appErrorMsg:
		m.clearExecution()
		message := ""
		if msg.Err != nil {
			message = FormatTerminalError(msg.Err)
		}
		m.state.SetRunningStatementContext(nil)
		m.state.SetError(message, msg.Status)
		return m, nil
	case runningTickMsg:
		running := m.state.Interaction.Running
		if running == nil || !running.StartedAt.Equal(msg.StartedAt) {
			return m, nil
		}

		updated := *running
		if msg.Now.After(updated.StartedAt) {
			updated.Elapsed = msg.Now.Sub(updated.StartedAt)
		}
		updated.SpinnerFrame = (updated.SpinnerFrame + 1) % len(runningSpinnerFrames)
		m.state.SetRunningStatementContext(&updated)
		return m, runningTickCmd(updated.StartedAt)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncPaneSizes()
	}

	if m.state.Interaction.ActiveMode == ModeResultsPane && !layoutShowsCommand(m.state.Interaction.Layout) {
		return m, nil
	}

	var cmd tea.Cmd
	m.command, cmd = m.command.Update(msg, m.state.Interaction)
	m.syncCurrentSQL()
	return m, cmd
}

func (m Model) View() tea.View {
	// Status bar always occupies the last two lines
	statusBar := m.statusBarView()
	statusDesc := m.statusDescriptionView()

	// Content area above the status bar
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.state.App.Current {
	case StateStartup:
		content = strings.Join([]string{
			appTheme.panelTitle.Render("[ startup ]"),
			appTheme.panelText.Render("Preparing command mode..."),
			appTheme.panelMuted.Render(m.state.Status),
		}, "\n")
	case StateReconnect:
		lines := []string{
			appTheme.panelTitle.Render("[ reconnect ]"),
			appTheme.panelText.Render("Connection recovery in progress."),
			appTheme.panelMuted.Render(m.state.Status),
		}
		if reconnect := m.state.App.Reconnect; reconnect != nil {
			if reconnect.Attempt > 0 {
				lines = append(lines, appTheme.panelText.Render(fmt.Sprintf("Attempt %d", reconnect.Attempt)))
			}
			if reason := strings.TrimSpace(reconnect.Reason); reason != "" {
				lines = append(lines, appTheme.panelText.Render(fmt.Sprintf("Reason: %s", reason)))
			}
			if lastError := strings.TrimSpace(reconnect.LastError); lastError != "" {
				lines = append(lines, appTheme.panelText.Render(fmt.Sprintf("Last error: %s", lastError)))
			}
		}
		content = strings.Join(lines, "\n")
	case StateError:
		lines := []string{
			appTheme.errorNotice.Render("[ error ]"),
			appTheme.panelText.Render(m.state.Status),
		}
		if appError := strings.TrimSpace(m.state.App.Error); appError != "" {
			lines = append(lines, appTheme.errorNotice.Render(appError))
		}
		content = strings.Join(lines, "\n")
	case StateReady:
		content = m.readyStateView(contentHeight)
	default:
		content = m.readyStateView(contentHeight)
	}

	v := tea.NewView(content + "\n" + statusBar + "\n" + statusDesc)
	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	return v
}

// syncPaneSizes computes inner pane dimensions and propagates them to sub-models.
func (m *Model) syncPaneSizes() {
	w := m.width
	h := m.height
	statusBarHeight := 2
	contentHeight := h - statusBarHeight
	if contentHeight < 2 {
		contentHeight = 2
	}

	// Account for border characters (│ on each side)
	innerWidth := w - 2
	if innerWidth < minimumEditorWidth {
		innerWidth = minimumEditorWidth
	}

	switch m.state.Interaction.Layout {
	case LayoutSplit:
		resultsPaneOuterH := int(float64(contentHeight) * m.splitRatio)
		if resultsPaneOuterH < 3 {
			resultsPaneOuterH = 3
		}
		commandOuterH := contentHeight - resultsPaneOuterH
		if commandOuterH < 3 {
			commandOuterH = 3
		}
		m.resultsPane.SetSize(innerWidth, resultsPaneOuterH-2)
		m.command.SetSize(innerWidth, commandOuterH-2)
	case LayoutResultsPaneOnly:
		m.resultsPane.SetSize(innerWidth, contentHeight-2)
		m.command.SetSize(innerWidth, contentHeight-2)
	default: // LayoutCommandOnly
		m.command.SetSize(innerWidth, contentHeight-2)
		m.resultsPane.SetSize(innerWidth, contentHeight-2)
	}
}

// renderBorderedPane wraps content in a rounded border with an optional title; active pane gets accent colour.
func (m Model) renderBorderedPane(content string, title string, active bool, outerWidth, innerHeight int) string {
	borderColor := appTheme.paneBorderInactive.GetForeground()
	if active {
		borderColor = appTheme.paneBorderActive.GetForeground()
	}
	innerWidth := outerWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 0 {
		innerHeight = 0
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Top line with optional title
	var topLine string
	if title != "" {
		titleRendered := appTheme.panelTitle.Render(title)
		titleVisualWidth := ansi.StringWidth(title)
		dashesAfter := innerWidth - 1 - titleVisualWidth - 1 // "─" + title + " " + dashes
		if dashesAfter < 0 {
			dashesAfter = 0
		}
		topLine = borderStyle.Render("╭─") + titleRendered + borderStyle.Render(" "+strings.Repeat("─", dashesAfter)+"╮")
	} else {
		topLine = borderStyle.Render("╭" + strings.Repeat("─", innerWidth) + "╮")
	}

	// Bottom line
	bottomLine := borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")

	// Split content into lines
	contentLines := strings.Split(content, "\n")

	// Pad or truncate to innerHeight lines
	for len(contentLines) < innerHeight {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}

	// Build the pane
	lines := make([]string, 0, innerHeight+2)
	lines = append(lines, topLine)
	for _, cl := range contentLines {
		visibleWidth := ansi.StringWidth(cl)
		padding := ""
		if visibleWidth < innerWidth {
			padding = strings.Repeat(" ", innerWidth-visibleWidth)
		}
		if visibleWidth > innerWidth {
			cl = ansi.Truncate(cl, innerWidth, "")
			padding = ""
		}
		lines = append(lines, borderStyle.Render("│")+cl+padding+borderStyle.Render("│"))
	}
	lines = append(lines, bottomLine)

	return strings.Join(lines, "\n")
}

func (m Model) readyStateView(totalHeight int) string {
	interaction := m.state.Interaction
	w := m.width
	if w < 4 {
		w = 4
	}

	helpOverlay := renderHelpSurface(interaction)

	var base string
	switch interaction.Layout {
	case LayoutSplit:
		resultsPaneOuterH := int(float64(totalHeight) * m.splitRatio)
		if resultsPaneOuterH < 3 {
			resultsPaneOuterH = 3
		}
		commandOuterH := totalHeight - resultsPaneOuterH
		if commandOuterH < 3 {
			commandOuterH = 3
		}
		resultsPaneContent := m.resultsPane.View(interaction)
		commandContent := m.command.View(interaction)
		resultsPaneActive := interaction.ActiveMode == ModeResultsPane
		resultsPanePane := m.renderBorderedPane(resultsPaneContent, "[1] Results", resultsPaneActive, w, resultsPaneOuterH-2)
		commandPane := m.renderBorderedPane(commandContent, "[2] Commands", !resultsPaneActive, w, commandOuterH-2)
		base = resultsPanePane + "\n" + commandPane
	case LayoutResultsPaneOnly:
		resultsPaneContent := m.resultsPane.View(interaction)
		base = m.renderBorderedPane(resultsPaneContent, "[1] Results", true, w, totalHeight-2)
	default: // LayoutCommandOnly
		commandContent := m.command.View(interaction)
		base = m.renderBorderedPane(commandContent, "[2] Commands", true, w, totalHeight-2)
	}

	baseH := totalHeight
	if helpOverlay != "" {
		base = helpOverlay + "\n" + base
		baseH = strings.Count(base, "\n") + 1
	}

	// Overlay popup window for history search and slash wizard.
	popupContent := renderHistorySearch(interaction)
	if popupContent == "" {
		popupContent = renderSlashWizard(interaction)
	}
	if popupContent != "" {
		maxW := min(popupBoxMaxWidth, w-4)
		if maxW >= popupBoxMinWidth {
			popupBox := renderPopupBox(popupContent, maxW)
			base = overlayCenter(base, popupBox, w, baseH)
		}
	}

	return base
}

func (m Model) statusBarView() string {
	interaction := m.state.Interaction
	var parts []string

	// Running indicator
	if running := interaction.Running; running != nil {
		parts = append(parts, formatRunningIndicator(running))
	}

	// Connection name
	if name := strings.TrimSpace(m.session.ConnectionName); name != "" {
		if color := strings.TrimSpace(m.session.ConnectionColor); color != "" {
			name = lipgloss.NewStyle().Foreground(resolveColor(color)).Render(name)
		}
		parts = append(parts, name)
	}

	// Keybind hints
	if m.state.App.Current == StateReady {
		parts = append(parts, m.command.FooterHints(interaction))
	} else {
		parts = append(parts, "ctrl+c quit")
	}

	bar := strings.Join(parts, " | ")

	// Pad/truncate to terminal width
	if m.width > 0 {
		bar = padOrTruncate(bar, m.width)
	}

	return appTheme.footer.Render(bar)
}

func (m Model) statusDescriptionView() string {
	status := strings.TrimSpace(m.state.Status)
	if status == "" {
		status = " "
	}

	line := status
	if m.width > 0 {
		line = padOrTruncate(line, m.width)
	}

	return appTheme.metaLine.Render(line)
}

func padOrTruncate(s string, width int) string {
	displayWidth := ansi.StringWidth(s)
	if displayWidth >= width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-displayWidth)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case msg.String() == "enter":
		// Enter always submits when a slash wizard popup is open.
		if m.state.Interaction.SlashWizard != nil {
			return func() tea.Msg { return submitIntentMsg{} }
		}
		// Enter submits when statement is complete (ends with ;), otherwise
		// falls through to textarea to insert a newline for multi-line SQL.
		m.syncCurrentSQL()
		currentSQL := m.command.Value()
		if isCompleteSQLStatement(currentSQL) || isSlashCommandText(currentSQL) {
			return func() tea.Msg { return submitIntentMsg{} }
		}
		return nil
	case key.Matches(msg, keys.LayoutCommandOnly):
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
	case key.Matches(msg, keys.Submit):
		return func() tea.Msg { return submitIntentMsg{} }
	case key.Matches(msg, keys.NextSuggestion):
		if m.state.Interaction.SlashWizard != nil {
			return func() tea.Msg { return slashWizardMoveIntentMsg{Delta: 1} }
		}
		return nil
	case key.Matches(msg, keys.PrevSuggestion):
		if m.state.Interaction.SlashWizard != nil {
			return func() tea.Msg { return slashWizardMoveIntentMsg{Delta: -1} }
		}
		return nil
	case key.Matches(msg, keys.Cancel):
		if m.state.Interaction.Running != nil {
			return func() tea.Msg { return cancelRunningIntentMsg{} }
		}
		if wizard := m.state.Interaction.SlashWizard; wizard != nil {
			if wizard.Step == SlashCommandWizardStepTarget {
				// If a filter is active, clear it first
			if strings.TrimSpace(wizard.TargetFilter) != "" {
				m.updateWizardTargetFilter("")
				return nopCmd
			}
				if wizard.DirectInvocation {
					return func() tea.Msg { return slashWizardCloseIntentMsg{} }
				}
				return func() tea.Msg { return slashWizardBackIntentMsg{} }
			}
			return func() tea.Msg { return slashWizardCloseIntentMsg{} }
		}
		// If the autocomplete dropdown is visible, dismiss it and preserve input.
		if m.command.AutocompleteVisible(m.state.Interaction) {
			m.command.DismissAutocomplete()
		}
		return nopCmd
	case key.Matches(msg, keys.History):
		return func() tea.Msg { return historyIntentMsg{} }
	case key.Matches(msg, keys.Help):
		return func() tea.Msg { return toggleHelpIntentMsg{} }
	case key.Matches(msg, keys.SwitchMode):
		return func() tea.Msg { return switchModeIntentMsg{} }
	case msg.String() == "ctrl+q":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }
	case msg.String() == "ctrl+w":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }
	case msg.String() == "ctrl+3", msg.String() == "alt+3":
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
	case msg.String() == "ctrl+z":
		return func() tea.Msg { return toggleZoomIntentMsg{} }
	case m.state.Interaction.SlashWizard != nil && m.state.Interaction.SlashWizard.Step == SlashCommandWizardStepTarget &&
		(msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete"):
		wizard := m.state.Interaction.SlashWizard
		m.updateWizardTargetFilter(trimLastRune(wizard.TargetFilter))
		return nopCmd
	case m.state.Interaction.SlashWizard != nil && m.state.Interaction.SlashWizard.Step == SlashCommandWizardStepTarget &&
		msg.String() == "space":
		wizard := m.state.Interaction.SlashWizard
		m.updateWizardTargetFilter(wizard.TargetFilter + " ")
		return nopCmd
	case m.state.Interaction.SlashWizard != nil && m.state.Interaction.SlashWizard.Step == SlashCommandWizardStepTarget &&
		len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		wizard := m.state.Interaction.SlashWizard
		m.updateWizardTargetFilter(wizard.TargetFilter + msg.Text)
		return nopCmd
	default:
		return nil
	}
}

func (m Model) handleLayoutKey(msg tea.KeyPressMsg) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case key.Matches(msg, keys.LayoutCommandOnly), msg.String() == "ctrl+3", msg.String() == "alt+3":
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
	case msg.String() == "ctrl+q":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }
	case msg.String() == "ctrl+w":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }
	case msg.String() == "ctrl+z":
		return func() tea.Msg { return toggleZoomIntentMsg{} }
	default:
		return nil
	}
}

func (m *Model) handleResultsPanePagingKey(msg tea.KeyPressMsg) bool {
	key := msg.String()
	isScroll := key == "ctrl+u" || key == "ctrl+d"
	isPaging := key == "ctrl+p" || key == "ctrl+n"
	if !isScroll && !isPaging {
		return false
	}
	if m.state.Interaction.ActiveMode != ModeResultsPane {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-page", "Results Pane has no rows to page.")
		return true
	}

	if isScroll {
		// ctrl+d scrolls down, ctrl+u scrolls up (vim-style half-page scroll)
		// Scrolling must NOT change the current page; it only moves the selection
		// within the bounds of the currently visible page.
		result := latest.PreservedResult
		if len(result.Rows) == 0 {
			return true
		}
		m.resultsPane.syncSelection(m.state.Interaction)
		page := resultsPanePageContextFor(m.state.Interaction.ViewerPage, len(result.Rows))
		pageMinRow := page.StartRow - 1 // inclusive lower bound (0-indexed)
		pageMaxRow := page.EndRow - 1   // inclusive upper bound (0-indexed)
		scrollAmount := max(1, m.resultsPane.height/2)
		if key == "ctrl+d" {
			m.resultsPane.selectedRow = min(m.resultsPane.selectedRow+scrollAmount, pageMaxRow)
		} else {
			m.resultsPane.selectedRow = max(m.resultsPane.selectedRow-scrollAmount, pageMinRow)
		}
		m.resultsPane.selectionActive = true
		// Do not call SetViewerPage — the page must not change on scroll.
		return true
	}

	// ctrl+p = prev page, ctrl+n = next page
	previous := m.state.Interaction.ViewerPage
	if key == "ctrl+p" {
		m.state.ChangeViewerPage(-1)
	} else {
		m.state.ChangeViewerPage(1)
	}

	page := resultsPanePageContextFor(m.state.Interaction.ViewerPage, len(latest.PreservedResult.Rows))
	if m.state.Interaction.ViewerPage == previous {
		if previous == 0 {
			m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Already at the first Results Pane page (%s).", formatResultsPaneRowRange(page)))
			return true
		}
		m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Already at the last Results Pane page (%s).", formatResultsPaneRowRange(page)))
		return true
	}

	m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Showing Results Pane page %d/%d (%s).", page.Number, page.TotalPages, formatResultsPaneRowRange(page)))
	return true
}

func (m *Model) handleResultsPaneNavigationKey(msg tea.KeyPressMsg) bool {
	if m.state.Interaction.ActiveMode != ModeResultsPane {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}

	page, handled := m.resultsPane.Navigate(msg, m.state.Interaction)
	if !handled {
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	m.state.SetViewerPage(page)
	return true
}

func (m *Model) handleResultsPaneSelectionKey(msg tea.KeyPressMsg) bool {
	if msg.String() != "space" && msg.String() != " " {
		return false
	}
	if m.state.Interaction.ActiveMode != ModeResultsPane {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	row, selected, handled := m.resultsPane.ToggleSelectedRow(&m.state.Interaction)
	if !handled {
		m.state.SetPendingIntent(IntentNone, "viewer-select", "Results Pane has no rows to select.")
		return true
	}

	if selected {
		m.state.SetPendingIntent(IntentNone, "viewer-select", fmt.Sprintf("Selected row %d (%d total).", row+1, len(m.state.Interaction.LatestResult.SelectedRows)))
		return true
	}

	m.state.SetPendingIntent(IntentNone, "viewer-select", fmt.Sprintf("Unselected row %d (%d total).", row+1, len(m.state.Interaction.LatestResult.SelectedRows)))
	return true
}

func (m *Model) handleResultsPaneComposeKey(msg tea.KeyPressMsg) bool {
	if m.state.Interaction.ActiveMode != ModeResultsPane {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}

	if len(msg.Text) != 1 || msg.Mod.Contains(tea.ModAlt) {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}

	switch []rune(msg.Text)[0] {
	case 'y':
		if m.resultsPane.pendingAction != resultsPanePendingActionComposeInsert {
			m.resultsPane.pendingAction = resultsPanePendingActionComposeInsert
			m.state.SetPendingIntent(IntentNone, "viewer-compose", "Press y again to load INSERT for the selected row into command mode.")
			return true
		}
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return m.composeResultsPaneInsert()
	case 'c':
		if m.resultsPane.pendingAction != resultsPanePendingActionComposeUpdate {
			m.resultsPane.pendingAction = resultsPanePendingActionComposeUpdate
			return true
		}
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return m.composeResultsPaneUpdate()
	case 'd':
		if m.resultsPane.pendingAction != resultsPanePendingActionComposeDelete {
			m.resultsPane.pendingAction = resultsPanePendingActionComposeDelete
			m.state.SetPendingIntent(IntentNone, "viewer-compose", "Press d again to load DELETE for the selected row into command mode.")
			return true
		}
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return m.composeResultsPaneDelete()
	default:
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}
}

func (m *Model) composeResultsPaneInsert() bool {
	if m.state.Interaction.LatestResult == nil || m.state.Interaction.LatestResult.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", "Results Pane has no rows to compose.")
		return true
	}

	result, err := composeResultsPaneInsertSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", fmt.Sprintf("Could not compose INSERT: %v", err))
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeHistorySearch()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, ModeResultsPane))
	m.state.SetActiveMode(ModeCommand)
	m.state.SetPendingModeSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "viewer-compose", resultsPaneComposeStatus(result))
	m.syncPaneSizes()
	return true
}

func (m *Model) composeResultsPaneUpdate() bool {
	if m.state.Interaction.LatestResult == nil || m.state.Interaction.LatestResult.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", "Results Pane has no rows to compose.")
		return true
	}

	result, err := composeResultsPaneUpdateSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", fmt.Sprintf("Could not compose UPDATE: %v", err))
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeHistorySearch()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, ModeResultsPane))
	m.state.SetActiveMode(ModeCommand)
	m.state.SetPendingModeSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "viewer-compose", resultsPaneComposeStatus(result))
	m.syncPaneSizes()
	return true
}

func (m *Model) composeResultsPaneDelete() bool {
	if m.state.Interaction.LatestResult == nil || m.state.Interaction.LatestResult.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", "Results Pane has no rows to compose.")
		return true
	}

	result, err := composeResultsPaneDeleteSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "viewer-compose", fmt.Sprintf("Could not compose DELETE: %v", err))
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeHistorySearch()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, ModeResultsPane))
	m.state.SetActiveMode(ModeCommand)
	m.state.SetPendingModeSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "viewer-compose", resultsPaneComposeStatus(result))
	m.syncPaneSizes()
	return true
}

func (m *Model) syncCurrentSQL() {
	m.state.SetCurrentSQL(m.command.Value())
}

func (m *Model) syncSessionHistorySnapshot() {
	if m.history == nil {
		m.state.SetSessionHistory(nil)
		return
	}

	entries := m.history.Entries()
	contexts := make([]HistoryEntryContext, 0, len(entries))
	for _, entry := range entries {
		contexts = append(contexts, HistoryEntryContext{
			SQL:            entry.SQL,
			ConnectionName: entry.ConnectionName,
			ExecutedAt:     entry.ExecutedAt,
		})
	}
	m.state.SetSessionHistory(contexts)
}

func (m *Model) appendSessionHistory(statement, resultSummary string) error {
	if m.history == nil || strings.TrimSpace(statement) == "" {
		return nil
	}

	err := m.history.Append(apphistory.Entry{
		SQL:            statement,
		ConnectionName: m.session.ConnectionName,
		ExecutedAt:     time.Now().UTC(),
		ResultSummary:  resultSummary,
	})
	m.syncSessionHistorySnapshot()
	return err
}

func withHistoryWarning(status string, err error) string {
	if err == nil {
		return status
	}

	return fmt.Sprintf("%s History was not persisted: %v", status, err)
}

func latestHistoryEntry(entries []HistoryEntryContext) *HistoryEntryContext {
	if len(entries) == 0 {
		return nil
	}

	entry := entries[len(entries)-1]
	return &entry
}

func (m Model) dialectName() string {
	if m.adapter != nil && m.adapter.Dialect() != nil {
		return m.adapter.Dialect().Name()
	}

	return strings.TrimSpace(m.session.ConnectionType)
}

func (m Model) refreshAutocompleteSchemaCmd() tea.Cmd {
	return loadAutocompleteSchemaCmd(m.adapter, m.loader)
}

func (m *Model) syncAutocompleteSchemaSnapshot() {
	if m.cache == nil {
		m.state.SetAutocompleteSchema(nil)
		return
	}

	m.state.SetAutocompleteSchema(m.cache.Snapshot())
}

func loadAutocompleteSchemaCmd(adapter *db.SQLAdapter, loader autocompleteSchemaLoader) tea.Cmd {
	if adapter == nil {
		return nil
	}
	if loader == nil {
		loader = loadAutocompleteSchema
	}

	return func() tea.Msg {
		schema, err := loader(context.Background(), adapter)
		return autocompleteSchemaLoadedMsg{Schema: schema, Err: err}
	}
}

func (m *Model) startExecution(label, status string, execute func(context.Context, time.Time) tea.Cmd) tea.Cmd {
	if execute == nil {
		return nil
	}
	if m.executionCancel != nil {
		m.executionCancel()
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultInteractiveExecutionTimeout)
	m.executionCancel = cancel
	m.state.SetRunningStatementContext(newRunningStatementContext(label, startedAt))
	m.state.SetReady("")
	m.state.SetPendingIntent(IntentSubmit, "submit", executionStatus(status, defaultInteractiveExecutionTimeout))
	return tea.Batch(execute(ctx, startedAt), runningTickCmd(startedAt))
}

func (m *Model) clearExecution() {
	if m.executionCancel == nil {
		return
	}
	m.executionCancel()
	m.executionCancel = nil
}

func loadAutocompleteSchema(ctx context.Context, adapter *db.SQLAdapter) (*AutocompleteSchemaContext, error) {
	if adapter == nil {
		return nil, nil
	}

	tables, err := adapter.Tables(ctx, db.TableFilter{})
	if err != nil {
		if errors.Is(err, db.ErrMetadataUnsupported) {
			return nil, nil
		}
		return nil, err
	}

	schema := &AutocompleteSchemaContext{Tables: make([]AutocompleteTableContext, 0, len(tables))}
	for _, table := range tables {
		entry := AutocompleteTableContext{Schema: table.Schema, Name: table.Name, ColumnTypes: make(map[string]string)}
		columns, err := adapter.Columns(ctx, db.TableRef{Catalog: table.Catalog, Schema: table.Schema, Name: table.Name})
		if err != nil {
			if !errors.Is(err, db.ErrMetadataUnsupported) {
				return nil, err
			}
		} else {
			for _, column := range columns {
				entry.Columns = append(entry.Columns, column.Name)
				if column.Type != "" {
					entry.ColumnTypes[strings.ToLower(column.Name)] = column.Type
				}
			}
		}
		schema.Tables = append(schema.Tables, entry)
	}

	return schema, nil
}

func executeSlashCommandCmd(commandContext slashCommandContext, parsed slashCommand) func(context.Context, time.Time) tea.Cmd {
	return func(ctx context.Context, _ time.Time) tea.Cmd {
		return func() tea.Msg {
			result, err := dispatchSlashCommand(ctx, commandContext, parsed)
			return slashCommandExecutedMsg{Command: parsed, Result: result, ResultSummary: summarizeSlashCommandResult(parsed, result, err), Err: err}
		}
	}
}

func summarizeSlashCommandResult(command slashCommand, result slashCommandResult, err error) string {
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	status := defaultStatus(result.Status, fmt.Sprintf("%s completed.", command.DisplayName))
	if strings.TrimSpace(result.Status) != "" {
		return status
	}
	if result.Statement != nil {
		return describeStatementStatus(result.Statement)
	}

	return status
}

func (m *Model) openTableSelectionForCommand(parsed *slashCommand) (Model, tea.Cmd) {
	commandCtx := slashCommandContext{
		Session: m.session,
		Adapter: m.adapter,
		Dialect: m.adapterDialect(),
		Query:   m.state.Interaction.snapshot(),
	}

	commands := buildSlashWizardCommands()
	selectedIdx := 0
	for i, cmd := range commands {
		if cmd.Name == parsed.Name {
			selectedIdx = i
			break
		}
	}

	targets, err := buildSlashWizardTargets(context.Background(), commandCtx)
	if err != nil {
		m.state.SetReady(fmt.Sprintf("%s failed: %v", parsed.DisplayName, err))
		return *m, nil
	}
	if len(targets) == 0 {
		m.state.SetReady(fmt.Sprintf("%s: no tables available.", parsed.DisplayName))
		return *m, nil
	}

	wizard := &SlashCommandWizardContext{
		Step:             SlashCommandWizardStepTarget,
		Commands:         commands,
		SelectedCommand:  selectedIdx,
		Targets:          targets,
		SelectedTarget:   0,
		DirectInvocation: true,
	}
	m.state.SetSlashWizardContext(wizard)
	m.state.SetReady(fmt.Sprintf("Choose a table for %s and press enter.", parsed.DisplayName))
	return *m, nil
}

func (m *Model) openCommandWizard() (Model, tea.Cmd) {
	commands := buildSlashWizardCommands()
	if len(commands) == 0 {
		m.state.SetReady("/commands: no slash commands available.")
		return *m, nil
	}
	wizard := &SlashCommandWizardContext{
		Step:     SlashCommandWizardStepCommand,
		Commands: commands,
	}
	m.state.SetSlashWizardContext(wizard)
	m.state.SetReady("Choose a slash command and press enter.")
	return *m, nil
}

func (m *Model) submitSlashWizard(wizard *SlashCommandWizardContext) (Model, tea.Cmd) {
	selectedCommand, ok := slashWizardCommandByIndex(wizard)
	if !ok {
		m.state.SetReady("Slash command wizard is empty.")
		m.state.SetSlashWizardContext(nil)
		return *m, nil
	}

	if selectedCommand.NeedsTarget {
		if wizard.Step != SlashCommandWizardStepTarget {
			nextWizard, err := buildSlashWizardFromCommand(context.Background(), slashCommandContext{
				Session: m.session,
				Adapter: m.adapter,
				Dialect: m.adapterDialect(),
				Query:   m.state.Interaction.snapshot(),
			}, wizard.Commands, selectedCommand, wizard.SelectedCommand)
			if err != nil {
				m.state.SetReady(fmt.Sprintf("/commands failed: %v", err))
				m.state.SetSlashWizardContext(nil)
				m.state.SetLatestResultContext(nil)
				return *m, nil
			}
			if nextWizard == nil || len(nextWizard.Targets) == 0 {
				m.state.SetReady(fmt.Sprintf("/commands: no tables available for %s.", selectedCommand.DisplayName))
				m.state.SetSlashWizardContext(nil)
				return *m, nil
			}
			m.state.SetSlashWizardContext(nextWizard)
			m.state.SetReady(fmt.Sprintf("Choose a table for %s and press enter.", selectedCommand.DisplayName))
			return *m, nil
		}

		selectedTarget, ok := slashWizardFilteredTargetByIndex(wizard)
		if !ok {
			m.state.SetReady(fmt.Sprintf("/commands: choose a table for %s.", selectedCommand.DisplayName))
			return *m, nil
		}

		parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
		return *m, m.startExecution(parsed.DisplayName, fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName), executeSlashCommandCmd(slashCommandContext{
			Session: m.session,
			Adapter: m.adapter,
			Dialect: m.adapterDialect(),
			Query:   m.state.Interaction.snapshot(),
		}, parsed))
	}

	parsed := buildSlashWizardCommand(selectedCommand, nil)
	return *m, m.startExecution(parsed.DisplayName, fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName), executeSlashCommandCmd(slashCommandContext{
		Session: m.session,
		Adapter: m.adapter,
		Dialect: m.adapterDialect(),
		Query:   m.state.Interaction.snapshot(),
	}, parsed))
}

func (m *Model) moveSlashWizardSelection(delta int) {
	wizard := cloneSlashCommandWizardContext(m.state.Interaction.SlashWizard)
	if wizard == nil || delta == 0 {
		return
	}

	switch wizard.Step {
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(wizard.Targets, wizard.TargetFilter)
		if len(filtered) == 0 {
			return
		}
		wizard.SelectedTarget = wrapSelection(wizard.SelectedTarget+delta, len(filtered))
		m.state.SetSlashWizardContext(wizard)
		m.state.SetReady(fmt.Sprintf("Selected table %s.", filtered[wizard.SelectedTarget].Display))
	default:
		if len(wizard.Commands) == 0 {
			return
		}
		wizard.SelectedCommand = wrapSelection(wizard.SelectedCommand+delta, len(wizard.Commands))
		selectedCommand, _ := slashWizardCommandByIndex(wizard)
		m.state.SetSlashWizardContext(wizard)
		m.state.SetReady(fmt.Sprintf("Selected %s.", selectedCommand.DisplayName))
	}
}

func (m *Model) updateWizardTargetFilter(filter string) {
	wizard := cloneSlashCommandWizardContext(m.state.Interaction.SlashWizard)
	if wizard == nil || wizard.Step != SlashCommandWizardStepTarget {
		return
	}
	wizard.TargetFilter = filter
	wizard.SelectedTarget = 0
	m.state.SetSlashWizardContext(wizard)
	filtered := filterWizardTargets(wizard.Targets, filter)
	if len(filtered) == 0 {
		m.state.SetReady(fmt.Sprintf("No tables match %q.", filter))
	} else {
		m.state.SetReady(fmt.Sprintf("%d table(s) match filter.", len(filtered)))
	}
}

func (m *Model) stepBackSlashWizard() {
	wizard := cloneSlashCommandWizardContext(m.state.Interaction.SlashWizard)
	if wizard == nil || wizard.Step != SlashCommandWizardStepTarget {
		return
	}
	wizard.Step = SlashCommandWizardStepCommand
	wizard.Targets = nil
	wizard.SelectedTarget = 0
	m.state.SetSlashWizardContext(wizard)
	selectedCommand, _ := slashWizardCommandByIndex(wizard)
	m.state.SetReady(fmt.Sprintf("Choose a command. %s is selected.", selectedCommand.DisplayName))
}

func wrapSelection(index, size int) int {
	if size <= 0 {
		return 0
	}
	index %= size
	if index < 0 {
		index += size
	}
	return index
}

func executeStatementCmd(adapter *db.SQLAdapter, statement string) func(context.Context, time.Time) tea.Cmd {
	return func(ctx context.Context, _ time.Time) tea.Cmd {
		return func() tea.Msg {
			if adapter == nil {
			return statementExecutedMsg{Statement: statement, ResultSummary: "error: adapter is required", Err: fmt.Errorf("adapter is required")}
		}

		result, err := adapter.ExecuteStatementContext(ctx, statement, db.ResultOptions{Source: inferQuerySourceTable(statement)})
		return statementExecutedMsg{Statement: statement, Result: result, ResultSummary: summarizeStatementResult(result, err), Err: err}
		}
	}
}

func executionStatus(status string, timeout time.Duration) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "Execution requested."
	}
	return fmt.Sprintf("%s Press esc to cancel; timeout after %s.", status, formatExecutionTimeout(timeout))
}

func formatExecutionTimeout(timeout time.Duration) string {
	if timeout <= 0 {
		return "0s"
	}
	if timeout%time.Second == 0 {
		return timeout.String()
	}
	return timeout.Round(100 * time.Millisecond).String()
}

func runningLabel(running *RunningStatementContext) string {
	if running == nil || strings.TrimSpace(running.Label) == "" {
		return "query"
	}
	return running.Label
}

func runningElapsed(running *RunningStatementContext) time.Duration {
	if running == nil {
		return 0
	}
	if running.Elapsed > 0 {
		return running.Elapsed
	}
	if running.StartedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(running.StartedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func executionInterruptedStatus(running *RunningStatementContext, err error) (string, bool) {
	if err == nil {
		return "", false
	}

	label := runningLabel(running)
	elapsed := formatRunningElapsed(runningElapsed(running))
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Sprintf("Cancelled %s after %s.", label, elapsed), true
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("%s timed out after %s. Press esc sooner to cancel manually, or retry with a narrower query.", label, elapsed), true
	default:
		return "", false
	}
}

func runningTickCmd(startedAt time.Time) tea.Cmd {
	if startedAt.IsZero() {
		return nil
	}

	return tea.Tick(100*time.Millisecond, func(now time.Time) tea.Msg {
		return runningTickMsg{StartedAt: startedAt, Now: now}
	})
}

func summarizeStatementResult(result *db.StatementResult, err error) string {
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}

	return describeStatementStatus(result)
}

func (m Model) adapterDialect() db.Dialect {
	if m.adapter == nil {
		return nil
	}

	return m.adapter.Dialect()
}

func (m Model) resultOriginMode() AppMode {
	if m.state.Interaction.ActiveMode == ModeHistorySearch {
		return ModeCommand
	}
	if layoutShowsCommand(m.state.Interaction.Layout) {
		return ModeCommand
	}
	return m.state.Interaction.ActiveMode
}

func buildLatestResultContext(query string, originMode AppMode, result *db.StatementResult) *LatestResultContext {
	if result == nil {
		return nil
	}

	context := &LatestResultContext{
		Statement:     query,
		OriginMode:    originMode,
		StatementKind: result.Kind,
		RowsAffected:  cloneInt64Pointer(result.RowsAffected),
		LastInsertID:  cloneInt64Pointer(result.LastInsertID),
	}

	if result.ResultSet != nil {
		context.PreservedResult = cloneResultSet(result.ResultSet)
		inferredSource := inferQuerySourceTable(query)
		if context.PreservedResult != nil && context.PreservedResult.Source == nil {
			context.PreservedResult.Source = inferredSource
		}
		context.InlineResult = buildInlineResultSet(query, result.ResultSet)
		if context.InlineResult != nil && context.InlineResult.Source == nil {
			if inferredSource != nil {
				source := *inferredSource
				context.InlineResult.Source = &source
			}
		}
		context.InlineRowsTruncated = resultSetRowCount(context.InlineResult) < resultSetRowCount(context.PreservedResult)
	}

	return context
}

func buildModeSwitchContext(fromLayout, toLayout AppLayout, fromMode, toMode AppMode, latest *LatestResultContext) *ModeSwitchContext {
	return &ModeSwitchContext{
		FromLayout:    fromLayout,
		ToLayout:      toLayout,
		FromMode:      fromMode,
		ToMode:        toMode,
		ResultContext: cloneLatestResultContext(latest),
	}
}

func nextModeForIntent(current AppMode) AppMode {
	switch current {
	case ModeResultsPane:
		return ModeCommand
	default:
		return ModeResultsPane
	}
}

func nextLayoutForModeIntent(currentLayout AppLayout, currentMode AppMode) AppLayout {
	switch currentLayout {
	case LayoutSplit:
		return LayoutSplit
	case LayoutResultsPaneOnly:
		return LayoutCommandOnly
	default:
		if currentMode == ModeResultsPane {
			return LayoutCommandOnly
		}
		return LayoutResultsPaneOnly
	}
}

func describeModeSwitchStatus(context *ModeSwitchContext) string {
	if context == nil {
		return "Mode switch requested."
	}

	if context.ToMode == ModeCommand {
		if context.ToLayout == LayoutSplit {
			return "Focused the command line in split layout."
		}
		return "Returned to command line."
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		if context.ToLayout == LayoutSplit {
			return "Focused the Results Pane in split layout. Run a query that returns rows to populate it."
		}
		return "Record viewer is available after running a query that returns tabular results."
	}

	result := context.ResultContext.PreservedResult
	if context.ToLayout == LayoutSplit {
		return fmt.Sprintf("Focused the Results Pane in split layout for %d row(s) across %d column(s).", len(result.Rows), len(result.Columns))
	}
	return fmt.Sprintf("Opened Results Pane for %d row(s) across %d column(s).", len(result.Rows), len(result.Columns))
}

func (m *Model) applyModeSwitch(context *ModeSwitchContext) {
	m.state.SetReady("")
	m.state.SetPendingModeSwitch(context)

	if context == nil {
		m.state.SetPendingIntent(IntentSwitchMode, "switch-mode", describeModeSwitchStatus(nil))
		return
	}

	if context.ToMode == ModeCommand {
		m.closeHistorySearch()
		m.command.Focus()
		m.state.SetLayout(context.ToLayout)
		m.state.SetActiveMode(ModeCommand)
		m.state.SetPendingModeSwitch(nil)
		m.state.SetPendingIntent(IntentNone, "switch-mode", describeModeSwitchStatus(context))
		return
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		if context.ToLayout == LayoutSplit {
			m.closeHistorySearch()
			m.command.Blur()
			m.state.SetLayout(context.ToLayout)
			m.state.SetActiveMode(context.ToMode)
			m.state.SetPendingModeSwitch(nil)
			m.state.SetPendingIntent(IntentNone, "switch-mode", describeModeSwitchStatus(context))
			return
		}
		m.state.SetPendingIntent(IntentSwitchMode, "switch-mode", describeModeSwitchStatus(context))
		return
	}
	m.closeHistorySearch()
	m.command.Blur()
	m.state.SetLayout(context.ToLayout)
	m.state.SetActiveMode(context.ToMode)
	m.state.SetPendingModeSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "switch-mode", describeModeSwitchStatus(context))
}

func (m *Model) applyLayoutSwitch(layout AppLayout) {
	current := m.state.Interaction.Layout
	if layout == "" {
		layout = LayoutCommandOnly
	}
	m.state.SetPendingModeSwitch(nil)

	if layout == current {
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Layout already set to %s.", layoutLabel(layout)))
		return
	}

	m.state.SetReady("")
	m.state.SetLayout(layout)

	switch layout {
	case LayoutResultsPaneOnly:
		if m.state.Interaction.ActiveMode == ModeHistorySearch {
			m.closeHistorySearch()
		}
		m.command.Blur()
		m.state.SetActiveMode(ModeResultsPane)
		if latest := m.state.Interaction.LatestResult; latest != nil && latest.PreservedResult != nil {
			m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s with %d row(s) visible.", layoutLabel(layout), len(latest.PreservedResult.Rows)))
			return
		}
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s. Run a query that returns rows to populate the viewer.", layoutLabel(layout)))
	case LayoutSplit:
		if m.state.Interaction.ActiveMode == ModeResultsPane {
			m.command.Blur()
		} else {
			m.command.Focus()
		}
		if m.state.Interaction.ActiveMode == ModeHistorySearch {
			m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s. History search stays open in the command line.", layoutLabel(layout)))
			return
		}
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s.", layoutLabel(layout)))
	case LayoutCommandOnly:
		if m.state.Interaction.ActiveMode == ModeResultsPane {
			m.state.SetActiveMode(ModeCommand)
		}
		m.command.Focus()
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s.", layoutLabel(layout)))
	default:
		m.command.Focus()
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Switched to %s.", layoutLabel(m.state.Interaction.Layout)))
	}
}

func (m *Model) handleFocusPane(pane PaneTarget) {
	m.state.SetReady("")
	m.state.SetPendingModeSwitch(nil)
	switch pane {
	case PaneResults:
		m.closeHistorySearch()
		switch m.state.Interaction.Layout {
		case LayoutCommandOnly:
			m.command.Blur()
			m.state.SetLayout(LayoutSplit)
			m.state.SetActiveMode(ModeResultsPane)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Switched to split layout with results pane focused.")
		case LayoutResultsPaneOnly:
			m.state.SetActiveMode(ModeResultsPane)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Results pane is already focused.")
		default: // LayoutSplit
			m.command.Blur()
			m.state.SetActiveMode(ModeResultsPane)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Focused results pane.")
		}
	case PaneCommand:
		m.closeHistorySearch()
		switch m.state.Interaction.Layout {
		case LayoutResultsPaneOnly:
			m.command.Focus()
			m.state.SetLayout(LayoutSplit)
			m.state.SetActiveMode(ModeCommand)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Switched to split layout with command pane focused.")
		case LayoutCommandOnly:
			m.command.Focus()
			m.state.SetActiveMode(ModeCommand)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Command pane is already focused.")
		default: // LayoutSplit
			m.command.Focus()
			m.state.SetActiveMode(ModeCommand)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Focused command pane.")
		}
	}
}

func (m *Model) handleToggleZoom() {
	switch m.state.Interaction.Layout {
	case LayoutSplit:
		if m.state.Interaction.ActiveMode == ModeResultsPane {
			m.command.Blur()
			m.state.SetLayout(LayoutResultsPaneOnly)
			m.state.SetActiveMode(ModeResultsPane)
			m.state.SetPendingIntent(IntentNone, "zoom", "Zoomed results pane.")
		} else {
			m.command.Focus()
			m.state.SetLayout(LayoutCommandOnly)
			m.state.SetActiveMode(ModeCommand)
			m.state.SetPendingIntent(IntentNone, "zoom", "Zoomed command pane.")
		}
	case LayoutCommandOnly:
		m.command.Blur()
		m.state.SetLayout(LayoutSplit)
		m.state.SetActiveMode(ModeCommand)
		m.command.Focus()
		m.state.SetPendingIntent(IntentNone, "zoom", "Returned to split layout.")
	case LayoutResultsPaneOnly:
		m.state.SetLayout(LayoutSplit)
		m.state.SetActiveMode(ModeResultsPane)
		m.command.Blur()
		m.state.SetPendingIntent(IntentNone, "zoom", "Returned to split layout.")
	}
}

func renderHelpSurface(st InteractionState) string {
	if !st.HelpVisible {
		return ""
	}

	sections := []helpSection{{
		Title: "Help",
		Lines: []string{
			"alt+h toggle help",
			"ctrl+c quit",
		},
	}}

	commandLines := []string{
		"enter submit SQL or slash command",
		"ctrl+r open history search",
		"ctrl+y accept suggestion; ctrl+n/ctrl+p move suggestion",
		"ctrl+x switch focus; ctrl+z zoom; ctrl+1 focus results; ctrl+2 focus command; ctrl+3 command layout",
	}
	if st.ActiveMode == ModeHistorySearch {
		commandLines = append(commandLines, "history search: enter restore; ctrl+r older; ctrl+n newer; esc close")
	}
	sections = append(sections, helpSection{Title: "Command mode", Lines: commandLines})

	viewerLines := []string{
		"arrows/hjkl move cell; space toggle selected row",
		"yy/cc/dd load INSERT/UPDATE/DELETE into command mode",
		":w [file] export selected rows or current result rows",
		"ctrl+u/ctrl+d scroll; ctrl+p/ctrl+n page; ctrl+x focus command",
	}
	if st.SlashWizard != nil {
		viewerLines = append(viewerLines, "slash wizard: enter confirm; ctrl+n/ctrl+p move; esc back or close")
	}
	sections = append(sections, helpSection{Title: "Record viewer", Lines: viewerLines})

	if st.Layout == LayoutSplit {
		var layoutLines []string
		if st.ActiveMode == ModeResultsPane {
			layoutLines = []string{"Record viewer [active]", "Command line"}
		} else {
			layoutLines = []string{"Record viewer", "Command line [active]"}
		}
		sections = append(sections, helpSection{Title: "Layout", Lines: layoutLines})
	}

	if st.ActiveMode == ModeHistorySearch {
		sections = append(sections, helpSection{Title: "History search", Lines: []string{
			"type to filter recent commands; enter restore selected entry",
			"ctrl+r or up select older match; ctrl+n or down select newer match",
			"esc close history search",
		}})
	}

	if st.SlashWizard != nil {
		sections = append(sections, helpSection{Title: "Command wizard", Lines: []string{
			"/commands opens the guided slash command wizard",
			"enter confirm selection; ctrl+n/ctrl+p move selection",
			"esc closes command selection or steps back from table selection",
		}})
	}

	slashLines := []string{
		"/help lists slash commands; /commands opens the guided wizard",
		"/tables and /columns inspect database metadata",
		"/select, /insert, /update, /delete expand SQL templates for review",
		"/create and /drop expand DDL templates for review",
	}
	slashLines = append(slashLines, slashCommandHelpLines()...)
	sections = append(sections, helpSection{Title: "Slash commands", Lines: slashLines})

	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if len(section.Lines) == 0 {
			continue
		}
		lines := make([]string, 0, len(section.Lines)+1)
		lines = append(lines, appTheme.panelTitle.Render(section.Title+":"))
		for _, line := range section.Lines {
			lines = append(lines, appTheme.panelText.Render("  "+line))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n\n")
}

func layoutShowsCommand(layout AppLayout) bool {
	return layout != LayoutResultsPaneOnly
}

func layoutLabel(layout AppLayout) string {
	switch layout {
	case LayoutSplit:
		return "split"
	case LayoutResultsPaneOnly:
		return "viewer only"
	default:
		return "command only"
	}
}

func buildInlineResultSet(query string, result *db.ResultSet) *db.ResultSet {
	if result == nil {
		return nil
	}

	inline := cloneResultSet(result)
	if statementUsesLimitedInlineRows(query) && len(inline.Rows) > 5 {
		inline.Rows = append([]db.ResultRow(nil), inline.Rows[:5]...)
	}
	return inline
}

func statementUsesLimitedInlineRows(query string) bool {
	switch leadingSQLKeyword(query) {
	case "SELECT", "WITH":
		return true
	default:
		return false
	}
}

func leadingSQLKeyword(query string) string {
	runes := []rune(query)
	for i := 0; i < len(runes); {
		switch {
		case isSQLSpaceRune(runes[i]):
			i++
		case hasSQLRunePrefix(runes, i, '-', '-'):
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case hasSQLRunePrefix(runes, i, '/', '*'):
			i += 2
			for i < len(runes) && !hasSQLRunePrefix(runes, i, '*', '/') {
				i++
			}
			if i < len(runes) {
				i += 2
			}
		case isSQLKeywordRune(runes[i]):
			start := i
			for i < len(runes) && isSQLKeywordRune(runes[i]) {
				i++
			}
			return strings.ToUpper(string(runes[start:i]))
		default:
			return ""
		}
	}

	return ""
}

func hasSQLRunePrefix(runes []rune, index int, first, second rune) bool {
	return index+1 < len(runes) && runes[index] == first && runes[index+1] == second
}

func isSQLSpaceRune(value rune) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func isSQLKeywordRune(value rune) bool {
	return value == '_' || value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func resultSetRowCount(result *db.ResultSet) int {
	if result == nil {
		return 0
	}

	return len(result.Rows)
}

func describeStatementStatus(result *db.StatementResult) string {
	if result == nil {
		return "Statement executed successfully."
	}

	if result.Kind == db.StatementResultKindQuery && result.ResultSet != nil {
		rowCount := len(result.ResultSet.Rows)
		if rowCount == 0 {
			return "Query returned no rows."
		}
		if rowCount == 1 {
			return "Query returned 1 row."
		}
		return fmt.Sprintf("Query returned %d rows.", rowCount)
	}

	if result.RowsAffected != nil {
		if *result.RowsAffected == 1 {
			return "Statement executed successfully. 1 row affected."
		}
		return fmt.Sprintf("Statement executed successfully. %d rows affected.", *result.RowsAffected)
	}

	return "Statement executed successfully."
}

func isSlashCommandText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/")
}

func formatReplStatementOutput(result *db.StatementResult, err error) string {
	if err != nil {
		return FormatTerminalError(err)
	}
	if result == nil {
		return "Statement executed successfully."
	}
	if result.Kind == db.StatementResultKindExec {
		var parts []string
		if result.RowsAffected != nil {
			label := "rows"
			if *result.RowsAffected == 1 {
				label = "row"
			}
			parts = append(parts, fmt.Sprintf("%d %s affected", *result.RowsAffected, label))
		} else {
			parts = append(parts, "Statement executed successfully")
		}
		if result.LastInsertID != nil && *result.LastInsertID != 0 {
			parts = append(parts, fmt.Sprintf("last insert id %d", *result.LastInsertID))
		}
		return strings.Join(parts, "\n")
	}
	if result.ResultSet == nil {
		return "Statement executed successfully."
	}
	rowCount := len(result.ResultSet.Rows)
	if rowCount == 0 {
		return "(no rows)"
	}
	if rowCount == 1 {
		return "1 row."
	}
	return fmt.Sprintf("%d rows.", rowCount)
}

func formatReplSlashOutput(msg slashCommandExecutedMsg) string {
	if msg.Err != nil {
		return FormatTerminalError(msg.Err)
	}
	if msg.Result.Statement != nil {
		return formatReplStatementOutput(msg.Result.Statement, nil)
	}
	status := defaultStatus(msg.Result.Status, fmt.Sprintf("%s completed.", msg.Command.DisplayName))
	return status
}
