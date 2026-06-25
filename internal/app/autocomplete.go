package app

import (
	"sort"
	"strings"
	"unicode"

	"github.com/adwinying/sqlcery/internal/sql"
)

const autocompleteLimit = 6

var autocompleteStrategyDecision = sqlAssistDecisionFor(sqlAssistSurfaceAutocomplete)

type autocompleteItem struct {
	Label      string
	InsertText string
	Kind       string
	Detail     string
}

type autocompleteContext struct {
	Prefix       string
	ReplaceStart int
	ReplaceEnd   int
	Qualifier    string
	SlashMode    bool
	Scope        autocompleteScope
	ActiveTables []string
}

type autocompleteScope string

const (
	autocompleteScopeUnknown           autocompleteScope = "unknown"
	autocompleteScopeStatementStart    autocompleteScope = "statement-start"
	autocompleteScopeSelectList        autocompleteScope = "select-list"
	autocompleteScopeTableRef          autocompleteScope = "table-ref"
	autocompleteScopeWhereExpr         autocompleteScope = "where-expr"
	autocompleteScopeJoinCondition     autocompleteScope = "join-condition"
	autocompleteScopeSetList           autocompleteScope = "set-list"
	autocompleteScopeHavingExpr        autocompleteScope = "having-expr"
	autocompleteScopeGroupBy           autocompleteScope = "group-by"
	autocompleteScopeOrderBy           autocompleteScope = "order-by"
	autocompleteScopeReturning         autocompleteScope = "returning"
	autocompleteScopeAfterTable        autocompleteScope = "after-table"
	autocompleteScopeAfterUpdateTarget autocompleteScope = "after-update-target"
	autocompleteScopeAfterInsertTarget autocompleteScope = "after-insert-target"
	autocompleteScopeInsertStatement   autocompleteScope = "insert-statement"
	autocompleteScopeDeleteStatement   autocompleteScope = "delete-statement"
	autocompleteScopeCreateStatement   autocompleteScope = "create-statement"
	autocompleteScopeDropStatement     autocompleteScope = "drop-statement"
)

type autocompleteCatalog struct {
	Tables          []autocompleteTable
	FallbackColumns []string
}

type autocompleteTable struct {
	Namespace   string
	Name        string
	Columns     []string
	ColumnTypes map[string]string // column name (lowercase) -> type
}

var slashCommandList = defaultSlashCommandRegistry.Names()

func buildAutocompleteItems(value string, cursor int, schema *AutocompleteSchemaContext, latestResult *LatestResultContext) []autocompleteItem {
	ctx := analyzeAutocompleteContext(value, cursor)
	catalog := buildAutocompleteCatalog(schema, latestResult)
	if ctx.Prefix == "" && ctx.Qualifier == "" && !ctx.SlashMode && ctx.Scope == autocompleteScopeUnknown {
		return nil
	}
	runes := []rune(value)
	if ctx.Prefix == "" && ctx.Qualifier == "" && !ctx.SlashMode && strings.TrimSpace(string(runes[:clampCursorOffset(cursor, len(runes))])) == "" {
		return nil
	}
	if ctx.Prefix == "" && !ctx.SlashMode {
		trimmed := strings.TrimRight(string(runes[:clampCursorOffset(ctx.ReplaceStart, len(runes))]), " \t\n\r")
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] == ';' {
			return nil
		}
	}

	items := make([]autocompleteItem, 0, autocompleteLimit)
	seen := map[string]struct{}{}

	if ctx.SlashMode {
		for _, command := range slashCommandList {
			if !matchesAutocompletePrefix(command, ctx.Prefix) {
				continue
			}
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      command,
				InsertText: command,
				Kind:       "cmd",
				Detail:     "slash command",
			})
		}
		return items
	}

	if ctx.Qualifier != "" {
		for _, column := range catalog.columnsForQualifier(ctx.Qualifier) {
			if !matchesAutocompletePrefix(column, ctx.Prefix) {
				continue
			}
			colType := catalog.columnType(column, []string{ctx.Qualifier})
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      column,
				InsertText: column,
				Kind:       "col",
				Detail:     colType,
			})
		}
		return finalizeAutocompleteItems(items, ctx, catalog)
	}

	if wantsTableSuggestions(ctx) {
		for _, table := range catalog.Tables {
			label := table.displayName()
			if !matchesAutocompletePrefix(label, ctx.Prefix) && !matchesAutocompletePrefix(table.Name, ctx.Prefix) {
				continue
			}
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      label,
				InsertText: label,
				Kind:       "tbl",
			})
		}
	}

	if wantsColumnSuggestions(ctx) {
		for _, column := range catalog.columnSuggestions(ctx.ActiveTables) {
			if !matchesAutocompletePrefix(column, ctx.Prefix) {
				continue
			}
			colType := catalog.columnType(column, ctx.ActiveTables)
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      column,
				InsertText: column,
				Kind:       "col",
				Detail:     colType,
			})
		}
	}

	if wantsKeywordSuggestions(ctx) {
		for _, keyword := range sql.Keywords {
			if !matchesAutocompletePrefix(keyword, ctx.Prefix) {
				continue
			}
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      keyword,
				InsertText: keyword,
				Kind:       "kwd",
			})
		}
	}

	if wantsGeneralTableSuggestions(ctx) {
		for _, table := range catalog.Tables {
			label := table.displayName()
			if !matchesAutocompletePrefix(label, ctx.Prefix) && !matchesAutocompletePrefix(table.Name, ctx.Prefix) {
				continue
			}
			items = appendAutocompleteItem(items, seen, autocompleteItem{
				Label:      label,
				InsertText: label,
				Kind:       "tbl",
			})
		}
	}

	return finalizeAutocompleteItems(items, ctx, catalog)
}

