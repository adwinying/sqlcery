package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/adwinying/sqlcery/internal/db"
)

type slashCommand struct {
	RawInput     string
	DisplayName  string
	Name         string
	Args         []string
	RawArguments string
}

type slashCommandContext struct {
	Session Session
	Dialect db.Dialect
	Query   InteractionState
}

type slashCommandResult struct {
	Action         string
	Status         string
	Statement      *db.StatementResult
	ReplaceEditor  string
	ShouldReplace  bool
	Wizard         *SlashCommandWizardContext
	PreserveResult bool
}

type slashCommandHandler func(context.Context, slashCommandContext, slashCommand) (slashCommandResult, error)

type slashCommandSpec struct {
	Name         string
	Summary      string
	Usage        string
	Handler      slashCommandHandler
	NeedsTarget  bool
	NeedsColumns bool
}

type slashCommandRegistry struct {
	ordered []slashCommandSpec
	byName  map[string]slashCommandSpec
}

type helpSection struct {
	Title string
	Lines []string
}

var defaultSlashCommandRegistry = newSlashCommandRegistry()

func slashCommandSpecs() []slashCommandSpec {
	return []slashCommandSpec{
		{Name: "commands", Summary: "open the guided slash command wizard", Usage: "/commands", Handler: handleSlashCommands},
		{Name: "tables", Summary: "list tables in the current database", Usage: "/tables", Handler: handleSlashTables},
		{Name: "columns", Summary: "list columns for a table", Usage: "/columns <table>", Handler: handleSlashColumns, NeedsTarget: true},
		{Name: "indices", Summary: "list indices for a table", Usage: "/indices <table>", Handler: handleSlashIndices, NeedsTarget: true},
		{Name: "select", Summary: "compose a SELECT statement", Usage: "/select <table>", Handler: handleSlashSelect, NeedsTarget: true, NeedsColumns: true},
		{Name: "insert", Summary: "compose an INSERT statement", Usage: "/insert <table>", Handler: handleSlashInsert, NeedsTarget: true},
		{Name: "update", Summary: "compose an UPDATE statement", Usage: "/update <table>", Handler: handleSlashUpdate, NeedsTarget: true},
		{Name: "delete", Summary: "compose a DELETE statement", Usage: "/delete <table>", Handler: handleSlashDelete, NeedsTarget: true},
		{Name: "create", Summary: "compose a CREATE TABLE statement", Usage: "/create <table>", Handler: handleSlashCreate, NeedsTarget: true},
		{Name: "drop", Summary: "compose a DROP TABLE statement", Usage: "/drop <table>", Handler: handleSlashDrop, NeedsTarget: true},
	}
}

func newSlashCommandRegistry() slashCommandRegistry {
	specs := slashCommandSpecs()

	registry := slashCommandRegistry{
		ordered: append([]slashCommandSpec(nil), specs...),
		byName:  make(map[string]slashCommandSpec, len(specs)),
	}
	for _, spec := range specs {
		registry.byName[spec.Name] = spec
	}

	return registry
}

func (r slashCommandRegistry) names() []string {
	results := make([]string, 0, len(r.ordered))
	for _, spec := range r.ordered {
		results = append(results, "/"+spec.Name)
	}
	return results
}

func parseSlashCommand(input string) (*slashCommand, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return nil, nil
	}

	parts, err := splitSlashCommandInput(trimmed)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("slash command name is required")
	}

	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	if name == "" {
		return nil, fmt.Errorf("slash command name is required")
	}

	command := &slashCommand{
		RawInput:    trimmed,
		DisplayName: "/" + name,
		Name:        name,
		Args:        append([]string(nil), parts[1:]...),
	}
	if len(parts) > 1 {
		command.RawArguments = strings.Join(parts[1:], " ")
	}

	return command, nil
}

