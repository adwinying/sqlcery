package app

import (
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

type AppMode string

const (
	ModeCommand       AppMode = "command"
	ModeHistorySearch AppMode = "history-search"
	ModeRecordViewer  AppMode = "record-viewer"
)

type AppLayout string

const (
	LayoutSplit       AppLayout = "split"
	LayoutCommandOnly AppLayout = "command-only"
	LayoutViewerOnly  AppLayout = "viewer-only"
)

type AppState string

const (
	StateStartup   AppState = "startup"
	StateReady     AppState = "ready"
	StateReconnect AppState = "reconnect"
	StateError     AppState = "error"
)

type PendingIntent string

const (
	IntentNone       PendingIntent = ""
	IntentSubmit     PendingIntent = "submit"
	IntentHistory    PendingIntent = "history"
	IntentSwitchMode PendingIntent = "switch-mode"
	IntentClearCommandPane PendingIntent = "clear-command-pane"
)

type SharedAppState struct {
	App    AppStateContext
	Interaction  InteractionState
	Status string
}

type AppStateContext struct {
	Current   AppState
	Error     string
	Reconnect *ReconnectContext
}

type ReconnectContext struct {
	Attempt   int
	Reason    string
	LastError string
}

type InteractionState struct {
	CurrentSQL           string
	LastSubmittedSQL     string
	PendingIntent        PendingIntent
	LastAction           string
	HelpVisible          bool
	Running              *RunningStatementContext
	Layout               AppLayout
	ActiveMode           AppMode
	ViewerPage           int
	SessionHistory       []HistoryEntryContext
	HistorySearch        *HistorySearchContext
	AutocompleteSchema   *AutocompleteSchemaContext
	LatestResult         *LatestResultContext
	SlashWizard          *SlashCommandWizardContext
	PendingModeSwitch    *ModeSwitchContext
	SelectedHistoryEntry *HistoryEntryContext
}

type HistorySearchContext struct {
	Filter        string
	SelectedIndex int
}

type RunningStatementContext struct {
	Label        string
	StartedAt    time.Time
	Elapsed      time.Duration
	SpinnerFrame int
}

type SlashCommandWizardStep string

const (
	SlashCommandWizardStepCommand SlashCommandWizardStep = "command"
	SlashCommandWizardStepTarget  SlashCommandWizardStep = "target"
)

type SlashCommandWizardContext struct {
	Step             SlashCommandWizardStep
	Commands         []SlashCommandWizardCommand
	Targets          []SlashCommandWizardTarget
	SelectedCommand  int
	SelectedTarget   int
	DirectInvocation bool
	TargetFilter     string
}

type SlashCommandWizardCommand struct {
	Name        string
	DisplayName string
	Summary     string
	Usage       string
	NeedsTarget bool
}

type SlashCommandWizardTarget struct {
	Value   string
	Display string
}

type AutocompleteSchemaContext struct {
	Tables []AutocompleteTableContext
}

type AutocompleteTableContext struct {
	Schema      string
	Name        string
	Columns     []string
	ColumnTypes map[string]string // column name (lowercase) -> type
}

type LatestResultContext struct {
	Statement           string
	OriginMode          AppMode
	PreservedResult     *db.ResultSet
	InlineResult        *db.ResultSet
	SelectedRows        []int
	StatementKind       db.StatementResultKind
	RowsAffected        *int64
	LastInsertID        *int64
	InlineRowsTruncated bool
}

type ModeSwitchContext struct {
	FromLayout    AppLayout
	ToLayout      AppLayout
	FromMode      AppMode
	ToMode        AppMode
	ResultContext *LatestResultContext
}

type HistoryEntryContext struct {
	SQL            string
	ConnectionName string
	ExecutedAt     time.Time
}

func NewSharedAppState() SharedAppState {
	return SharedAppState{
		App: AppStateContext{
			Current: StateStartup,
		},
		Interaction: InteractionState{
			Layout:     LayoutSplit,
			ActiveMode: ModeCommand,
			ViewerPage: 0,
		},
		Status: "Starting SQLcery.",
	}
}

func (s SharedAppState) Snapshot() SharedAppState {
	clone := s
	clone.App = s.App.snapshot()
	clone.Interaction = s.Interaction.snapshot()
	return clone
}

func (s *SharedAppState) SetStartup(status string) {
	s.App.Current = StateStartup
	s.App.Error = ""
	s.App.Reconnect = nil
	s.Status = defaultStatus(status, "Starting SQLcery.")
}

func (s *SharedAppState) SetReady(status string) {
	s.App.Current = StateReady
	s.App.Error = ""
	s.App.Reconnect = nil
	s.Status = defaultStatus(status, "Ready for SQL input.")
}

func (s *SharedAppState) SetReconnect(status string, reconnect *ReconnectContext) {
	s.App.Current = StateReconnect
	s.App.Error = ""
	s.App.Reconnect = cloneReconnectContext(reconnect)
	s.Status = defaultStatus(status, "Reconnect requested; retry flow not implemented yet.")
}

func (s *SharedAppState) SetError(message, status string) {
	s.App.Current = StateError
	s.App.Error = defaultStatus(message, "An unexpected error occurred.")
	s.App.Reconnect = nil
	s.Status = defaultStatus(status, "The app is paused in an error state.")
}

func (s *SharedAppState) SetCurrentSQL(sql string) {
	s.Interaction.CurrentSQL = sql
}

func (s *SharedAppState) SetLastSubmittedSQL(sql string) {
	s.Interaction.LastSubmittedSQL = sql
}

func (s *SharedAppState) SetPendingIntent(intent PendingIntent, action, status string) {
	s.Interaction.PendingIntent = intent
	s.Interaction.LastAction = strings.TrimSpace(action)
	s.Status = strings.TrimSpace(status)
}

func (s *SharedAppState) SetActiveMode(mode AppMode) {
	if strings.TrimSpace(string(mode)) == "" {
		mode = ModeCommand
	}

	s.Interaction.ActiveMode = mode
}

func (s *SharedAppState) SetLayout(layout AppLayout) {
	switch layout {
	case LayoutSplit, LayoutViewerOnly:
		// keep requested layout
	default:
		layout = LayoutCommandOnly
	}

	s.Interaction.Layout = layout
}

func (s *SharedAppState) SetRunningStatementContext(context *RunningStatementContext) {
	s.Interaction.Running = cloneRunningStatementContext(context)
}

func (s *SharedAppState) SetSessionHistory(entries []HistoryEntryContext) {
	s.Interaction.SessionHistory = cloneHistoryEntries(entries)
}

func (s *SharedAppState) SetHistorySearchContext(context *HistorySearchContext) {
	s.Interaction.HistorySearch = cloneHistorySearchContext(context)
}

func (s *SharedAppState) SetLatestResultContext(context *LatestResultContext) {
	s.Interaction.LatestResult = cloneLatestResultContext(context)
	s.Interaction.ViewerPage = 0
}

func (s *SharedAppState) SetViewerPage(page int) {
	s.Interaction.ViewerPage = clampRecordViewerPage(page, recordViewerRowCount(s.Interaction.LatestResult))
}

func (s *SharedAppState) ChangeViewerPage(delta int) {
	s.SetViewerPage(s.Interaction.ViewerPage + delta)
}

func (s *SharedAppState) SetSlashWizardContext(context *SlashCommandWizardContext) {
	s.Interaction.SlashWizard = cloneSlashCommandWizardContext(context)
}

func (s *SharedAppState) SetPendingModeSwitch(context *ModeSwitchContext) {
	s.Interaction.PendingModeSwitch = cloneModeSwitchContext(context)
}

func (s *SharedAppState) SetHelpVisible(visible bool) {
	s.Interaction.HelpVisible = visible
}

func (s *SharedAppState) SetAutocompleteSchema(schema *AutocompleteSchemaContext) {
	s.Interaction.AutocompleteSchema = cloneAutocompleteSchemaContext(schema)
}

func (s *SharedAppState) SetSelectedHistoryEntry(entry *HistoryEntryContext) {
	s.Interaction.SelectedHistoryEntry = cloneHistoryEntryContext(entry)
}

func (q InteractionState) snapshot() InteractionState {
	clone := q
	clone.Running = cloneRunningStatementContext(q.Running)
	clone.SessionHistory = cloneHistoryEntries(q.SessionHistory)
	clone.HistorySearch = cloneHistorySearchContext(q.HistorySearch)
	clone.AutocompleteSchema = cloneAutocompleteSchemaContext(q.AutocompleteSchema)
	clone.LatestResult = cloneLatestResultContext(q.LatestResult)
	clone.SlashWizard = cloneSlashCommandWizardContext(q.SlashWizard)
	clone.PendingModeSwitch = cloneModeSwitchContext(q.PendingModeSwitch)
	clone.SelectedHistoryEntry = cloneHistoryEntryContext(q.SelectedHistoryEntry)
	return clone
}

func cloneRunningStatementContext(context *RunningStatementContext) *RunningStatementContext {
	if context == nil {
		return nil
	}

	clone := *context
	return &clone
}

func (a AppStateContext) snapshot() AppStateContext {
	clone := a
	clone.Reconnect = cloneReconnectContext(a.Reconnect)
	return clone
}

func cloneLatestResultContext(context *LatestResultContext) *LatestResultContext {
	if context == nil {
		return nil
	}

	clone := *context
	clone.PreservedResult = cloneResultSet(context.PreservedResult)
	clone.InlineResult = cloneResultSet(context.InlineResult)
	clone.SelectedRows = append([]int(nil), context.SelectedRows...)
	clone.RowsAffected = cloneInt64Pointer(context.RowsAffected)
	clone.LastInsertID = cloneInt64Pointer(context.LastInsertID)
	return &clone
}

func cloneModeSwitchContext(context *ModeSwitchContext) *ModeSwitchContext {
	if context == nil {
		return nil
	}

	clone := *context
	clone.ResultContext = cloneLatestResultContext(context.ResultContext)
	return &clone
}

func cloneSlashCommandWizardContext(context *SlashCommandWizardContext) *SlashCommandWizardContext {
	if context == nil {
		return nil
	}

	clone := &SlashCommandWizardContext{
		Step:             context.Step,
		SelectedCommand:  context.SelectedCommand,
		SelectedTarget:   context.SelectedTarget,
		DirectInvocation: context.DirectInvocation,
		TargetFilter:     context.TargetFilter,
		Commands:         make([]SlashCommandWizardCommand, len(context.Commands)),
		Targets:          make([]SlashCommandWizardTarget, len(context.Targets)),
	}
	copy(clone.Commands, context.Commands)
	copy(clone.Targets, context.Targets)
	return clone
}

func cloneAutocompleteSchemaContext(schema *AutocompleteSchemaContext) *AutocompleteSchemaContext {
	if schema == nil {
		return nil
	}

	clone := &AutocompleteSchemaContext{Tables: make([]AutocompleteTableContext, len(schema.Tables))}
	for i, table := range schema.Tables {
		entry := AutocompleteTableContext{
			Schema:  table.Schema,
			Name:    table.Name,
			Columns: append([]string(nil), table.Columns...),
		}
		if table.ColumnTypes != nil {
			entry.ColumnTypes = make(map[string]string, len(table.ColumnTypes))
			for k, v := range table.ColumnTypes {
				entry.ColumnTypes[k] = v
			}
		}
		clone.Tables[i] = entry
	}

	return clone
}

func cloneHistoryEntryContext(entry *HistoryEntryContext) *HistoryEntryContext {
	if entry == nil {
		return nil
	}

	clone := *entry
	return &clone
}

func cloneHistoryEntries(entries []HistoryEntryContext) []HistoryEntryContext {
	if len(entries) == 0 {
		return nil
	}

	clone := make([]HistoryEntryContext, len(entries))
	copy(clone, entries)
	return clone
}

func cloneHistorySearchContext(context *HistorySearchContext) *HistorySearchContext {
	if context == nil {
		return nil
	}

	clone := *context
	return &clone
}

func cloneReconnectContext(reconnect *ReconnectContext) *ReconnectContext {
	if reconnect == nil {
		return nil
	}

	clone := *reconnect
	return &clone
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}

	copy := *value
	return &copy
}

