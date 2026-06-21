package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/export"
	apphistory "github.com/adwinying/sqlcery/internal/history"
	"github.com/adwinying/sqlcery/internal/tui"
)

type Model struct {
	session         Session
	history         *apphistory.History
	executionCancel context.CancelFunc
	command         commandModeModel
	resultsPane     resultsPaneModeModel
	state           SharedAppState
	schema          *AutocompleteSchemaContext
	loader          autocompleteSchemaLoader
	modals          []Modal
	width           int
	height          int
	splitRatio      float64
	pendingQuit     bool
}

type autocompleteSchemaLoader func(context.Context, *db.SQLAdapter) (*AutocompleteSchemaContext, error)

type modelDependencies struct {
	loader  autocompleteSchemaLoader
	history *apphistory.History
}

// nopCmd is a non-nil tea.Cmd that produces no message. Use it when a key
// event has been fully handled but no further action is needed — returning a
// non-nil cmd prevents the event from falling through to the command pane.
var nopCmd tea.Cmd = func() tea.Msg { return nil }

type submitIntentMsg struct{}

type cancelRunningIntentMsg struct{}

type notificationClearMsg struct {
	createdAt time.Time
}

func notificationClearCmd(createdAt time.Time) tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return notificationClearMsg{createdAt: createdAt}
	})
}

// notificationClearCmdIfSet returns a 3-second clear timer if the current
// notification is non-empty. Call after any SetReady/SetPendingIntent call.
func (m Model) notificationClearCmdIfSet() tea.Cmd {
	if m.state.Notification.Text == "" {
		return nil
	}
	return notificationClearCmd(m.state.Notification.CreatedAt)
}

// newNotificationClearCmdIfChanged returns a clear cmd only when the
// notification was replaced since prevCreatedAt. Use this at bool-handler
// call sites where the notification is set inside the handler.
func (m Model) newNotificationClearCmdIfChanged(prevCreatedAt time.Time) tea.Cmd {
	n := m.state.Notification
	if n.Text == "" || n.CreatedAt == prevCreatedAt {
		return nil
	}
	return notificationClearCmd(n.CreatedAt)
}

type historyIntentMsg struct{}

type historyNavBoundaryMsg struct{}

type toggleHelpIntentMsg struct{}

type toggleZoomIntentMsg struct{}

type switchPaneIntentMsg struct{}

type switchLayoutIntentMsg struct {
	Layout AppLayout
}


type focusPaneIntentMsg struct {
	Pane Pane
}

type clearInputIntentMsg struct{}

type composeResultsPaneIntentMsg struct {
	action string // "insert", "update", "delete"
}

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

func NewModel(session Session) Model {
	return newModelWithDependencies(session, modelDependencies{})
}

func newModelWithDependencies(session Session, deps modelDependencies) Model {
	loader := deps.loader
	if loader == nil {
		loader = loadAutocompleteSchema
	}

	sessionHistory := deps.history
	if sessionHistory == nil {
		sessionHistory = apphistory.NewHistory()
	}

	model := Model{
		session:     session,
		history:     sessionHistory,
		command:     newCommandModeModel(),
		resultsPane: newResultsPaneModeModel(),
		state:       NewSharedAppState(),
		loader:      loader,
		splitRatio:  0.65,
	}
	model.syncAutocompleteSchemaSnapshot()
	model.syncHistorySnapshot()

	return model
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return startupCompleteMsg{} }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPressMsg(msg)
	case submitIntentMsg:
		return m.handleSubmitIntent()
	case cancelRunningIntentMsg:
		return m.handleCancelRunningIntent()
	case statementExecutedMsg:
		return m.handleStatementExecuted(msg)
	case slashCommandExecutedMsg:
		return m.handleSlashCommandExecuted(msg)
	case toggleHelpIntentMsg:
		if m.currentModal() != nil && m.currentModal().Name() == ModalKeybindings {
			m.popModal()
			m.state.SetPendingIntent(IntentNone, "help", "", NotificationNone)
		} else {
			m.pushModal(&helpModal{
				contextModal: m.state.Interaction.ActiveModal,
				contextPane:  m.state.Interaction.ActivePane,
			})
			m.state.SetPendingIntent(IntentNone, "help", "", NotificationNone)
		}
		return m, m.notificationClearCmdIfSet()
	case composeResultsPaneIntentMsg:
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		switch msg.action {
		case "insert":
			m.composeResultsPaneInsert()
		case "update":
			m.composeResultsPaneUpdate()
		case "delete":
			m.composeResultsPaneDelete()
		}
		return m, nil
	case clearInputIntentMsg:
		m.closeModal()
		m.command.Clear()
		m.syncCurrentSQL()
		m.state.SetReady("", NotificationNone)
		m.state.SetPendingIntent(IntentClearCommandPane, "clear", "", NotificationNone)
		return m, m.notificationClearCmdIfSet()
	case historyIntentMsg:
		m.syncCurrentSQL()
		m.state.SetReady("", NotificationNone)
		if !layoutShowsCommand(m.state.Interaction.Layout) {
			m.state.SetLayout(LayoutSplit)
		}
		h := &historySearchModal{}
		m.pushModal(h)
		m.state.SetPendingIntent(IntentHistory, "history", "", NotificationNone)
		return m, m.notificationClearCmdIfSet()
	case historyNavBoundaryMsg:
		prevCreatedAt := m.state.Notification.CreatedAt
		m.state.SetPendingIntent(IntentNone, "history-nav", "Beginning of history.", NotificationInfo)
		return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
	case switchPaneIntentMsg:
		m.syncCurrentSQL()
		switchContext := buildPaneSwitchContext(m.state.Interaction.Layout, nextLayoutForModeIntent(m.state.Interaction.Layout, m.state.Interaction.ActivePane), m.state.Interaction.ActivePane, nextModeForIntent(m.state.Interaction.ActivePane), m.state.Interaction.LatestResult)
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
		m.state.SetReady("", NotificationNone)
		return m, tea.Batch(m.command.Init(), m.refreshAutocompleteSchemaCmd())
	case autocompleteSchemaLoadedMsg:
		if msg.Err == nil {
			m.schema = msg.Schema
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
	case notificationClearMsg:
		if m.state.Notification.CreatedAt.Equal(msg.createdAt) {
			m.state.Notification = Notification{}
		}
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

	if m.state.Interaction.ActivePane == PaneResults && !layoutShowsCommand(m.state.Interaction.Layout) {
		return m, nil
	}

	var cmd tea.Cmd
	m.command, cmd = m.command.Update(msg, m.state.Interaction)
	m.syncCurrentSQL()
	return m, cmd
}