func splitSlashCommandInput(input string) ([]string, error) {
	runes := []rune(input)
	parts := make([]string, 0, 4)

	for i := 0; i < len(runes); {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}

		var builder strings.Builder
		quote := rune(0)
		started := false

		for i < len(runes) {
			r := runes[i]
			switch {
			case quote != 0:
				started = true
				switch r {
				case '\\':
					if i+1 >= len(runes) {
						return nil, fmt.Errorf("slash command has an incomplete escape sequence")
					}
					builder.WriteRune(runes[i+1])
					i += 2
				case quote:
					quote = 0
					i++
				default:
					builder.WriteRune(r)
					i++
				}
			case unicode.IsSpace(r):
				if !started {
					i++
					continue
				}
				parts = append(parts, builder.String())
				i++
				goto nextPart
			case r == '\'' || r == '"':
				started = true
				quote = r
				i++
			case r == '\\':
				if i+1 >= len(runes) {
					return nil, fmt.Errorf("slash command has an incomplete escape sequence")
				}
				started = true
				builder.WriteRune(runes[i+1])
				i += 2
			default:
				started = true
				builder.WriteRune(r)
				i++
			}
		}

		if quote != 0 {
			return nil, fmt.Errorf("slash command has an unterminated quoted argument")
		}
		if started {
			parts = append(parts, builder.String())
		}

		continue

	nextPart:
		continue
	}

	return parts, nil
}

func dispatchSlashCommand(ctx context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	spec, ok := defaultSlashCommandRegistry.byName[parsed.Name]
	if !ok {
		return slashCommandResult{}, fmt.Errorf("unknown slash command %s", parsed.DisplayName)
	}

	result, err := spec.Handler(ctx, command, parsed)
	if err != nil {
		return slashCommandResult{}, err
	}
	if strings.TrimSpace(result.Action) == "" {
		result.Action = "slash:" + parsed.DisplayName
	}

	return result, nil
}

func slashCommandHelpLines() []string {
	lines := make([]string, 0, len(defaultSlashCommandRegistry.ordered))
	for _, spec := range defaultSlashCommandRegistry.ordered {
		lines = append(lines, fmt.Sprintf("%s - %s (%s)", "/"+spec.Name, spec.Summary, spec.Usage))
	}
	return lines
}

func handleSlashCommands(_ context.Context, _ slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	if err := validateSlashCommandArgs(parsed, 0); err != nil {
		return slashCommandResult{}, err
	}

	commands := buildSlashWizardCommands()
	if len(commands) == 0 {
		return slashCommandResult{}, fmt.Errorf("no slash commands available")
	}

	return slashCommandResult{
		Status: "Opened the slash command wizard. Choose a command and press enter.",
		Wizard: &SlashCommandWizardContext{
			Step:     SlashCommandWizardStepCommand,
			Commands: commands,
		},
		PreserveResult: true,
	}, nil
}

func handleSlashTables(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	sql := slashTablesSQL(command.Dialect)
	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, "current database"),
		ReplaceEditor: sql,
		ShouldReplace: true,
	}, nil
}

func slashTablesSQL(dialect db.Dialect) string {
	switch slashDialectOrFallback(dialect).Name() {
	case "postgres":
		return "SELECT table_schema, table_name, table_type\nFROM information_schema.tables\nWHERE table_schema NOT IN ('pg_catalog', 'information_schema')\nORDER BY table_schema, table_name;"
	case "mysql":
		return "SHOW TABLES;"
	default: // sqlite
		return "SELECT name, type\nFROM sqlite_master\nWHERE type IN ('table', 'view')\nORDER BY name;"
	}
}

func handleSlashColumns(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	if err := validateSlashCommandArgs(parsed, 1); err != nil {
		return slashCommandResult{}, err
	}

	table := parseSlashTableRef(parsed.Args[0])
	sql := slashColumnsSQL(command.Dialect, table)
	qualified := displaySlashTableRef(table)
	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, qualified),
		ReplaceEditor: sql,
		ShouldReplace: true,
	}, nil
}

