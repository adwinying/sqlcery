package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

type Model struct {
	session Session
	adapter *db.SQLAdapter
	history *apphistory.Session
	command commandModeModel
	viewer  recordViewerModeModel
	state   SharedAppState
	cache   *autocompleteSchemaCache
	loader  autocompleteSchemaLoader
}

type autocompleteSchemaLoader func(context.Context, *db.SQLAdapter) (*AutocompleteSchemaContext, error)

type modelDependencies struct {
	cache   *autocompleteSchemaCache
	loader  autocompleteSchemaLoader
	history *apphistory.Session
}

type submitIntentMsg struct{}

type slashWizardMoveIntentMsg struct {
	Delta int
}

type slashWizardBackIntentMsg struct{}

type slashWizardCloseIntentMsg struct{}

type historyIntentMsg struct{}

type switchModeIntentMsg struct{}

type clearInputIntentMsg struct{}

type statementExecutedMsg struct {
	Query         string
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

func NewModel(session Session, adapter *db.SQLAdapter) Model {
	return newModelWithDependencies(session, adapter, modelDependencies{})
}

func newModelWithDependencies(session Session, adapter *db.SQLAdapter, deps modelDependencies) Model {
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
		sessionHistory = apphistory.NewSession()
	}

	model := Model{
		session: session,
		adapter: adapter,
		history: sessionHistory,
		command: newCommandModeModel(),
		viewer:  newRecordViewerModeModel(),
		state:   NewSharedAppState(),
		cache:   cache,
		loader:  loader,
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
	case tea.KeyMsg:
		if m.state.App.Current != StateReady {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}

		if m.state.Query.ActiveMode == ModeHistorySearch {
			return m, m.handleHistorySearchKey(msg)
		}

		if cmd := m.handleKey(msg); cmd != nil {
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if !m.command.Focused() {
				return m, tea.Quit
			}
		}
	case submitIntentMsg:
		if wizard := m.state.Query.SlashWizard; wizard != nil {
			return m.submitSlashWizard(wizard)
		}

		m.syncCurrentSQL()
		submittedSQL := m.state.Query.CurrentSQL
		m.state.SetLastSubmittedSQL(submittedSQL)
		if strings.TrimSpace(submittedSQL) == "" {
			m.state.SetReady("")
			m.state.SetPendingIntent(IntentSubmit, "submit", "Submit requested with empty input.")
			return m, nil
		}

		parsedSlash, err := parseSlashCommand(submittedSQL)
		if err != nil {
			m.state.SetReady("")
			m.state.SetPendingIntent(IntentNone, "submit", fmt.Sprintf("Slash command parse failed: %v", err))
			m.state.SetLatestResultContext(nil)
			m.state.SetPendingModeSwitch(nil)
			return m, nil
		}
		if parsedSlash != nil {
			m.state.SetReady("")
			m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Dispatching %s.", parsedSlash.DisplayName))
			return m, executeSlashCommandCmd(slashCommandContext{
				Session: m.session,
				Adapter: m.adapter,
				Dialect: m.adapterDialect(),
				Query:   m.state.Query.snapshot(),
			}, *parsedSlash)
		}

		m.state.SetReady("")
		m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Executing %d characters of SQL.", len(submittedSQL)))
		return m, executeStatementCmd(m.adapter, submittedSQL)
	case statementExecutedMsg:
		historyErr := m.appendSessionHistory(msg.Query, msg.ResultSummary)
		m.state.Query.PendingIntent = IntentNone
		m.state.Query.LastAction = "submit"
		m.state.SetPendingModeSwitch(nil)
		if msg.Err != nil {
			m.state.SetReady(withHistoryWarning(fmt.Sprintf("Execution failed: %v", msg.Err), historyErr))
			m.state.SetLatestResultContext(nil)
			return m, nil
		}

		m.state.SetReady(withHistoryWarning(describeStatementStatus(msg.Result), historyErr))
		m.state.SetLatestResultContext(buildLatestResultContext(msg.Query, m.state.Query.ActiveMode, msg.Result))
		return m, nil
	case slashCommandExecutedMsg:
		historyErr := m.appendSessionHistory(msg.Command.RawInput, msg.ResultSummary)
		m.state.Query.PendingIntent = IntentNone
		m.state.Query.LastAction = "slash:" + msg.Command.DisplayName
		m.state.SetPendingModeSwitch(nil)
		if msg.Err != nil {
			m.state.SetSlashWizardContext(nil)
			m.state.SetReady(withHistoryWarning(fmt.Sprintf("%s failed: %v", msg.Command.DisplayName, msg.Err), historyErr))
			m.state.SetLatestResultContext(nil)
			return m, nil
		}

		m.state.SetSlashWizardContext(msg.Result.Wizard)

		if msg.Result.ShouldReplace {
			m.command.editor.SetValue(msg.Result.ReplaceEditor)
			m.command.editor.CursorEnd()
			m.command.syncScroll()
			m.command.selectedSuggestion = 0
			m.syncCurrentSQL()
			m.state.SetLatestResultContext(nil)
		}

		if msg.Result.Statement != nil {
			m.state.SetLatestResultContext(buildLatestResultContext(msg.Command.RawInput, m.state.Query.ActiveMode, msg.Result.Statement))
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
	case clearInputIntentMsg:
		if m.state.Query.SlashWizard != nil {
			m.state.SetSlashWizardContext(nil)
		}
		m.command.Clear()
		m.syncCurrentSQL()
		m.state.SetReady("")
		m.state.SetPendingIntent(IntentClearInput, "clear", "Cleared current input.")
		return m, nil
	case historyIntentMsg:
		m.syncCurrentSQL()
		m.state.SetReady("")
		m.openHistorySearch()
		return m, nil
	case switchModeIntentMsg:
		m.syncCurrentSQL()
		switchContext := buildModeSwitchContext(m.state.Query.ActiveMode, nextModeForIntent(m.state.Query.ActiveMode), m.state.Query.LatestResult)
		m.applyModeSwitch(switchContext)
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
		message := ""
		if msg.Err != nil {
			message = msg.Err.Error()
		}
		m.state.SetError(message, msg.Status)
		return m, nil
	case tea.WindowSizeMsg:
		m.command.SetSize(msg.Width, msg.Height)
		m.viewer.SetSize(msg.Width, msg.Height)
	}

	if m.state.Query.ActiveMode == ModeRecordViewer {
		return m, nil
	}

	var cmd tea.Cmd
	m.command, cmd = m.command.Update(msg, m.state.Query)
	m.syncCurrentSQL()
	return m, cmd
}

func (m Model) View() string {
	if m.state.App.Current == StateReady && m.state.Query.ActiveMode == ModeRecordViewer {
		return strings.Join([]string{m.stateView(), "", m.footerView()}, "\n")
	}

	lines := []string{"SQLcery", ""}

	if name := strings.TrimSpace(m.session.ConnectionName); name != "" {
		lines = append(lines, fmt.Sprintf("Connection: %s", name))
	}

	if dialect := m.dialectName(); dialect != "" {
		lines = append(lines, fmt.Sprintf("Dialect: %s", dialect))
	}

	lines = append(lines, fmt.Sprintf("App state: %s", m.state.App.Current))
	lines = append(lines, fmt.Sprintf("Mode: %s", m.state.Query.ActiveMode))
	if pending := strings.TrimSpace(string(m.state.Query.PendingIntent)); pending != "" {
		lines = append(lines, fmt.Sprintf("Pending: %s", pending))
	}
	if action := strings.TrimSpace(m.state.Query.LastAction); action != "" {
		lines = append(lines, fmt.Sprintf("Last action: %s", action))
	}
	if currentSQL := strings.TrimSpace(m.state.Query.CurrentSQL); currentSQL != "" {
		lines = append(lines, fmt.Sprintf("Current SQL: %d characters", len(m.state.Query.CurrentSQL)))
	}
	if submittedSQL := strings.TrimSpace(m.state.Query.LastSubmittedSQL); submittedSQL != "" {
		lines = append(lines, fmt.Sprintf("Last submitted SQL: %d characters", len(m.state.Query.LastSubmittedSQL)))
	}
	if status := strings.TrimSpace(m.state.Status); status != "" {
		lines = append(lines, fmt.Sprintf("Status: %s", status))
	}
	if appError := strings.TrimSpace(m.state.App.Error); appError != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", appError))
	}
	if reconnect := m.state.App.Reconnect; reconnect != nil {
		if reconnect.Attempt > 0 {
			lines = append(lines, fmt.Sprintf("Reconnect attempt: %d", reconnect.Attempt))
		}
		if reason := strings.TrimSpace(reconnect.Reason); reason != "" {
			lines = append(lines, fmt.Sprintf("Reconnect reason: %s", reason))
		}
		if lastError := strings.TrimSpace(reconnect.LastError); lastError != "" {
			lines = append(lines, fmt.Sprintf("Reconnect error: %s", lastError))
		}
	}
	if m.state.Query.LatestResult != nil {
		lines = append(lines, "Latest result: available")
	}
	if m.state.Query.PendingModeSwitch != nil {
		lines = append(lines, "Pending mode switch: available")
	}
	if count := len(m.state.Query.SessionHistory); count > 0 {
		lines = append(lines, fmt.Sprintf("Session history: %d entries", count))
	}
	if m.state.Query.SelectedHistoryEntry != nil {
		lines = append(lines, "Selected history entry: available")
	}
	if m.state.Query.HistorySearch != nil {
		lines = append(lines, "History search: active")
	}

	lines = append(lines, "", m.stateView(), "", m.footerView())

	return strings.Join(lines, "\n")
}