func (m Model) handleKeyPressMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.state.App.Current != StateReady {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if modal := m.currentModal(); modal != nil {
		if msg.String() != "ctrl+c" {
			m.pendingQuit = false
		}
		result := modal.HandleKey(msg, ModalContext{
			Interaction: m.state.Interaction.snapshot(),
			Session:     m.session,
			Dialect:     m.adapterDialect(),
		})
		return m, m.applyModalResult(result)
	}

	if msg.String() != "ctrl+c" {
		m.pendingQuit = false
	}

	if m.handleResultsPaneNavigationKey(msg) {
		return m, nil
	}
	prevCreatedAt := m.state.Notification.CreatedAt
	if m.handleResultsPaneExportWizardKey(msg) {
		return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
	}
	if m.handleResultsPaneSelectionKey(msg) {
		return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
	}
	if m.handleResultsPaneComposeKey(msg) {
		return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
	}
	if m.handleResultsPanePagingKey(msg) {
		return m, m.newNotificationClearCmdIfChanged(prevCreatedAt)
	}
	if cmd := m.handleKey(msg); cmd != nil {
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		if m.state.App.Current == StateReady && m.command.Focused() && strings.TrimSpace(m.command.Value()) != "" {
			m.pendingQuit = false
			return m, func() tea.Msg { return clearInputIntentMsg{} }
		}
		if m.pendingQuit {
			return m, tea.Quit
		}
		m.pendingQuit = true
		m.state.SetPendingIntent(IntentNone, "quit", "Press ctrl+c again to exit.", NotificationInfo)
		return m, m.notificationClearCmdIfSet()
	case "q":
		if !m.command.Focused() {
			return m, tea.Quit
		}
	}

	if m.state.Interaction.ActivePane == PaneResults && !layoutShowsCommand(m.state.Interaction.Layout) {
		return m, nil
	}

	var cmd tea.Cmd
	m.command, cmd = m.command.Update(msg, m.state.Interaction)
	m.syncCurrentSQL()
	return m, cmd
}

func (m Model) handleSubmitIntent() (tea.Model, tea.Cmd) {
	if running := m.state.Interaction.Running; running != nil {
		m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("%s is still running. Press esc to cancel; timeout after %s.", runningLabel(running), formatExecutionTimeout(defaultInteractiveExecutionTimeout)), NotificationInfo)
		return m, m.notificationClearCmdIfSet()
	}

	m.syncCurrentSQL()
	submittedSQL := m.state.Interaction.CurrentSQL
	if strings.TrimSpace(submittedSQL) == "" {
		m.state.SetRunningStatementContext(nil)
		m.state.SetReady("", NotificationNone)
		m.state.SetPendingIntent(IntentSubmit, "submit", "Submit requested with empty input.", NotificationInfo)
		return m, m.notificationClearCmdIfSet()
	}

	parsedSlash, err := parseSlashCommand(submittedSQL)
	if err != nil {
		m.state.SetRunningStatementContext(nil)
		m.state.SetReady("", NotificationNone)
		m.state.SetPendingIntent(IntentNone, "submit", fmt.Sprintf("Slash command parse failed: %v", err), NotificationError)
		m.state.SetLatestResultContext(nil)
		m.state.SetPendingPaneSwitch(nil)
		return m, m.notificationClearCmdIfSet()
	}
	if parsedSlash != nil {
		if spec, ok := defaultSlashCommandRegistry.byName[parsedSlash.Name]; ok && spec.NeedsTarget && len(parsedSlash.Args) == 0 {
			return m.openTableSelectionForCommand(parsedSlash)
		}
		if parsedSlash.Name == "commands" {
			return m.openCommandWizard()
		}
		return m, m.startExecution(parsedSlash.DisplayName, fmt.Sprintf("Dispatching %s.", parsedSlash.DisplayName), NotificationInfo, executeSlashCommandCmd(slashCommandContext{
			Session: m.session,
			Dialect: m.adapterDialect(),
			Query:   m.state.Interaction.snapshot(),
		}, *parsedSlash))
	}

	if !isCompleteSQLStatement(submittedSQL) {
		m.state.SetRunningStatementContext(nil)
		m.state.SetPendingIntent(IntentNone, "submit", "SQL is incomplete. End the statement with ';' to run it.", NotificationInfo)
		return m, m.notificationClearCmdIfSet()
	}

	m.state.SetLastSubmittedSQL(submittedSQL)
	return m, m.startExecution("SQL", fmt.Sprintf("Executing %d characters of SQL.", len(submittedSQL)), NotificationInfo, executeStatementCmd(m.session.Adapter, submittedSQL))
}

func (m Model) handleCancelRunningIntent() (tea.Model, tea.Cmd) {
	if running := m.state.Interaction.Running; running != nil {
		if m.executionCancel != nil {
			m.executionCancel()
		}
		m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Cancelling %s...", runningLabel(running)), NotificationInfo)
	}
	return m, m.notificationClearCmdIfSet()
}