func slashColumnsSQL(dialect db.Dialect, table db.TableRef) string {
	tableName := table.Name
	schemaName := table.Namespace
	switch slashDialectOrFallback(dialect).Name() {
	case "postgres":
		if strings.TrimSpace(schemaName) != "" {
			return fmt.Sprintf("SELECT column_name, data_type, is_nullable, column_default\nFROM information_schema.columns\nWHERE table_schema = '%s'\n  AND table_name = '%s'\nORDER BY ordinal_position;", schemaName, tableName)
		}
		return fmt.Sprintf("SELECT column_name, data_type, is_nullable, column_default\nFROM information_schema.columns\nWHERE table_name = '%s'\nORDER BY ordinal_position;", tableName)
	case "mysql":
		return fmt.Sprintf("DESCRIBE %s;", quoteSlashTableRef(dialect, table))
	default: // sqlite
		return fmt.Sprintf("PRAGMA table_info(%s);", quoteSlashTableRef(dialect, table))
	}
}

func handleSlashIndices(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	if err := validateSlashCommandArgs(parsed, 1); err != nil {
		return slashCommandResult{}, err
	}

	table := parseSlashTableRef(parsed.Args[0])
	sql := slashIndicesSQL(command.Dialect, table)
	qualified := displaySlashTableRef(table)
	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, qualified),
		ReplaceEditor: sql,
		ShouldReplace: true,
	}, nil
}

func slashIndicesSQL(dialect db.Dialect, table db.TableRef) string {
	tableName := table.Name
	schemaName := table.Namespace
	switch slashDialectOrFallback(dialect).Name() {
	case "postgres":
		if strings.TrimSpace(schemaName) != "" {
			return fmt.Sprintf("SELECT indexname, indexdef\nFROM pg_indexes\nWHERE schemaname = '%s'\n  AND tablename = '%s'\nORDER BY indexname;", schemaName, tableName)
		}
		return fmt.Sprintf("SELECT indexname, indexdef\nFROM pg_indexes\nWHERE tablename = '%s'\nORDER BY indexname;", tableName)
	case "mysql":
		return fmt.Sprintf("SHOW INDEX FROM %s;", quoteSlashTableRef(dialect, table))
	default: // sqlite
		return fmt.Sprintf("PRAGMA index_list(%s);", quoteSlashTableRef(dialect, table))
	}
}

func handleSlashSelect(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	quotedTable := quoteSlashTableRef(command.Dialect, table)
	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("SELECT * FROM %s;", quotedTable),
		ShouldReplace: true,
	}, nil
}

func buildSelectSQL(dialect db.Dialect, table db.TableRef, columns []SlashCommandWizardColumn) string {
	quotedTable := quoteSlashTableRef(dialect, table)

	allSelected := true
	for _, col := range columns {
		if !col.Selected {
			allSelected = false
			break
		}
	}
	if len(columns) == 0 || allSelected {
		return fmt.Sprintf("SELECT * FROM %s;", quotedTable)
	}

	parts := make([]string, 0, len(columns))
	for _, col := range columns {
		if col.Selected {
			parts = append(parts, "  "+quoteSlashIdentifier(dialect, col.Name))
		}
	}
	return fmt.Sprintf("SELECT\n%s\nFROM %s;", strings.Join(parts, ",\n"), quotedTable)
}

func handleSlashInsert(ctx context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	columns, _ := loadSlashColumns(ctx, command.Session.Adapter, table)
	quotedTable := quoteSlashTableRef(command.Dialect, table)
	placeholderDialect := slashDialectOrFallback(command.Dialect)

	columnNames := make([]string, 0, len(columns))
	placeholders := make([]string, 0, len(columns))
	for i, column := range columns {
		columnNames = append(columnNames, "  "+quoteSlashIdentifier(command.Dialect, column.Name))
		placeholders = append(placeholders, "  "+placeholderDialect.Placeholder(i+1))
	}
	if len(columnNames) == 0 {
		columnNames = []string{"  " + quoteSlashIdentifier(command.Dialect, "column_1")}
		placeholders = []string{"  " + placeholderDialect.Placeholder(1)}
	}

	return slashCommandResult{
		Status: slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES (\n%s\n);",
			quotedTable,
			strings.Join(columnNames, ",\n"),
			strings.Join(placeholders, ",\n"),
		),
		ShouldReplace: true,
	}, nil
}