func (m Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case key.Matches(msg, keys.Submit):
		return func() tea.Msg { return submitIntentMsg{} }
	case key.Matches(msg, keys.NextSuggestion):
		if m.state.Query.SlashWizard != nil {
			return func() tea.Msg { return slashWizardMoveIntentMsg{Delta: 1} }
		}
		return nil
	case key.Matches(msg, keys.PrevSuggestion):
		if m.state.Query.SlashWizard != nil {
			return func() tea.Msg { return slashWizardMoveIntentMsg{Delta: -1} }
		}
		return nil
	case key.Matches(msg, keys.Cancel):
		if wizard := m.state.Query.SlashWizard; wizard != nil {
			if wizard.Step == SlashCommandWizardStepTarget {
				return func() tea.Msg { return slashWizardBackIntentMsg{} }
			}
			return func() tea.Msg { return slashWizardCloseIntentMsg{} }
		}
		return func() tea.Msg { return clearInputIntentMsg{} }
	case key.Matches(msg, keys.History):
		return func() tea.Msg { return historyIntentMsg{} }
	case key.Matches(msg, keys.SwitchMode):
		return func() tea.Msg { return switchModeIntentMsg{} }
	default:
		return nil
	}
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
			SQL:            entry.Command,
			ConnectionName: entry.ConnectionName,
			ExecutedAt:     entry.ExecutedAt,
		})
	}
	m.state.SetSessionHistory(contexts)
}