func appendAutocompleteItem(items []autocompleteItem, seen map[string]struct{}, item autocompleteItem) []autocompleteItem {
	key := strings.ToLower(item.Kind + ":" + item.Label)
	if _, ok := seen[key]; ok {
		return items
	}

	seen[key] = struct{}{}
	return append(items, item)
}

func finalizeAutocompleteItems(items []autocompleteItem, ctx autocompleteContext, catalog autocompleteCatalog) []autocompleteItem {
	sort.SliceStable(items, func(i, j int) bool {
		left := autocompleteSortKey(items[i], ctx, catalog)
		right := autocompleteSortKey(items[j], ctx, catalog)
		for idx := range left {
			if left[idx] != right[idx] {
				return left[idx] < right[idx]
			}
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})

	if len(items) > autocompleteLimit {
		items = items[:autocompleteLimit]
	}

	return items
}

func matchesAutocompletePrefix(candidate, prefix string) bool {
	if prefix == "" {
		return true
	}
	_, ok := fuzzyMatch(prefix, candidate)
	return ok
}

func wantsTableSuggestions(ctx autocompleteContext) bool {
	return ctx.Scope == autocompleteScopeTableRef
}

func wantsColumnSuggestions(ctx autocompleteContext) bool {
	switch ctx.Scope {
	case autocompleteScopeSelectList, autocompleteScopeWhereExpr, autocompleteScopeJoinCondition,
		autocompleteScopeSetList, autocompleteScopeHavingExpr, autocompleteScopeGroupBy,
		autocompleteScopeOrderBy, autocompleteScopeReturning:
		return true
	default:
		return false
	}
}

func wantsGeneralTableSuggestions(ctx autocompleteContext) bool {
	if ctx.Prefix == "" {
		return false
	}

	switch ctx.Scope {
	case autocompleteScopeUnknown, autocompleteScopeStatementStart:
		return true
	default:
		return false
	}
}

func wantsKeywordSuggestions(ctx autocompleteContext) bool {
	return !ctx.SlashMode && ctx.Scope != autocompleteScopeTableRef
}

func analyzeAutocompleteContext(value string, cursor int) autocompleteContext {
	runes := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}

	lineStart := cursor
	for lineStart > 0 && runes[lineStart-1] != '\n' {
		lineStart--
	}

	trimmedLineStart := lineStart
	for trimmedLineStart < cursor && unicode.IsSpace(runes[trimmedLineStart]) {
		trimmedLineStart++
	}

	ctx := autocompleteContext{ReplaceStart: cursor, ReplaceEnd: cursor}

	if trimmedLineStart < cursor && runes[trimmedLineStart] == '/' {
		start := cursor
		for start > trimmedLineStart+1 && isSlashCommandPart(runes[start-1]) {
			start--
		}
		if start > trimmedLineStart && runes[start-1] == '/' {
			start--
		}
		ctx.SlashMode = true
		ctx.ReplaceStart = start
		ctx.Prefix = string(runes[start:cursor])
		return ctx
	}

	start := cursor
	for start > 0 && sql.IsIdentifierPart(runes[start-1]) {
		start--
	}
	ctx.ReplaceStart = start
	ctx.Prefix = string(runes[start:cursor])

	if start > 0 && runes[start-1] == '.' {
		ctx.ReplaceStart = start
		ctx.Qualifier = scanIdentifierBackward(runes, start-1)
	} else if cursor > 0 && runes[cursor-1] == '.' {
		ctx.ReplaceStart = cursor
		ctx.Prefix = ""
		ctx.Qualifier = scanIdentifierBackward(runes, cursor-1)
	}

	// Autocomplete intentionally follows the lightweight SQL assistance decision
	// in sql_assist_strategy.go: scan tokens from the current statement, infer the
	// active clause, and avoid AST-level parsing until parser triggers apply.
	tokens := sqlLex(string(runes[:ctx.ReplaceStart]))
	ctx.Scope = analyzeAutocompleteScope(tokens)
	ctx.ActiveTables = referencedTables(tokens)

	return ctx
}