func (m Model) handleStatementExecuted(msg statementExecutedMsg) (tea.Model, tea.Cmd) {
	running := m.state.Interaction.Running
	m.clearExecution()
	historyErr := m.appendHistory(msg.Statement, msg.ResultSummary)
	m.state.SetRunningStatementContext(nil)
	m.state.Interaction.PendingIntent = IntentNone
	m.state.Interaction.LastAction = "submit"
	m.state.SetPendingPaneSwitch(nil)
	if msg.Err != nil {
		m.command.AppendReplEntry("> ", msg.Statement, "ERROR: "+strings.TrimSpace(msg.Err.Error()))
		m.command.Clear()
		if status, ok := executionInterruptedStatus(running, msg.Err); ok {
			m.state.SetReady(withHistoryWarning(status, historyErr), historyNotificationLevel(NotificationInfo, historyErr))
			m.state.SetLatestResultContext(nil)
			return m, m.notificationClearCmdIfSet()
		}
		m.state.SetReady(withHistoryWarning(formatOperationFailure("Execution failed.", msg.Err), historyErr), NotificationError)
		m.state.SetLatestResultContext(nil)
		return m, m.notificationClearCmdIfSet()
	}
	m.command.AppendReplEntry("> ", msg.Statement, "OK: "+formatReplStatementOutput(msg.Result, nil))
	m.command.Clear()
	m.state.SetReady(withHistoryWarning(describeStatementStatus(msg.Result), historyErr), historyNotificationLevel(NotificationSuccess, historyErr))
	m.state.SetLatestResultContext(buildLatestResultContext(msg.Statement, m.resultOriginPane(), msg.Result))
	return m, m.notificationClearCmdIfSet()
}

func (m Model) handleSlashCommandExecuted(msg slashCommandExecutedMsg) (tea.Model, tea.Cmd) {
	running := m.state.Interaction.Running
	m.clearExecution()
	shouldRecordSlashCommand := msg.Err != nil || !msg.Result.ShouldReplace
	var historyErr error
	if shouldRecordSlashCommand {
		historyErr = m.appendHistory(msg.Command.RawInput, msg.ResultSummary)
		m.state.SetLastSubmittedSQL(msg.Command.RawInput)
	}
	m.state.SetRunningStatementContext(nil)
	m.state.Interaction.PendingIntent = IntentNone
	m.state.Interaction.LastAction = "slash:" + msg.Command.DisplayName
	m.state.SetPendingPaneSwitch(nil)
	if msg.Err != nil {
		m.closeModal()
		m.command.AppendReplEntry("> ", msg.Command.RawInput, "ERROR: "+strings.TrimSpace(msg.Err.Error()))
		m.command.Clear()
		if status, ok := executionInterruptedStatus(running, msg.Err); ok {
			m.state.SetReady(withHistoryWarning(status, historyErr), historyNotificationLevel(NotificationInfo, historyErr))
			m.state.SetLatestResultContext(nil)
			return m, m.notificationClearCmdIfSet()
		}
		m.state.SetReady(withHistoryWarning(formatOperationFailure(msg.Command.DisplayName+" failed", msg.Err), historyErr), NotificationError)
		m.state.SetLatestResultContext(nil)
		return m, m.notificationClearCmdIfSet()
	}

	if msg.Result.Wizard != nil {
		m.pushModal(&slashWizardModal{wizard: *msg.Result.Wizard})
	}

	if msg.Result.ShouldReplace {
		m.command.SetEditorValue(msg.Result.ReplaceEditor)
		m.syncCurrentSQL()
		m.state.SetLatestResultContext(nil)
	} else {
		m.command.AppendReplEntry("> ", msg.Command.RawInput, "OK: "+formatReplSlashOutput(msg))
		m.command.Clear()
	}

	if msg.Result.Statement != nil {
		m.state.SetLatestResultContext(buildLatestResultContext(msg.Command.RawInput, m.resultOriginPane(), msg.Result.Statement))
	} else if !msg.Result.PreserveResult && !msg.Result.ShouldReplace {
		m.state.SetLatestResultContext(nil)
	}

	m.state.SetReady(withHistoryWarning(defaultStatus(msg.Result.Status, fmt.Sprintf("%s completed.", msg.Command.DisplayName)), historyErr), historyNotificationLevel(NotificationSuccess, historyErr))
	return m, m.notificationClearCmdIfSet()
}

func (m Model) View() tea.View {
	// Status bar occupies the last line
	statusBar := m.statusBarView()

	// Content area above the status bar
	contentHeight := m.height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.state.App.Current {
	case StateStartup:
		content = strings.Join([]string{
			tui.AppTheme.PanelTitle.Render("[ startup ]"),
			tui.AppTheme.PanelText.Render("Preparing command mode..."),
			tui.AppTheme.PanelMuted.Render(m.state.Notification.Text),
		}, "\n")
	case StateReconnect:
		lines := []string{
			tui.AppTheme.PanelTitle.Render("[ reconnect ]"),
			tui.AppTheme.PanelText.Render("Connection recovery in progress."),
			tui.AppTheme.PanelMuted.Render(m.state.Notification.Text),
		}
		if reconnect := m.state.App.Reconnect; reconnect != nil {
			if reconnect.Attempt > 0 {
				lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("Attempt %d", reconnect.Attempt)))
			}
			if reason := strings.TrimSpace(reconnect.Reason); reason != "" {
				lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("Reason: %s", reason)))
			}
			if lastError := strings.TrimSpace(reconnect.LastError); lastError != "" {
				lines = append(lines, tui.AppTheme.PanelText.Render(fmt.Sprintf("Last error: %s", lastError)))
			}
		}
		content = strings.Join(lines, "\n")
	case StateError:
		lines := []string{
			tui.AppTheme.ErrorNotice.Render("[ error ]"),
			tui.AppTheme.PanelText.Render(m.state.Notification.Text),
		}
		if appError := strings.TrimSpace(m.state.App.Error); appError != "" {
			lines = append(lines, tui.AppTheme.ErrorNotice.Render(appError))
		}
		content = strings.Join(lines, "\n")
	case StateReady:
		content = m.readyStateView(contentHeight)
	default:
		content = m.readyStateView(contentHeight)
	}

	v := tea.NewView(content + "\n" + statusBar)
	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	return v
}