func handleSlashUpdate(ctx context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	columns, _ := loadSlashColumns(ctx, command.Session.Adapter, table)
	primaryKeys, _ := loadSlashPrimaryKeys(ctx, command.Session.Adapter, table)
	quotedTable := quoteSlashTableRef(command.Dialect, table)
	placeholderDialect := slashDialectOrFallback(command.Dialect)

	assignments := make([]string, 0, len(columns))
	placeholderIndex := 1
	for _, column := range columns {
		if slashPrimaryKeyColumn(primaryKeys, column.Name) {
			continue
		}
		assignments = append(assignments, fmt.Sprintf("  %s = %s", quoteSlashIdentifier(command.Dialect, column.Name), placeholderDialect.Placeholder(placeholderIndex)))
		placeholderIndex++
	}
	if len(assignments) == 0 {
		assignments = []string{"  " + quoteSlashIdentifier(command.Dialect, "column_1") + " = " + placeholderDialect.Placeholder(1)}
	}

	return slashCommandResult{
		Status: slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("UPDATE %s\nSET\n%s\nWHERE condition;",
			quotedTable,
			strings.Join(assignments, ",\n"),
		),
		ShouldReplace: true,
	}, nil
}

func handleSlashDelete(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("DELETE FROM %s\nWHERE condition;", quoteSlashTableRef(command.Dialect, table)),
		ShouldReplace: true,
	}, nil
}

func handleSlashCreate(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	columnDefinitions := slashCreateColumnDefinitions(command.Dialect)

	return slashCommandResult{
		Status: slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("CREATE TABLE %s (\n%s\n);",
			quoteSlashTableRef(command.Dialect, table),
			strings.Join(columnDefinitions, ",\n"),
		),
		ShouldReplace: true,
	}, nil
}

func handleSlashDrop(_ context.Context, command slashCommandContext, parsed slashCommand) (slashCommandResult, error) {
	table, err := slashTargetTable(parsed)
	if err != nil {
		return slashCommandResult{}, err
	}

	return slashCommandResult{
		Status:        slashTemplateStatus(parsed.DisplayName, displaySlashTableRef(table)),
		ReplaceEditor: fmt.Sprintf("DROP TABLE %s;", quoteSlashTableRef(command.Dialect, table)),
		ShouldReplace: true,
	}, nil
}

func slashTemplateStatus(displayName, target string) string {
	return fmt.Sprintf("Expanded %s for %s into command mode. Review it, then press enter to run.", displayName, target)
}

func slashTargetTable(parsed slashCommand) (db.TableRef, error) {
	if err := validateSlashCommandArgs(parsed, 1); err != nil {
		return db.TableRef{}, err
	}

	return parseSlashTableRef(parsed.Args[0]), nil
}

func validateSlashCommandArgs(parsed slashCommand, want int) error {
	if len(parsed.Args) == want {
		return nil
	}

	info, ok := lookupSlashCommandInfo(parsed.Name)
	if !ok {
		return fmt.Errorf("unknown slash command %s", parsed.DisplayName)
	}

	if want == 0 {
		return fmt.Errorf("%s does not accept arguments; usage: %s", parsed.DisplayName, info.Usage)
	}

	return fmt.Errorf("%s expects %d argument(s); usage: %s", parsed.DisplayName, want, info.Usage)
}

func lookupSlashCommandInfo(name string) (slashCommandSpec, bool) {
	for _, spec := range slashCommandSpecs() {
		if strings.EqualFold(spec.Name, name) {
			return spec, true
		}
	}
	return slashCommandSpec{}, false
}