func scanIdentifierBackward(runes []rune, end int) string {
	start := end
	for start > 0 && sql.IsIdentifierPart(runes[start-1]) {
		start--
	}

	if start == end {
		return ""
	}

	return string(runes[start:end])
}

func isSlashCommandPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}

func analyzeAutocompleteScope(tokens []sqlToken) autocompleteScope {
	tokens = currentStatementTokens(tokens)
	if len(tokens) == 0 {
		return autocompleteScopeStatementStart
	}

	if lastTokenKeyword(tokens, "INSERT") {
		return autocompleteScopeInsertStatement
	}
	if lastTokenKeyword(tokens, "DELETE") {
		return autocompleteScopeDeleteStatement
	}
	if lastTokenKeyword(tokens, "CREATE") {
		return autocompleteScopeCreateStatement
	}
	if lastTokenKeyword(tokens, "DROP") {
		return autocompleteScopeDropStatement
	}

	clause, clauseIndex := lastClause(tokens)
	switch clause {
	case "SELECT":
		return autocompleteScopeSelectList
	case "FROM", "JOIN":
		if clauseHasContent(tokens[clauseIndex+1:]) {
			return autocompleteScopeAfterTable
		}
		return autocompleteScopeTableRef
	case "UPDATE":
		if clauseHasContent(tokens[clauseIndex+1:]) {
			return autocompleteScopeAfterUpdateTarget
		}
		return autocompleteScopeTableRef
	case "INTO":
		if clauseHasContent(tokens[clauseIndex+1:]) {
			return autocompleteScopeAfterInsertTarget
		}
		return autocompleteScopeTableRef
	case "WHERE":
		return autocompleteScopeWhereExpr
	case "ON":
		return autocompleteScopeJoinCondition
	case "SET":
		return autocompleteScopeSetList
	case "HAVING":
		return autocompleteScopeHavingExpr
	case "GROUP BY":
		return autocompleteScopeGroupBy
	case "ORDER BY":
		return autocompleteScopeOrderBy
	case "RETURNING":
		return autocompleteScopeReturning
	default:
		return autocompleteScopeUnknown
	}
}

func currentStatementTokens(tokens []sqlToken) []sqlToken {
	start := 0
	for i, token := range tokens {
		if token.Symbol && token.Text == ";" {
			start = i + 1
		}
	}
	return tokens[start:]
}

func lastTokenKeyword(tokens []sqlToken, want string) bool {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].Symbol && tokens[i].Text == ";" {
			return false
		}
		if !tokens[i].Keyword {
			continue
		}
		return strings.EqualFold(tokens[i].Text, want)
	}
	return false
}

func lastClause(tokens []sqlToken) (string, int) {
	for i := len(tokens) - 1; i >= 0; i-- {
		if !tokens[i].Keyword {
			continue
		}

		keyword := strings.ToUpper(tokens[i].Text)
		if keyword == "BY" && i > 0 && tokens[i-1].Keyword {
			prev := strings.ToUpper(tokens[i-1].Text)
			if prev == "ORDER" || prev == "GROUP" {
				return prev + " BY", i - 1
			}
		}

		switch keyword {
		case "SELECT", "FROM", "JOIN", "WHERE", "ON", "HAVING", "UPDATE", "INTO", "SET", "RETURNING":
			return keyword, i
		}
	}

	return "", -1
}

func clauseHasContent(tokens []sqlToken) bool {
	for _, token := range tokens {
		if token.Ident || token.Keyword {
			return true
		}
	}
	return false
}

func autocompleteSortKey(item autocompleteItem, ctx autocompleteContext, catalog autocompleteCatalog) [3]int {
	return [3]int{
		autocompleteKindRank(item, ctx),
		autocompletePrefixRank(item, ctx),
		autocompleteItemRank(item, ctx, catalog),
	}
}