// syncPaneSizes computes inner pane dimensions and propagates them to sub-models.
func (m *Model) syncPaneSizes() {
	w := m.width
	h := m.height
	statusBarHeight := 1
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
		resultsPaneOuterH := max(3, int(float64(contentHeight)*m.splitRatio))
		commandOuterH := max(3, contentHeight-resultsPaneOuterH)
		// If both minimum guards fired, their sum exceeds contentHeight.
		// Re-derive resultsPaneOuterH so the total stays exactly contentHeight.
		if resultsPaneOuterH+commandOuterH > contentHeight {
			resultsPaneOuterH = contentHeight - commandOuterH
		}
		m.resultsPane.SetSize(innerWidth, max(0, resultsPaneOuterH-2))
		m.command.SetSize(innerWidth, commandOuterH-2)
	case LayoutResultsOnly:
		m.resultsPane.SetSize(innerWidth, contentHeight-2)
		m.command.SetSize(innerWidth, contentHeight-2)
	default: // LayoutCommandOnly
		m.command.SetSize(innerWidth, contentHeight-2)
		m.resultsPane.SetSize(innerWidth, contentHeight-2)
	}
}

// renderBorderedPane wraps content in a rounded border with an optional title; active pane gets accent colour.
func (m Model) renderBorderedPane(content string, title string, active bool, outerWidth, innerHeight int) string {
	borderColor := tui.AppTheme.PaneBorderInactive.GetForeground()
	if active {
		borderColor = tui.AppTheme.PaneBorderActive.GetForeground()
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
		titleRendered := tui.AppTheme.PanelTitle.Render(title)
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

	var base string
	switch interaction.Layout {
	case LayoutSplit:
		resultsPaneOuterH := max(3, int(float64(totalHeight)*m.splitRatio))
		commandOuterH := max(3, totalHeight-resultsPaneOuterH)
		// If both minimum guards fired, their sum exceeds totalHeight.
		// Re-derive resultsPaneOuterH so the total stays exactly totalHeight.
		if resultsPaneOuterH+commandOuterH > totalHeight {
			resultsPaneOuterH = totalHeight - commandOuterH
		}
		resultsPaneContent := m.resultsPane.View(m.resultsPane.buildViewContext(interaction))
		commandContent := m.command.View(interaction)
		resultsPaneActive := interaction.ActivePane == PaneResults
		resultsPanePane := m.renderBorderedPane(resultsPaneContent, "[1] Results", resultsPaneActive, w, resultsPaneOuterH-2)
		commandPane := m.renderBorderedPane(commandContent, "[2] Commands", !resultsPaneActive, w, commandOuterH-2)
		base = resultsPanePane + "\n" + commandPane
	case LayoutResultsOnly:
		resultsPaneContent := m.resultsPane.View(m.resultsPane.buildViewContext(interaction))
		base = m.renderBorderedPane(resultsPaneContent, "[1] Results", true, w, totalHeight-2)
	default: // LayoutCommandOnly
		commandContent := m.command.View(interaction)
		base = m.renderBorderedPane(commandContent, "[2] Commands", true, w, totalHeight-2)
	}

	if modal := m.currentModal(); modal != nil {
		maxW := min(tui.ModalMaxWidth, w-4)
		if maxW >= tui.ModalMinWidth {
			innerWidth := maxW - 2
			title := modal.Title()
			counter := modal.CounterText(interaction)
			var rendered string
			if filterText := modal.FilterText(); filterText != "" {
				filterBox := tui.RenderTitledBox(modal.FilterLabel(), filterText, "", maxW, tui.ModalFilterRows)
				suggestionsBox := tui.RenderTitledBox(title, modal.Render(interaction, innerWidth), counter, maxW, tui.ModalSplitListRows)
				rendered = filterBox + "\n" + suggestionsBox
			} else {
				rendered = tui.RenderTitledBox(title, modal.Render(interaction, innerWidth), counter, maxW, tui.ModalFixedRows)
			}
			base = tui.OverlayCenter(base, rendered, w, totalHeight)
		}
	}

	return base
}

func (m Model) statusBarView() string {
	interaction := m.state.Interaction

	// Left: notification slot — running indicator takes priority over timed notification
	var notification string
	if running := interaction.Running; running != nil {
		notification = formatRunningIndicator(running)
	} else if n := m.state.Notification; n.Text != "" {
		switch n.Level {
		case NotificationSuccess:
			notification = tui.AppTheme.NotificationSuccess.Render(n.Text)
		case NotificationInfo:
			notification = tui.AppTheme.NotificationInfo.Render(n.Text)
		case NotificationError:
			notification = tui.AppTheme.NotificationError.Render(n.Text)
		default:
			notification = n.Text
		}
	}

	// Middle: keybind hints
	var hints string
	if m.state.App.Current == StateReady {
		if modal := m.currentModal(); modal != nil {
			hints = modal.FooterHints(interaction)
		} else if interaction.ActivePane == PaneResults && interaction.Layout != LayoutCommandOnly {
			hints = m.resultsPane.FooterHints(interaction)
		} else {
			hints = m.command.FooterHints(interaction)
		}
	} else {
		hints = "ctrl+c quit"
	}

	// Right: connection name (coloured if configured)
	var connectionName string
	if name := strings.TrimSpace(m.session.ConnectionName); name != "" {
		if color := strings.TrimSpace(m.session.ConnectionColor); color != "" {
			connectionName = lipgloss.NewStyle().Foreground(tui.ResolveColor(color)).Render(name)
		} else {
			connectionName = name
		}
	}

	// Compose: [notification |] hints <spacer> connection
	left := hints
	if notification != "" {
		left = notification + " | " + hints
	}

	var bar string
	if connectionName != "" && m.width > 0 {
		leftWidth := ansi.StringWidth(left)
		connWidth := ansi.StringWidth(connectionName)
		spacer := m.width - leftWidth - connWidth
		if spacer > 0 {
			bar = left + strings.Repeat(" ", spacer) + connectionName
		} else {
			bar = left
		}
	} else if connectionName != "" {
		bar = left + " | " + connectionName
	} else {
		bar = left
	}

	if m.width > 0 {
		bar = padOrTruncate(bar, m.width)
	}

	return tui.AppTheme.Footer.Render(bar)
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
		return nil
	case key.Matches(msg, keys.PrevSuggestion):
		return nil
	case key.Matches(msg, keys.Cancel):
		if m.state.Interaction.Running != nil {
			return func() tea.Msg { return cancelRunningIntentMsg{} }
		}
		// If the autocomplete dropdown is visible, dismiss it and preserve input.
		if m.command.AutocompleteVisible(m.state.Interaction) {
			m.command.DismissAutocomplete()
		}
		return nopCmd
	case key.Matches(msg, keys.History):
		if m.state.Interaction.ActivePane != PaneCommand {
			return nil
		}
		return func() tea.Msg { return historyIntentMsg{} }
	case key.Matches(msg, keys.Help):
		return func() tea.Msg { return toggleHelpIntentMsg{} }
	case key.Matches(msg, keys.SwitchMode):
		return func() tea.Msg { return switchPaneIntentMsg{} }
	case msg.String() == "ctrl+q":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneResults} }
	case msg.String() == "ctrl+w":
		return func() tea.Msg { return focusPaneIntentMsg{Pane: PaneCommand} }
	case msg.String() == "ctrl+3", msg.String() == "alt+3":
		return func() tea.Msg { return switchLayoutIntentMsg{Layout: LayoutCommandOnly} }
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
	if m.state.Interaction.ActivePane != PaneResults {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-page", "Results Pane has no rows to page.", NotificationInfo)
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
		page := tui.ResultsPanePageContextFor(m.state.Interaction.ResultsPanePage, len(result.Rows))
		pageMinRow := page.StartRow - 1 // inclusive lower bound (0-indexed)
		pageMaxRow := page.EndRow - 1   // inclusive upper bound (0-indexed)
		scrollAmount := max(1, m.resultsPane.height/2)
		if key == "ctrl+d" {
			m.resultsPane.selectedRow = min(m.resultsPane.selectedRow+scrollAmount, pageMaxRow)
		} else {
			m.resultsPane.selectedRow = max(m.resultsPane.selectedRow-scrollAmount, pageMinRow)
		}
		m.resultsPane.selectionActive = true
		// Do not call SetResultsPanePage — the page must not change on scroll.
		return true
	}

	// ctrl+p = prev page, ctrl+n = next page
	previous := m.state.Interaction.ResultsPanePage
	if key == "ctrl+p" {
		m.state.ChangeResultsPanePage(-1)
	} else {
		m.state.ChangeResultsPanePage(1)
	}

	page := tui.ResultsPanePageContextFor(m.state.Interaction.ResultsPanePage, len(latest.PreservedResult.Rows))
	if m.state.Interaction.ResultsPanePage == previous {
		if previous == 0 {
			m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Already at the first Results Pane page (%s).", tui.ResultsPaneFormatRowRange(page)), NotificationInfo)
			return true
		}
		m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Already at the last Results Pane page (%s).", tui.ResultsPaneFormatRowRange(page)), NotificationInfo)
		return true
	}

	m.state.SetPendingIntent(IntentNone, "results-pane-page", fmt.Sprintf("Showing Results Pane page %d/%d (%s).", page.Number, page.TotalPages, tui.ResultsPaneFormatRowRange(page)), NotificationSuccess)
	return true
}