func buildSlashWizardCommands() []SlashCommandWizardCommand {
	specs := slashCommandSpecs()
	commands := make([]SlashCommandWizardCommand, 0, len(specs))
	for _, spec := range specs {
		if spec.Name == "commands" {
			continue
		}
		commands = append(commands, SlashCommandWizardCommand{
			Name:         spec.Name,
			DisplayName:  "/" + spec.Name,
			Summary:      spec.Summary,
			Usage:        spec.Usage,
			NeedsTarget:  spec.NeedsTarget,
			NeedsColumns: spec.NeedsColumns,
		})
	}
	return commands
}

func buildSlashWizardTargets(ctx context.Context, command slashCommandContext) ([]SlashCommandWizardTarget, error) {
	if command.Query.AutocompleteSchema != nil && len(command.Query.AutocompleteSchema.Tables) > 0 {
		return slashWizardTargetsFromSchema(command.Query.AutocompleteSchema), nil
	}

	adapter, err := ensureSlashAdapter(command)
	if err != nil {
		return nil, err
	}

	tables, err := adapter.Tables(ctx, db.TableFilter{})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(tables, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(tables[i].Namespace) + "." + strings.TrimSpace(tables[i].Name))
		right := strings.ToLower(strings.TrimSpace(tables[j].Namespace) + "." + strings.TrimSpace(tables[j].Name))
		return left < right
	})

	targets := make([]SlashCommandWizardTarget, 0, len(tables))
	for _, table := range tables {
		ref := db.TableRef{Catalog: table.Catalog, Namespace: table.Namespace, Name: table.Name}
		display := displaySlashTableRef(ref)
		if strings.TrimSpace(display) == "" {
			continue
		}
		targets = append(targets, SlashCommandWizardTarget{Value: display, Display: display})
	}
	return targets, nil
}

func buildSlashWizardFromCommand(ctx context.Context, command slashCommandContext, commands []SlashCommandWizardCommand, selectedCommand SlashCommandWizardCommand, selectedCommandIndex int) (*SlashCommandWizardContext, error) {
	if len(commands) == 0 {
		commands = buildSlashWizardCommands()
	}

	if !selectedCommand.NeedsTarget {
		return &SlashCommandWizardContext{
			Step:            SlashCommandWizardStepCommand,
			Commands:        append([]SlashCommandWizardCommand(nil), commands...),
			SelectedCommand: selectedCommandIndex,
		}, nil
	}

	targets, err := buildSlashWizardTargets(ctx, command)
	if err != nil {
		return nil, err
	}

	return &SlashCommandWizardContext{
		Step:            SlashCommandWizardStepTarget,
		Commands:        append([]SlashCommandWizardCommand(nil), commands...),
		SelectedCommand: selectedCommandIndex,
		Targets:         targets,
		SelectedTarget:  0,
	}, nil
}

func buildSlashWizardColumnStep(ctx context.Context, command slashCommandContext, wizard SlashCommandWizardContext, target SlashCommandWizardTarget) (*SlashCommandWizardContext, error) {
	table := parseSlashTableRef(target.Value)
	cols := buildSlashWizardColumnsFromSchema(command.Query.AutocompleteSchema, table)
	if len(cols) == 0 {
		dbCols, err := loadSlashColumns(ctx, command.Session.Adapter, table)
		if err != nil {
			return nil, err
		}
		for _, c := range dbCols {
			cols = append(cols, SlashCommandWizardColumn{Name: c.Name, Selected: true})
		}
	}

	next := wizard
	next.Step = SlashCommandWizardStepColumn
	next.Columns = cols
	next.SelectedColumnCursor = 0
	return &next, nil
}

func buildSlashWizardColumnsFromSchema(schema *AutocompleteSchemaContext, table db.TableRef) []SlashCommandWizardColumn {
	if schema == nil {
		return nil
	}
	for _, t := range schema.Tables {
		if !strings.EqualFold(t.Name, table.Name) {
			continue
		}
		if table.Namespace != "" && !strings.EqualFold(t.Namespace, table.Namespace) {
			continue
		}
		cols := make([]SlashCommandWizardColumn, 0, len(t.Columns))
		for _, name := range t.Columns {
			colType := t.ColumnTypes[strings.ToLower(name)]
			cols = append(cols, SlashCommandWizardColumn{Name: name, Type: colType, Selected: true})
		}
		return cols
	}
	return nil
}