func autocompleteKindRank(item autocompleteItem, ctx autocompleteContext) int {
	switch ctx.Scope {
	case autocompleteScopeStatementStart, autocompleteScopeAfterTable, autocompleteScopeAfterUpdateTarget,
		autocompleteScopeAfterInsertTarget, autocompleteScopeInsertStatement,
		autocompleteScopeDeleteStatement, autocompleteScopeCreateStatement, autocompleteScopeDropStatement:
		switch item.Kind {
		case "kwd":
			return 0
		case "tbl":
			return 1
		case "col":
			return 2
		default:
			return 3
		}
	case autocompleteScopeSelectList, autocompleteScopeWhereExpr, autocompleteScopeJoinCondition,
		autocompleteScopeSetList, autocompleteScopeHavingExpr, autocompleteScopeGroupBy,
		autocompleteScopeOrderBy, autocompleteScopeReturning:
		switch item.Kind {
		case "col":
			return 0
		case "kwd":
			return 1
		case "tbl":
			return 2
		default:
			return 3
		}
	case autocompleteScopeTableRef:
		switch item.Kind {
		case "tbl":
			return 0
		case "kwd":
			return 1
		case "col":
			return 2
		default:
			return 3
		}
	default:
		switch item.Kind {
		case "tbl":
			return 0
		case "col":
			return 1
		case "kwd":
			return 2
		default:
			return 3
		}
	}
}