func defaultStatus(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}

	return fallback
}

func cloneResultSet(result *db.ResultSet) *db.ResultSet {
	if result == nil {
		return nil
	}

	clone := &db.ResultSet{}
	if result.Source != nil {
		source := *result.Source
		clone.Source = &source
	}

	clone.Columns = make([]db.ResultColumn, len(result.Columns))
	for i, column := range result.Columns {
		clone.Columns[i] = cloneResultColumn(column)
	}

	clone.Rows = make([]db.ResultRow, len(result.Rows))
	for i, row := range result.Rows {
		clone.Rows[i] = cloneResultRow(row)
	}

	return clone
}

func cloneResultColumn(column db.ResultColumn) db.ResultColumn {
	clone := column

	if column.Nullable != nil {
		nullable := *column.Nullable
		clone.Nullable = &nullable
	}

	if column.Length != nil {
		length := *column.Length
		clone.Length = &length
	}

	if column.DecimalSize != nil {
		decimalSize := *column.DecimalSize
		clone.DecimalSize = &decimalSize
	}

	if column.Schema != nil {
		schema := *column.Schema
		clone.Schema = &schema
	}

	if column.PrimaryKey != nil {
		primaryKey := *column.PrimaryKey
		clone.PrimaryKey = &primaryKey
	}

	return clone
}

func cloneResultRow(row db.ResultRow) db.ResultRow {
	clone := db.ResultRow{
		Position: row.Position,
		Values:   make([]db.ResultValue, len(row.Values)),
	}

	for i, value := range row.Values {
		clone.Values[i] = cloneResultValue(value)
	}

	return clone
}

func cloneResultValue(value db.ResultValue) db.ResultValue {
	clone := value

	if bytesValue, ok := value.Value.([]byte); ok {
		clone.Value = append([]byte(nil), bytesValue...)
	}

	return clone
}