func slashWizardTargetsFromSchema(schema *AutocompleteSchemaContext) []SlashCommandWizardTarget {
	if schema == nil {
		return nil
	}

	targets := make([]SlashCommandWizardTarget, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		ref := db.TableRef{Namespace: table.Namespace, Name: table.Name}
		display := displaySlashTableRef(ref)
		if strings.TrimSpace(display) == "" {
			continue
		}
		targets = append(targets, SlashCommandWizardTarget{Value: display, Display: display})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return strings.ToLower(targets[i].Display) < strings.ToLower(targets[j].Display)
	})
	return targets
}

func slashWizardCommandByIndex(wizard *SlashCommandWizardContext) (SlashCommandWizardCommand, bool) {
	if wizard == nil || len(wizard.Commands) == 0 {
		return SlashCommandWizardCommand{}, false
	}
	index := wizard.SelectedCommand
	if index < 0 {
		index = 0
	}
	if index >= len(wizard.Commands) {
		index = len(wizard.Commands) - 1
	}
	return wizard.Commands[index], true
}

func slashWizardTargetByIndex(wizard *SlashCommandWizardContext) (SlashCommandWizardTarget, bool) {
	if wizard == nil || len(wizard.Targets) == 0 {
		return SlashCommandWizardTarget{}, false
	}
	index := wizard.SelectedTarget
	if index < 0 {
		index = 0
	}
	if index >= len(wizard.Targets) {
		index = len(wizard.Targets) - 1
	}
	return wizard.Targets[index], true
}

func slashWizardFilteredTargetByIndex(wizard *SlashCommandWizardContext) (SlashCommandWizardTarget, bool) {
	if wizard == nil {
		return SlashCommandWizardTarget{}, false
	}
	filtered := filterWizardTargets(wizard.Targets, wizard.TargetFilter)
	if len(filtered) == 0 {
		return SlashCommandWizardTarget{}, false
	}
	index := wizard.SelectedTarget
	if index < 0 {
		index = 0
	}
	if index >= len(filtered) {
		index = len(filtered) - 1
	}
	return filtered[index], true
}

func buildSlashWizardCommand(command SlashCommandWizardCommand, target *SlashCommandWizardTarget) slashCommand {
	parsed := slashCommand{
		RawInput:    command.DisplayName,
		DisplayName: command.DisplayName,
		Name:        command.Name,
	}
	if target != nil {
		parsed.Args = []string{target.Value}
		parsed.RawArguments = target.Value
		parsed.RawInput = command.DisplayName + " " + target.Value
	}
	return parsed
}

func ensureSlashAdapter(command slashCommandContext) (*db.SQLAdapter, error) {
	if command.Session.Adapter == nil {
		return nil, fmt.Errorf("adapter is required")
	}

	return command.Session.Adapter, nil
}

func loadSlashColumns(ctx context.Context, adapter *db.SQLAdapter, table db.TableRef) ([]db.Column, error) {
	if adapter == nil {
		return nil, nil
	}

	columns, err := adapter.Columns(ctx, table)
	if err != nil && !errors.Is(err, db.ErrMetadataUnsupported) {
		return nil, err
	}
	if err != nil {
		return nil, nil
	}

	return columns, nil
}

func loadSlashPrimaryKeys(ctx context.Context, adapter *db.SQLAdapter, table db.TableRef) ([]db.PrimaryKey, error) {
	if adapter == nil {
		return nil, nil
	}

	keys, err := adapter.PrimaryKeys(ctx, table)
	if err != nil && !errors.Is(err, db.ErrMetadataUnsupported) {
		return nil, err
	}
	if err != nil {
		return nil, nil
	}

	return keys, nil
}