func (m *Model) appendSessionHistory(command, resultSummary string) error {
	if m.history == nil || strings.TrimSpace(command) == "" {
		return nil
	}

	err := m.history.Append(apphistory.Entry{
		Command:        command,
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

func (m Model) stateView() string {
	switch m.state.App.Current {
	case StateStartup:
		return strings.Join([]string{
			"[ startup ]",
			"Preparing command mode...",
			m.state.Status,
		}, "\n")
	case StateReconnect:
		lines := []string{
			"[ reconnect ]",
			"Connection recovery placeholder is active.",
			m.state.Status,
		}

		if reconnect := m.state.App.Reconnect; reconnect != nil {
			if reconnect.Attempt > 0 {
				lines = append(lines, fmt.Sprintf("Attempt %d", reconnect.Attempt))
			}
			if reason := strings.TrimSpace(reconnect.Reason); reason != "" {
				lines = append(lines, fmt.Sprintf("Reason: %s", reason))
			}
			if lastError := strings.TrimSpace(reconnect.LastError); lastError != "" {
				lines = append(lines, fmt.Sprintf("Last error: %s", lastError))
			}
		}

		return strings.Join(lines, "\n")
	case StateError:
		lines := []string{
			"[ error ]",
			m.state.Status,
		}

		if appError := strings.TrimSpace(m.state.App.Error); appError != "" {
			lines = append(lines, appError)
		}

		return strings.Join(lines, "\n")
	case StateReady:
		if m.state.Query.ActiveMode == ModeRecordViewer {
			return m.viewer.View(m.state.Query)
		}
		return m.command.View(m.state.Query)
	default:
		return m.command.View(m.state.Query)
	}
}

func (m Model) footerView() string {
	if m.state.App.Current == StateReady {
		if m.state.Query.ActiveMode == ModeRecordViewer {
			return m.viewer.Footer(m.session.ConnectionName, m.dialectName(), m.state.Query)
		}
		return m.command.Footer(m.session.ConnectionName, m.dialectName(), m.state.Query)
	}

	parts := []string{fmt.Sprintf("App state %s", m.state.App.Current)}
	if label := strings.TrimSpace(m.session.ConnectionName); label != "" {
		parts = append(parts, fmt.Sprintf("connection %s", label))
	}
	parts = append(parts, "ctrl+c quit")
	return strings.Join(parts, " | ")
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
		entry := AutocompleteTableContext{Schema: table.Schema, Name: table.Name}
		columns, err := adapter.Columns(ctx, db.TableRef{Catalog: table.Catalog, Schema: table.Schema, Name: table.Name})
		if err != nil {
			if !errors.Is(err, db.ErrMetadataUnsupported) {
				return nil, err
			}
		} else {
			for _, column := range columns {
				entry.Columns = append(entry.Columns, column.Name)
			}
		}
		schema.Tables = append(schema.Tables, entry)
	}

	return schema, nil
}

func executeSlashCommandCmd(commandContext slashCommandContext, parsed slashCommand) tea.Cmd {
	return func() tea.Msg {
		result, err := dispatchSlashCommand(context.Background(), commandContext, parsed)
		return slashCommandExecutedMsg{Command: parsed, Result: result, ResultSummary: summarizeSlashCommandResult(parsed, result, err), Err: err}
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
				Query:   m.state.Query.snapshot(),
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
			m.state.SetReady(fmt.Sprintf("Choose a table for %s and press ctrl+g.", selectedCommand.DisplayName))
			return *m, nil
		}

		selectedTarget, ok := slashWizardTargetByIndex(wizard)
		if !ok {
			m.state.SetReady(fmt.Sprintf("/commands: choose a table for %s.", selectedCommand.DisplayName))
			return *m, nil
		}

		parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
		m.state.SetReady("")
		m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName))
		return *m, executeSlashCommandCmd(slashCommandContext{
			Session: m.session,
			Adapter: m.adapter,
			Dialect: m.adapterDialect(),
			Query:   m.state.Query.snapshot(),
		}, parsed)
	}

	parsed := buildSlashWizardCommand(selectedCommand, nil)
	m.state.SetReady("")
	m.state.SetPendingIntent(IntentSubmit, "submit", fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName))
	return *m, executeSlashCommandCmd(slashCommandContext{
		Session: m.session,
		Adapter: m.adapter,
		Dialect: m.adapterDialect(),
		Query:   m.state.Query.snapshot(),
	}, parsed)
}