func autocompleteItemRank(item autocompleteItem, ctx autocompleteContext, catalog autocompleteCatalog) int {
	switch item.Kind {
	case "kwd":
		return autocompleteKeywordRank(item.Label, ctx.Scope)
	case "col":
		if columnInActiveTables(item.Label, ctx, catalog) {
			return 0
		}
		return 1
	case "tbl":
		if strings.Contains(item.Label, ".") && matchesAutocompletePrefix(unqualifiedAutocompleteLabel(item.Label), ctx.Prefix) {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func autocompleteKeywordRank(keyword string, scope autocompleteScope) int {
	priority := []string(nil)
	switch scope {
	case autocompleteScopeStatementStart:
		priority = []string{"SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP"}
	case autocompleteScopeSelectList:
		priority = []string{"DISTINCT", "ALL", "CASE"}
	case autocompleteScopeWhereExpr, autocompleteScopeJoinCondition, autocompleteScopeHavingExpr:
		priority = []string{"AND", "OR", "NOT", "IN", "IS", "LIKE", "BETWEEN", "EXISTS", "CASE", "NULL", "TRUE", "FALSE"}
	case autocompleteScopeSetList:
		priority = []string{"CASE", "NULL", "TRUE", "FALSE"}
	case autocompleteScopeOrderBy:
		priority = []string{"ASC", "DESC"}
	case autocompleteScopeAfterTable:
		priority = []string{"JOIN", "WHERE", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "GROUP", "ORDER", "LIMIT", "OFFSET", "UNION"}
	case autocompleteScopeAfterUpdateTarget:
		priority = []string{"SET", "WHERE", "RETURNING"}
	case autocompleteScopeAfterInsertTarget:
		priority = []string{"VALUES", "SELECT", "RETURNING"}
	case autocompleteScopeInsertStatement:
		priority = []string{"INTO"}
	case autocompleteScopeDeleteStatement:
		priority = []string{"FROM"}
	case autocompleteScopeCreateStatement, autocompleteScopeDropStatement:
		priority = []string{"TABLE", "VIEW"}
	case autocompleteScopeReturning:
		priority = []string{"CASE", "NULL", "TRUE", "FALSE"}
	}

	upper := strings.ToUpper(keyword)
	for i, candidate := range priority {
		if candidate == upper {
			return i
		}
	}

	return len(priority) + 1
}

func autocompletePrefixRank(item autocompleteItem, ctx autocompleteContext) int {
	if ctx.Prefix == "" {
		return 0
	}
	score, _ := fuzzyMatch(ctx.Prefix, item.Label)
	if item.Kind == "tbl" {
		if s, ok := fuzzyMatch(ctx.Prefix, unqualifiedAutocompleteLabel(item.Label)); ok && s > score {
			score = s
		}
	}
	return -score
}

func columnInActiveTables(column string, ctx autocompleteContext, catalog autocompleteCatalog) bool {
	for _, tableName := range ctx.ActiveTables {
		for _, candidate := range catalog.columnsForQualifier(tableName) {
			if strings.EqualFold(candidate, column) {
				return true
			}
		}
	}
	return false
}

func unqualifiedAutocompleteLabel(label string) string {
	if idx := strings.LastIndex(label, "."); idx >= 0 {
		return label[idx+1:]
	}
	return label
}

func referencedTables(tokens []sqlToken) []string {
	results := make([]string, 0, 4)
	seen := map[string]struct{}{}

	for i := 0; i < len(tokens); i++ {
		if !tokens[i].Keyword {
			continue
		}

		keyword := strings.ToUpper(tokens[i].Text)
		if keyword != "FROM" && keyword != "JOIN" && keyword != "UPDATE" && keyword != "INTO" {
			continue
		}

		name, next := parseTableReference(tokens, i+1)
		i = next - 1
		if name == "" {
			continue
		}

		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, name)
	}

	return results
}

func parseTableReference(tokens []sqlToken, start int) (string, int) {
	parts, next := scanDottedIdentifier(tokens, start)
	return strings.Join(parts, "."), next
}

func buildAutocompleteCatalog(schema *AutocompleteSchemaContext, result *LatestResultContext) autocompleteCatalog {
	catalog := autocompleteCatalog{}

	if schema != nil {
		for _, table := range schema.Tables {
			entry := autocompleteTable{
				Namespace:   table.Namespace,
				Name:        table.Name,
				Columns:     append([]string(nil), table.Columns...),
				ColumnTypes: table.ColumnTypes,
			}
			catalog.Tables = append(catalog.Tables, entry)
		}
	}

	if latest := result; latest != nil && latest.PreservedResult != nil {
		for _, column := range latest.PreservedResult.Columns {
			if strings.TrimSpace(column.Name) != "" {
				catalog.FallbackColumns = appendUniqueFold(catalog.FallbackColumns, column.Name)
			}
		}

		if latest.PreservedResult.Source != nil && strings.TrimSpace(latest.PreservedResult.Source.Name) != "" {
			table := autocompleteTable{
				Namespace: latest.PreservedResult.Source.Namespace,
				Name:      latest.PreservedResult.Source.Name,
			}
			for _, column := range latest.PreservedResult.Columns {
				if strings.TrimSpace(column.Name) != "" {
					table.Columns = appendUniqueFold(table.Columns, column.Name)
				}
			}
			catalog.mergeTable(table)
		}
	}

	return catalog
}

func (c *autocompleteCatalog) mergeTable(table autocompleteTable) {
	for i := range c.Tables {
		if !sameAutocompleteTable(c.Tables[i], table) {
			continue
		}
		for _, column := range table.Columns {
			c.Tables[i].Columns = appendUniqueFold(c.Tables[i].Columns, column)
		}
		return
	}

	c.Tables = append(c.Tables, table)
}

func sameAutocompleteTable(left, right autocompleteTable) bool {
	return strings.EqualFold(left.Namespace, right.Namespace) && strings.EqualFold(left.Name, right.Name)
}

func (t autocompleteTable) displayName() string {
	if strings.TrimSpace(t.Namespace) == "" {
		return t.Name
	}

	return t.Namespace + "." + t.Name
}

func (c autocompleteCatalog) columnsForQualifier(qualifier string) []string {
	results := make([]string, 0, 8)
	for _, table := range c.Tables {
		if strings.EqualFold(table.Name, qualifier) || strings.EqualFold(table.displayName(), qualifier) {
			for _, column := range table.Columns {
				results = appendUniqueFold(results, column)
			}
		}
	}

	if len(results) > 0 {
		return results
	}

	return append([]string(nil), c.FallbackColumns...)
}

func (c autocompleteCatalog) columnSuggestions(activeTables []string) []string {
	results := make([]string, 0, 16)
	for _, tableName := range activeTables {
		for _, column := range c.columnsForQualifier(tableName) {
			results = appendUniqueFold(results, column)
		}
	}

	for _, column := range c.FallbackColumns {
		results = appendUniqueFold(results, column)
	}

	return results
}

func (c autocompleteCatalog) columnType(column string, activeTables []string) string {
	for _, tableName := range activeTables {
		for _, table := range c.Tables {
			if !strings.EqualFold(table.Name, tableName) && !strings.EqualFold(table.displayName(), tableName) {
				continue
			}
			if t, ok := table.ColumnTypes[strings.ToLower(column)]; ok && t != "" {
				return t
			}
		}
	}
	// search all tables
	for _, table := range c.Tables {
		if t, ok := table.ColumnTypes[strings.ToLower(column)]; ok && t != "" {
			return t
		}
	}
	return ""
}

func appendUniqueFold(values []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return values
	}

	for _, existing := range values {
		if strings.EqualFold(existing, trimmed) {
			return values
		}
	}

	return append(values, trimmed)
}