func parseSlashTableRef(value string) db.TableRef {
	parts := strings.Split(strings.TrimSpace(value), ".")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		clean = append(clean, unquoteSlashIdentifier(trimmed))
	}

	ref := db.TableRef{}
	switch len(clean) {
	case 0:
		return ref
	case 1:
		ref.Name = clean[0]
	case 2:
		ref.Namespace = clean[0]
		ref.Name = clean[1]
	default:
		ref.Catalog = clean[len(clean)-3]
		ref.Namespace = clean[len(clean)-2]
		ref.Name = clean[len(clean)-1]
	}

	return ref
}

func unquoteSlashIdentifier(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		switch {
		case trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"':
			return trimmed[1 : len(trimmed)-1]
		case trimmed[0] == '`' && trimmed[len(trimmed)-1] == '`':
			return trimmed[1 : len(trimmed)-1]
		case trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']':
			return trimmed[1 : len(trimmed)-1]
		}
	}

	return trimmed
}

func displaySlashTableRef(table db.TableRef) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(table.Catalog) != "" {
		parts = append(parts, table.Catalog)
	}
	if strings.TrimSpace(table.Namespace) != "" {
		parts = append(parts, table.Namespace)
	}
	if strings.TrimSpace(table.Name) != "" {
		parts = append(parts, table.Name)
	}
	return strings.Join(parts, ".")
}

func quoteSlashTableRef(dialect db.Dialect, table db.TableRef) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(table.Catalog) != "" {
		parts = append(parts, table.Catalog)
	}
	if strings.TrimSpace(table.Namespace) != "" {
		parts = append(parts, table.Namespace)
	}
	if strings.TrimSpace(table.Name) != "" {
		parts = append(parts, table.Name)
	}
	if len(parts) == 0 {
		return ""
	}

	dialect = slashDialectOrFallback(dialect)
	return dialect.QuoteIdentifier(parts...)
}

func quoteSlashIdentifier(dialect db.Dialect, value string) string {
	dialect = slashDialectOrFallback(dialect)
	return dialect.QuoteIdentifier(value)
}

func slashDialectOrFallback(dialect db.Dialect) db.Dialect {
	if dialect != nil {
		return dialect
	}

	return db.SQLiteDialect()
}

func slashPrimaryKeyColumn(keys []db.PrimaryKey, name string) bool {
	for _, key := range keys {
		if strings.EqualFold(key.Column, name) {
			return true
		}
	}
	return false
}

func slashCreateColumnDefinitions(dialect db.Dialect) []string {
	switch slashDialectOrFallback(dialect).Name() {
	case "postgres":
		return []string{
			"  id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY",
			"  name TEXT",
		}
	case "mysql":
		return []string{
			"  id BIGINT AUTO_INCREMENT PRIMARY KEY",
			"  name VARCHAR(255)",
		}
	default:
		return []string{
			"  id INTEGER PRIMARY KEY",
			"  name TEXT",
		}
	}
}

func replaceEditorCmd(result slashCommandResult) func(context.Context, time.Time) tea.Cmd {
	return func(_ context.Context, _ time.Time) tea.Cmd {
		return func() tea.Msg {
			return slashCommandExecutedMsg{
				Command:       slashCommand{DisplayName: result.Action},
				Result:        result,
				ResultSummary: result.Status,
			}
		}
	}
}

func boolWord(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func buildSlashQueryResult(columns []string, rows [][]string) *db.StatementResult {
	result := &db.ResultSet{Columns: make([]db.ResultColumn, len(columns))}
	for i, column := range columns {
		result.Columns[i] = db.ResultColumn{Name: column, Position: i + 1}
	}
	for rowIndex, row := range rows {
		entry := db.ResultRow{Position: rowIndex + 1, Values: make([]db.ResultValue, len(columns))}
		for i := range columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			entry.Values[i] = db.ResultValue{Kind: db.ValueKindString, Value: value}
		}
		result.Rows = append(result.Rows, entry)
	}

	return &db.StatementResult{
		Kind:      db.StatementResultKindQuery,
		ResultSet: result,
	}
}