func (m *Model) handleResultsPaneNavigationKey(msg tea.KeyPressMsg) bool {
	if m.state.Interaction.ActivePane != PaneResults {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}

	page, handled := m.resultsPane.Navigate(msg, m.state.Interaction)
	if !handled {
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	m.state.SetResultsPanePage(page)
	return true
}

func (m *Model) handleResultsPaneSelectionKey(msg tea.KeyPressMsg) bool {
	if msg.String() != "space" && msg.String() != " " {
		return false
	}
	if m.state.Interaction.ActivePane != PaneResults {
		m.resultsPane.pendingAction = resultsPanePendingActionNone
		return false
	}
	m.resultsPane.pendingAction = resultsPanePendingActionNone

	row, newMarked, selected, handled := m.resultsPane.ToggleSelectedRow(m.state.Interaction)
	if !handled {
		m.state.SetPendingIntent(IntentNone, "results-pane-select", "Results Pane has no rows to select.", NotificationInfo)
		return true
	}

	m.state.SetMarkedRows(newMarked)
	if selected {
		m.state.SetPendingIntent(IntentNone, "results-pane-select", fmt.Sprintf("Selected row %d (%d total).", row+1, len(m.state.Interaction.MarkedRows)), NotificationSuccess)
		return true
	}

	m.state.SetPendingIntent(IntentNone, "results-pane-select", fmt.Sprintf("Unselected row %d (%d total).", row+1, len(m.state.Interaction.MarkedRows)), NotificationSuccess)
	return true
}

func (m *Model) handleResultsPaneComposeKey(msg tea.KeyPressMsg) bool {
	if m.state.Interaction.ActivePane != PaneResults {
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
			m.state.SetPendingIntent(IntentNone, "results-pane-compose", "Press y again to load INSERT for the selected row into command mode.", NotificationInfo)
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
			m.state.SetPendingIntent(IntentNone, "results-pane-compose", "Press d again to load DELETE for the selected row into command mode.", NotificationInfo)
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
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", "Results Pane has no rows to compose.", NotificationInfo)
		return true
	}

	result, err := composeResultsPaneInsertSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", fmt.Sprintf("Could not compose INSERT: %v", err), NotificationError)
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeModal()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, PaneResults))
	m.state.SetActivePane(PaneCommand)
	m.state.SetPendingPaneSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "results-pane-compose", resultsPaneComposeStatus(result), NotificationSuccess)
	m.syncPaneSizes()
	return true
}

