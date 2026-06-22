package app

import (
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/tui"
)

type Pane string

const (
	PaneCommand     Pane = "command"
	PaneResults Pane = "results-pane"
)

// AppModal tracks which modal is currently open. Orthogonal to
// ActivePane: a modal can be open while either pane has focus.
type AppModal string

const (
	ModalNone          AppModal = ""
	ModalHistorySearch AppModal = "history-search"
	ModalSlashWizard   AppModal = "slash-wizard"
	ModalKeybindings   AppModal = "keybindings"
	ModalExportWizard  AppModal = "export-wizard"
)

type AppLayout string

const (
	LayoutSplit       AppLayout = "split"
	LayoutCommandOnly AppLayout = "command-only"
	LayoutResultsOnly  AppLayout = "results-pane-only"
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
	IntentSwitchPane PendingIntent = "switch-pane"
	IntentClearCommandPane PendingIntent = "clear-command-pane"
)

type NotificationLevel int

const (
	NotificationNone    NotificationLevel = 0
	NotificationSuccess NotificationLevel = 1
	NotificationInfo    NotificationLevel = 2
	NotificationError   NotificationLevel = 3
)

type Notification struct {
	Text      string
	Level     NotificationLevel
	CreatedAt time.Time
}

type SharedAppState struct {
	App          AppStateContext
	Interaction  InteractionState
	Notification Notification
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
	CurrentSQL         string
	LastSubmittedSQL   string
	PendingIntent      PendingIntent
	LastAction         string
	Running            *RunningStatementContext
	Layout             AppLayout
	ActivePane         Pane
	ActiveModal        AppModal
	ResultsPanePage    int
	WindowFocused      bool
	MarkedRows         []int
	History            []HistoryEntryContext
	AutocompleteSchema *AutocompleteSchemaContext
	LatestResult       *LatestResultContext
	PendingPaneSwitch  *PaneSwitchContext
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
	SlashCommandWizardStepColumn  SlashCommandWizardStep = "column"
)

type SlashCommandWizardContext struct {
	Step                 SlashCommandWizardStep
	Commands             []SlashCommandWizardCommand
	Targets              []SlashCommandWizardTarget
	Columns              []SlashCommandWizardColumn
	SelectedCommand      int
	SelectedTarget       int
	SelectedColumnCursor int
	DirectInvocation     bool
	TargetFilter         string
}

type SlashCommandWizardCommand struct {
	Name         string
	DisplayName  string
	Summary      string
	Usage        string
	NeedsTarget  bool
	NeedsColumns bool
}

type SlashCommandWizardTarget struct {
	Value   string
	Display string
}

type SlashCommandWizardColumn struct {
	Name     string
	Type     string
	Selected bool
}

type AutocompleteSchemaContext struct {
	Tables []AutocompleteTableContext
}

type AutocompleteTableContext struct {
	Namespace   string
	Name        string
	Columns     []string
	ColumnTypes map[string]string // column name (lowercase) -> type
}

type LatestResultContext struct {
	Statement           string
	OriginPane          Pane
	PreservedResult     *db.ResultSet
	InlineResult        *db.ResultSet
	StatementKind       db.StatementResultKind
	RowsAffected        *int64
	LastInsertID        *int64
	InlineRowsTruncated bool
}

type PaneSwitchContext struct {
	FromLayout    AppLayout
	ToLayout      AppLayout
	FromPane      Pane
	ToPane        Pane
	ResultContext *LatestResultContext
}

type HistoryEntryContext struct {
	Statement      string
	ConnectionName string
	ExecutedAt     time.Time
}