func (m *Model) moveSlashWizardSelection(delta int) {
	wizard := cloneSlashCommandWizardContext(m.state.Query.SlashWizard)
	if wizard == nil || delta == 0 {
		return
	}

	switch wizard.Step {
	case SlashCommandWizardStepTarget:
		if len(wizard.Targets) == 0 {
			return
		}
		wizard.SelectedTarget = wrapSelection(wizard.SelectedTarget+delta, len(wizard.Targets))
		selectedTarget, _ := slashWizardTargetByIndex(wizard)
		m.state.SetSlashWizardContext(wizard)
		m.state.SetReady(fmt.Sprintf("Selected table %s.", selectedTarget.Display))
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

func (m *Model) stepBackSlashWizard() {
	wizard := cloneSlashCommandWizardContext(m.state.Query.SlashWizard)
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

func executeStatementCmd(adapter *db.SQLAdapter, query string) tea.Cmd {
	return func() tea.Msg {
		if adapter == nil {
			return statementExecutedMsg{Query: query, ResultSummary: "error: adapter is required", Err: fmt.Errorf("adapter is required")}
		}

		result, err := adapter.ExecuteStatementContext(context.Background(), query, db.ResultOptions{})
		return statementExecutedMsg{Query: query, Result: result, ResultSummary: summarizeStatementResult(result, err), Err: err}
	}
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

func buildLatestResultContext(query string, originMode AppMode, result *db.StatementResult) *LatestResultContext {
	if result == nil {
		return nil
	}

	context := &LatestResultContext{
		Query:         query,
		OriginMode:    originMode,
		StatementKind: result.Kind,
		RowsAffected:  cloneInt64Pointer(result.RowsAffected),
		LastInsertID:  cloneInt64Pointer(result.LastInsertID),
	}

	if result.ResultSet != nil {
		context.PreservedResult = cloneResultSet(result.ResultSet)
		context.InlineResult = buildInlineResultSet(query, result.ResultSet)
		context.InlineRowsTruncated = resultSetRowCount(context.InlineResult) < resultSetRowCount(context.PreservedResult)
	}

	return context
}

func buildModeSwitchContext(fromMode, toMode AppMode, latest *LatestResultContext) *ModeSwitchContext {
	return &ModeSwitchContext{
		FromMode:      fromMode,
		ToMode:        toMode,
		ResultContext: cloneLatestResultContext(latest),
	}
}

func nextModeForIntent(current AppMode) AppMode {
	switch current {
	case ModeRecordViewer:
		return ModeCommand
	default:
		return ModeRecordViewer
	}
}

func describeModeSwitchStatus(context *ModeSwitchContext) string {
	if context == nil {
		return "Mode switch requested."
	}

	if context.ToMode == ModeCommand {
		return "Returned to command mode."
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		return "Record viewer is available after running a query that returns tabular results."
	}

	result := context.ResultContext.PreservedResult
	return fmt.Sprintf("Opened record viewer for %d row(s) across %d column(s).", len(result.Rows), len(result.Columns))
}

func (m *Model) applyModeSwitch(context *ModeSwitchContext) {
	m.state.SetReady("")
	m.state.SetPendingModeSwitch(context)

	if context == nil {
		m.state.SetPendingIntent(IntentSwitchMode, "switch-mode", describeModeSwitchStatus(nil))
		return
	}

	if context.ToMode == ModeCommand {
		m.state.SetActiveMode(ModeCommand)
		m.state.SetPendingModeSwitch(nil)
		m.state.SetPendingIntent(IntentNone, "switch-mode", describeModeSwitchStatus(context))
		return
	}

	if context.ResultContext == nil || context.ResultContext.PreservedResult == nil {
		m.state.SetPendingIntent(IntentSwitchMode, "switch-mode", describeModeSwitchStatus(context))
		return
	}

	m.state.SetActiveMode(context.ToMode)
	m.state.SetPendingModeSwitch(nil)
	m.state.SetPendingIntent(IntentNone, "switch-mode", describeModeSwitchStatus(context))
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