func (m *Model) composeResultsPaneUpdate() bool {
	if m.state.Interaction.LatestResult == nil || m.state.Interaction.LatestResult.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", "Results Pane has no rows to compose.", NotificationInfo)
		return true
	}

	result, err := composeResultsPaneUpdateSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", fmt.Sprintf("Could not compose UPDATE: %v", err), NotificationError)
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeModal()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, PaneResults))
	m.state.SetActivePane(PaneCommand)
	m.state.SetPendingPaneSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "results-pane-compose", resultsPaneComposeStatus(result), NotificationSuccess)
	m.syncPaneSizes()
	return true
}

func (m *Model) composeResultsPaneDelete() bool {
	if m.state.Interaction.LatestResult == nil || m.state.Interaction.LatestResult.PreservedResult == nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", "Results Pane has no rows to compose.", NotificationInfo)
		return true
	}

	result, err := composeResultsPaneDeleteSQL(m.adapterDialect(), m.state.Interaction.LatestResult, m.resultsPane.selectedRow)
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "results-pane-compose", fmt.Sprintf("Could not compose DELETE: %v", err), NotificationError)
		return true
	}

	m.command.SetEditorValue(result.SQL)
	m.syncCurrentSQL()
	m.closeModal()
	m.command.Focus()
	m.state.SetLayout(nextLayoutForModeIntent(m.state.Interaction.Layout, PaneResults))
	m.state.SetActivePane(PaneCommand)
	m.state.SetPendingPaneSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "results-pane-compose", resultsPaneComposeStatus(result), NotificationSuccess)
	m.syncPaneSizes()
	return true
}

func (m *Model) syncCurrentSQL() {
	m.state.SetCurrentSQL(m.command.Value())
}

func (m *Model) syncHistorySnapshot() {
	if m.history == nil {
		m.state.SetHistory(nil)
		return
	}

	entries := m.history.Entries()
	contexts := make([]HistoryEntryContext, 0, len(entries))
	for _, entry := range entries {
		contexts = append(contexts, HistoryEntryContext{
			Statement:      entry.Statement,
			ConnectionName: entry.ConnectionName,
			ExecutedAt:     entry.ExecutedAt,
		})
	}
	m.state.SetHistory(contexts)
}

func (m *Model) appendHistory(statement, resultSummary string) error {
	if m.history == nil || strings.TrimSpace(statement) == "" {
		return nil
	}

	err := m.history.Append(apphistory.Entry{
		Statement:      statement,
		ConnectionName: m.session.ConnectionName,
		ExecutedAt:     time.Now().UTC(),
		ResultSummary:  resultSummary,
	})
	m.syncHistorySnapshot()
	return err
}

func withHistoryWarning(status string, err error) string {
	if err == nil {
		return status
	}

	return fmt.Sprintf("%s History was not persisted: %v", status, err)
}

func historyNotificationLevel(base NotificationLevel, historyErr error) NotificationLevel {
	if historyErr != nil {
		return NotificationError
	}
	return base
}

func latestHistoryEntry(entries []HistoryEntryContext) *HistoryEntryContext {
	if len(entries) == 0 {
		return nil
	}

	entry := entries[len(entries)-1]
	return &entry
}

func (m Model) dialectName() string {
	if m.session.Adapter != nil && m.session.Adapter.Dialect() != nil {
		return m.session.Adapter.Dialect().Name()
	}

	return strings.TrimSpace(m.session.DatabaseType)
}

func (m Model) refreshAutocompleteSchemaCmd() tea.Cmd {
	return loadAutocompleteSchemaCmd(m.session.Adapter, m.loader)
}