func NewSharedAppState() SharedAppState {
	return SharedAppState{
		App: AppStateContext{
			Current: StateStartup,
		},
		Interaction: InteractionState{
			Layout:          LayoutSplit,
			ActivePane:      PaneCommand,
			ResultsPanePage: 0,
			WindowFocused:   true,
		},
		Notification: Notification{Text: "Starting SQLcery.", Level: NotificationInfo, CreatedAt: time.Now()},
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
	text := defaultStatus(status, "Starting SQLcery.")
	s.Notification = Notification{Text: text, Level: NotificationInfo, CreatedAt: time.Now()}
}

func (s *SharedAppState) SetReady(status string, level NotificationLevel) {
	s.App.Current = StateReady
	s.App.Error = ""
	s.App.Reconnect = nil
	text := strings.TrimSpace(status)
	if text == "" {
		s.Notification = Notification{}
	} else {
		s.Notification = Notification{Text: text, Level: level, CreatedAt: time.Now()}
	}
}

func (s *SharedAppState) SetReconnect(status string, reconnect *ReconnectContext) {
	s.App.Current = StateReconnect
	s.App.Error = ""
	s.App.Reconnect = cloneReconnectContext(reconnect)
	text := defaultStatus(status, "Reconnect requested; retry flow not implemented yet.")
	s.Notification = Notification{Text: text, Level: NotificationInfo, CreatedAt: time.Now()}
}

func (s *SharedAppState) SetError(message, status string) {
	s.App.Current = StateError
	s.App.Error = defaultStatus(message, "An unexpected error occurred.")
	s.App.Reconnect = nil
	text := defaultStatus(status, "The app is paused in an error state.")
	s.Notification = Notification{Text: text, Level: NotificationError, CreatedAt: time.Now()}
}

func (s *SharedAppState) SetCurrentSQL(sql string) {
	s.Interaction.CurrentSQL = sql
}

func (s *SharedAppState) SetLastSubmittedSQL(sql string) {
	s.Interaction.LastSubmittedSQL = sql
}

func (s *SharedAppState) SetPendingIntent(intent PendingIntent, action, status string, level NotificationLevel) {
	s.Interaction.PendingIntent = intent
	s.Interaction.LastAction = strings.TrimSpace(action)
	text := strings.TrimSpace(status)
	if text != "" {
		s.Notification = Notification{Text: text, Level: level, CreatedAt: time.Now()}
	}
}

func (s *SharedAppState) SetActivePane(pane Pane) {
	if strings.TrimSpace(string(pane)) == "" {
		pane = PaneCommand
	}

	s.Interaction.ActivePane = pane
}

func (s *SharedAppState) SetActiveModal(modal AppModal) {
	s.Interaction.ActiveModal = modal
}

func (s *SharedAppState) SetLayout(layout AppLayout) {
	switch layout {
	case LayoutSplit, LayoutResultsOnly:
		// keep requested layout
	default:
		layout = LayoutCommandOnly
	}

	s.Interaction.Layout = layout
}

func (s *SharedAppState) SetRunningStatementContext(context *RunningStatementContext) {
	s.Interaction.Running = context
}

func (s *SharedAppState) SetHistory(entries []HistoryEntryContext) {
	s.Interaction.History = entries
}

func (s *SharedAppState) SetLatestResultContext(context *LatestResultContext) {
	s.Interaction.LatestResult = cloneLatestResultContext(context)
	s.Interaction.ResultsPanePage = 0
	s.Interaction.MarkedRows = nil
}

func (s *SharedAppState) SetMarkedRows(rows []int) {
	if len(rows) == 0 {
		s.Interaction.MarkedRows = nil
		return
	}
	s.Interaction.MarkedRows = append([]int(nil), rows...)
}

func (s *SharedAppState) ClearMarkedRows() {
	s.Interaction.MarkedRows = nil
}

func (s *SharedAppState) SetResultsPanePage(page int) {
	s.Interaction.ResultsPanePage = tui.ClampResultsPanePage(page, resultsPaneRowCount(s.Interaction.LatestResult))
}

func (s *SharedAppState) ChangeResultsPanePage(delta int) {
	s.SetResultsPanePage(s.Interaction.ResultsPanePage + delta)
}

func (s *SharedAppState) SetPendingPaneSwitch(context *PaneSwitchContext) {
	s.Interaction.PendingPaneSwitch = clonePaneSwitchContext(context)
}

func (s *SharedAppState) SetAutocompleteSchema(schema *AutocompleteSchemaContext) {
	s.Interaction.AutocompleteSchema = schema
}

func (q InteractionState) snapshot() InteractionState {
	clone := q
	clone.Running = cloneRunningStatementContext(q.Running)
	clone.History = cloneHistoryEntries(q.History)
	clone.AutocompleteSchema = cloneAutocompleteSchemaContext(q.AutocompleteSchema)
	clone.LatestResult = cloneLatestResultContext(q.LatestResult)
	clone.PendingPaneSwitch = clonePaneSwitchContext(q.PendingPaneSwitch)
	clone.MarkedRows = append([]int(nil), q.MarkedRows...)
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
	clone.RowsAffected = cloneInt64Pointer(context.RowsAffected)
	clone.LastInsertID = cloneInt64Pointer(context.LastInsertID)
	return &clone
}

func clonePaneSwitchContext(context *PaneSwitchContext) *PaneSwitchContext {
	if context == nil {
		return nil
	}

	clone := *context
	clone.ResultContext = cloneLatestResultContext(context.ResultContext)
	return &clone
}

func cloneAutocompleteSchemaContext(schema *AutocompleteSchemaContext) *AutocompleteSchemaContext {
	if schema == nil {
		return nil
	}

	clone := &AutocompleteSchemaContext{Tables: make([]AutocompleteTableContext, len(schema.Tables))}
	for i, table := range schema.Tables {
		entry := AutocompleteTableContext{
			Namespace: table.Namespace,
			Name:      table.Name,
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

func cloneHistoryEntries(entries []HistoryEntryContext) []HistoryEntryContext {
	if len(entries) == 0 {
		return nil
	}

	clone := make([]HistoryEntryContext, len(entries))
	copy(clone, entries)
	return clone
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