func (m *Model) syncAutocompleteSchemaSnapshot() {
	m.state.SetAutocompleteSchema(m.schema)
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

func (m *Model) startExecution(label, status string, level NotificationLevel, execute func(context.Context, time.Time) tea.Cmd) tea.Cmd {
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
	m.state.SetReady("", NotificationNone)
	m.state.SetPendingIntent(IntentSubmit, "submit", executionStatus(status, defaultInteractiveExecutionTimeout), level)
	return tea.Batch(execute(ctx, startedAt), runningTickCmd(startedAt), m.notificationClearCmdIfSet())
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
		entry := AutocompleteTableContext{Namespace: table.Namespace, Name: table.Name, ColumnTypes: make(map[string]string)}
		columns, err := adapter.Columns(ctx, db.TableRef{Catalog: table.Catalog, Namespace: table.Namespace, Name: table.Name})
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
	cmd := m.pushWizardForCommand(parsed.Name, fmt.Sprintf("Choose a table for %s and press enter.", parsed.DisplayName), NotificationInfo)
	return *m, cmd
}

func (m *Model) pushWizardForCommand(commandName, status string, level NotificationLevel) tea.Cmd {
	commandCtx := slashCommandContext{
		Session: m.session,
		Dialect: m.adapterDialect(),
		Query:   m.state.Interaction.snapshot(),
	}

	commands := buildSlashWizardCommands()
	selectedIdx := 0
	for i, cmd := range commands {
		if cmd.Name == commandName {
			selectedIdx = i
			break
		}
	}

	targets, err := buildSlashWizardTargets(context.Background(), commandCtx)
	if err != nil {
		m.state.SetReady(fmt.Sprintf("/%s failed: %v", commandName, err), NotificationError)
		return m.notificationClearCmdIfSet()
	}
	if len(targets) == 0 {
		m.state.SetReady(fmt.Sprintf("/%s: no tables available.", commandName), NotificationError)
		return m.notificationClearCmdIfSet()
	}

	m.pushModal(&slashWizardModal{wizard: SlashCommandWizardContext{
		Step:             SlashCommandWizardStepTarget,
		Commands:         commands,
		SelectedCommand:  selectedIdx,
		Targets:          targets,
		SelectedTarget:   0,
		DirectInvocation: true,
	}})
	m.state.SetReady(defaultStatus(status, fmt.Sprintf("Choose a table for %s and press enter.", commands[selectedIdx].DisplayName)), level)
	return m.notificationClearCmdIfSet()
}

func (m *Model) executeExportWizard(format export.Format, path string) tea.Cmd {
	latest := m.state.Interaction.LatestResult
	if latest == nil || latest.PreservedResult == nil || len(latest.PreservedResult.Rows) == 0 {
		m.state.SetPendingIntent(IntentNone, "export", "Results Pane has no rows to export.", NotificationInfo)
		return m.notificationClearCmdIfSet()
	}

	rowIndices := selectedRowsForExport(latest, m.state.Interaction.MarkedRows)
	usedSelectedRows := len(m.state.Interaction.MarkedRows) > 0
	scope := "current result rows"
	if usedSelectedRows {
		scope = "selected rows"
	}

	if strings.TrimSpace(path) == "" {
		data, rowCount, err := export.Marshal(latest.PreservedResult, rowIndices, format)
		if err != nil {
			m.state.SetPendingIntent(IntentNone, "export", fmt.Sprintf("Could not export rows: %v", err), NotificationError)
			return m.notificationClearCmdIfSet()
		}
		if err := clipboard.WriteAll(string(data)); err != nil {
			m.state.SetPendingIntent(IntentNone, "export", fmt.Sprintf("Could not copy to clipboard: %v", err), NotificationError)
			return m.notificationClearCmdIfSet()
		}
		m.state.SetPendingIntent(IntentNone, "export",
			fmt.Sprintf("Copied %d row(s) as %s from %s to clipboard.", rowCount, strings.ToLower(string(format)), scope),
			NotificationSuccess)
		return m.notificationClearCmdIfSet()
	}

	written, err := export.Export(export.ExportOptions{
		CWD:        m.session.WorkingDir,
		Filename:   path,
		Result:     latest.PreservedResult,
		RowIndices: rowIndices,
		Format:     format,
	})
	if err != nil {
		m.state.SetPendingIntent(IntentNone, "export", fmt.Sprintf("Could not export rows: %v", err), NotificationError)
		return m.notificationClearCmdIfSet()
	}

	displayPath := written.Path
	if rel, err := filepath.Rel(m.session.WorkingDir, written.Path); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		displayPath = rel
	}
	m.state.SetPendingIntent(IntentNone, "export",
		fmt.Sprintf("Exported %d row(s) as %s from %s to %s.", written.Rows, strings.ToLower(string(format)), scope, displayPath),
		NotificationSuccess)
	return m.notificationClearCmdIfSet()
}

func (m *Model) openCommandWizard() (Model, tea.Cmd) {
	commands := buildSlashWizardCommands()
	if len(commands) == 0 {
		m.state.SetReady("/commands: no slash commands available.", NotificationError)
		return *m, m.notificationClearCmdIfSet()
	}
	m.pushModal(&slashWizardModal{wizard: SlashCommandWizardContext{
		Step:     SlashCommandWizardStepCommand,
		Commands: commands,
	}})
	m.state.SetReady("Choose a slash command and press enter.", NotificationInfo)
	return *m, m.notificationClearCmdIfSet()
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
	if m.session.Adapter == nil {
		return nil
	}

	return m.session.Adapter.Dialect()
}

func (m Model) resultOriginPane() Pane {
	if m.state.Interaction.ActiveModal == ModalHistorySearch {
		return PaneCommand
	}
	if layoutShowsCommand(m.state.Interaction.Layout) {
		return PaneCommand
	}
	return m.state.Interaction.ActivePane
}

func buildLatestResultContext(query string, originMode Pane, result *db.StatementResult) *LatestResultContext {
	if result == nil {
		return nil
	}

	context := &LatestResultContext{
		Statement:     query,
		OriginPane:    originMode,
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

func buildPaneSwitchContext(fromLayout, toLayout AppLayout, fromMode, toMode Pane, latest *LatestResultContext) *PaneSwitchContext {
	return &PaneSwitchContext{
		FromLayout:    fromLayout,
		ToLayout:      toLayout,
		FromPane:      fromMode,
		ToPane:        toMode,
		ResultContext: cloneLatestResultContext(latest),
	}
}

func nextModeForIntent(current Pane) Pane {
	switch current {
	case PaneResults:
		return PaneCommand
	default:
		return PaneResults
	}
}

func nextLayoutForModeIntent(currentLayout AppLayout, currentMode Pane) AppLayout {
	switch currentLayout {
	case LayoutSplit:
		return LayoutSplit
	case LayoutResultsOnly:
		return LayoutCommandOnly
	default:
		if currentMode == PaneResults {
			return LayoutCommandOnly
		}
		return LayoutResultsOnly
	}
}

func describeModeSwitchStatus(context *PaneSwitchContext) string {
	if context == nil {
		return "Mode switch requested."
	}

	if context.ToPane == PaneCommand {
		if context.ToLayout == LayoutSplit {
			return "Focused the command line in split layout."
		}
		return "Returned to command line."
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		if context.ToLayout == LayoutSplit {
			return "Focused the Results Pane in split layout. Run a query that returns rows to populate it."
		}
		return "Results Pane is available after running a query that returns tabular results."
	}

	result := context.ResultContext.PreservedResult
	if context.ToLayout == LayoutSplit {
		return fmt.Sprintf("Focused the Results Pane in split layout for %d row(s) across %d column(s).", len(result.Rows), len(result.Columns))
	}
	return fmt.Sprintf("Opened Results Pane for %d row(s) across %d column(s).", len(result.Rows), len(result.Columns))
}

func (m *Model) applyModeSwitch(context *PaneSwitchContext) {
	m.state.SetReady("", NotificationNone)
	m.state.SetPendingPaneSwitch(context)

	if context == nil {
		m.state.SetPendingIntent(IntentSwitchPane, "switch-mode", "", NotificationNone)
		return
	}

	if context.ToPane == PaneCommand {
		m.closeModal()
		m.command.Focus()
		m.state.SetLayout(context.ToLayout)
		m.state.SetActivePane(PaneCommand)
		m.state.SetPendingPaneSwitch(nil)
		m.state.SetPendingIntent(IntentNone, "switch-mode", "", NotificationNone)
		return
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		if context.ToLayout == LayoutSplit {
			m.closeModal()
			m.command.Blur()
			m.state.SetLayout(context.ToLayout)
			m.state.SetActivePane(context.ToPane)
			m.state.SetPendingPaneSwitch(nil)
			m.state.SetPendingIntent(IntentNone, "switch-mode", "", NotificationNone)
			return
		}
		m.state.SetPendingIntent(IntentSwitchPane, "switch-mode", "", NotificationNone)
		return
	}
	m.closeModal()
	m.command.Blur()
	m.state.SetLayout(context.ToLayout)
	m.state.SetActivePane(context.ToPane)
	m.state.SetPendingPaneSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "switch-mode", "", NotificationNone)
}

func (m *Model) applyLayoutSwitch(layout AppLayout) {
	current := m.state.Interaction.Layout
	if layout == "" {
		layout = LayoutCommandOnly
	}
	m.state.SetPendingPaneSwitch(nil)

	if layout == current {
		m.state.SetPendingIntent(IntentNone, "layout", fmt.Sprintf("Layout already set to %s.", layoutLabel(layout)), NotificationInfo)
		return
	}

	m.state.SetReady("", NotificationNone)
	m.state.SetLayout(layout)

	switch layout {
	case LayoutResultsOnly:
		if m.state.Interaction.ActiveModal == ModalHistorySearch {
			m.closeModal()
		}
		m.command.Blur()
		m.state.SetActivePane(PaneResults)
	case LayoutSplit:
		if m.state.Interaction.ActivePane == PaneResults {
			m.command.Blur()
		} else {
			m.command.Focus()
		}
	case LayoutCommandOnly:
		if m.state.Interaction.ActivePane == PaneResults {
			m.state.SetActivePane(PaneCommand)
		}
		m.command.Focus()
	default:
		m.command.Focus()
	}
}

func (m *Model) handleFocusPane(pane Pane) {
	m.state.SetReady("", NotificationNone)
	m.state.SetPendingPaneSwitch(nil)
	switch pane {
	case PaneResults:
		m.closeModal()
		switch m.state.Interaction.Layout {
		case LayoutCommandOnly:
			m.command.Blur()
			m.state.SetLayout(LayoutSplit)
			m.state.SetActivePane(PaneResults)
		case LayoutResultsOnly:
			m.state.SetActivePane(PaneResults)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Results pane is already focused.", NotificationInfo)
		default: // LayoutSplit
			m.command.Blur()
			m.state.SetActivePane(PaneResults)
		}
	case PaneCommand:
		m.closeModal()
		switch m.state.Interaction.Layout {
		case LayoutResultsOnly:
			m.command.Focus()
			m.state.SetLayout(LayoutSplit)
			m.state.SetActivePane(PaneCommand)
		case LayoutCommandOnly:
			m.command.Focus()
			m.state.SetActivePane(PaneCommand)
			m.state.SetPendingIntent(IntentNone, "focus-pane", "Command pane is already focused.", NotificationInfo)
		default: // LayoutSplit
			m.command.Focus()
			m.state.SetActivePane(PaneCommand)
		}
	}
}

func (m *Model) handleToggleZoom() {
	m.state.SetReady("", NotificationNone)
	switch m.state.Interaction.Layout {
	case LayoutSplit:
		if m.state.Interaction.ActivePane == PaneResults {
			m.command.Blur()
			m.state.SetLayout(LayoutResultsOnly)
			m.state.SetActivePane(PaneResults)
		} else {
			m.command.Focus()
			m.state.SetLayout(LayoutCommandOnly)
			m.state.SetActivePane(PaneCommand)
		}
	case LayoutCommandOnly:
		m.command.Blur()
		m.state.SetLayout(LayoutSplit)
		m.state.SetActivePane(PaneCommand)
		m.command.Focus()
	case LayoutResultsOnly:
		m.state.SetLayout(LayoutSplit)
		m.state.SetActivePane(PaneResults)
		m.command.Blur()
	}
}

func layoutShowsCommand(layout AppLayout) bool {
	return layout != LayoutResultsOnly
}

func layoutLabel(layout AppLayout) string {
	switch layout {
	case LayoutSplit:
		return "split"
	case LayoutResultsOnly:
		return "results pane only"
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
