package app

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

type resultsPanePendingAction string

const (
	resultsPanePendingActionNone          resultsPanePendingAction = ""
	resultsPanePendingActionComposeInsert resultsPanePendingAction = "compose-insert"
	resultsPanePendingActionComposeUpdate resultsPanePendingAction = "compose-update"
	resultsPanePendingActionComposeDelete resultsPanePendingAction = "compose-delete"
)

type resultsPaneComposeAction string

const (
	resultsPaneComposeActionInsert resultsPaneComposeAction = "INSERT"
	resultsPaneComposeActionUpdate resultsPaneComposeAction = "UPDATE"
	resultsPaneComposeActionDelete resultsPaneComposeAction = "DELETE"
)

type resultsPaneComposeResult struct {
	SQL             string
	Row             int
	UsedPrimaryKeys bool
	Source          db.TableRef
	Action          resultsPaneComposeAction
}

func composeResultsPaneUpdateSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	assignments := resultsPaneUpdateAssignments(dialect, result, result.Rows[rowIndex])
	predicates, usedPrimaryKeys := resultsPaneRowPredicates(dialect, result, result.Rows[rowIndex])
	if len(assignments) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no columns to update")
	}
	if !usedPrimaryKeys {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no primary key columns; cannot compose a safe UPDATE")
	}
	if len(predicates) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return resultsPaneComposeResult{
		SQL: fmt.Sprintf("UPDATE %s\nSET\n%s\nWHERE\n%s;",
			quoteSlashTableRef(dialect, source),
			strings.Join(assignments, ",\n"),
			strings.Join(predicates, "\n  AND "),
		),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          resultsPaneComposeActionUpdate,
	}, nil
}

func composeResultsPaneInsertSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	columns := resultsPaneInsertColumns(dialect, result)
	values := resultsPaneInsertValues(dialect, result, result.Rows[rowIndex])
	if len(columns) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no columns to insert")
	}

	return resultsPaneComposeResult{
		SQL: fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES (\n%s\n);",
			quoteSlashTableRef(dialect, source),
			strings.Join(columns, ",\n"),
			strings.Join(values, ",\n"),
		),
		Row:    rowIndex,
		Source: source,
		Action: resultsPaneComposeActionInsert,
	}, nil
}

func composeResultsPaneDeleteSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (resultsPaneComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	predicates, usedPrimaryKeys := resultsPaneRowPredicates(dialect, result, result.Rows[rowIndex])
	if len(predicates) == 0 {
		return resultsPaneComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return resultsPaneComposeResult{
		SQL: fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
			quoteSlashTableRef(dialect, source),
			strings.Join(predicates, "\n  AND "),
		),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          resultsPaneComposeActionDelete,
	}, nil
}

type resultsPaneComposeBulkResult struct {
	SQL    string
	Count  int
	Source db.TableRef
	Action resultsPaneComposeAction
}

func sortedRowIndices(rows []int) []int {
	sorted := append([]int(nil), rows...)
	sort.Ints(sorted)
	return sorted
}

func composeResultsPaneInsertBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("result source table is unknown")
	}

	columns := resultsPaneInsertColumns(dialect, result)
	if len(columns) == 0 {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("rows have no columns to insert")
	}

	indices := sortedRowIndices(rowIndices)
	tuples := make([]string, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(result.Rows) {
			return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
		}
		vals := resultsPaneInsertValues(dialect, result, result.Rows[idx])
		tuples = append(tuples, "  ("+strings.Join(trimLeadingSpaces(vals), ", ")+")")
	}

	return resultsPaneComposeBulkResult{
		SQL: fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES\n%s;",
			quoteSlashTableRef(dialect, source),
			strings.Join(columns, ",\n"),
			strings.Join(tuples, ",\n"),
		),
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionInsert,
	}, nil
}

func composeResultsPaneUpdateBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	indices := sortedRowIndices(rowIndices)
	stmts := make([]string, 0, len(indices))
	var source db.TableRef
	for _, idx := range indices {
		r, err := composeResultsPaneUpdateSQL(dialect, latest, idx)
		if err != nil {
			return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d: %w", idx+1, err)
		}
		stmts = append(stmts, r.SQL)
		source = r.Source
	}

	return resultsPaneComposeBulkResult{
		SQL:    strings.Join(stmts, "\n"),
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionUpdate,
	}, nil
}

func composeResultsPaneDeleteBulkSQL(dialect db.Dialect, latest *LatestResultContext, rowIndices []int) (resultsPaneComposeBulkResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("Results Pane has no rows to compose")
	}

	result := latest.PreservedResult
	source, ok := resultsPaneResultSource(latest)
	if !ok {
		return resultsPaneComposeBulkResult{}, fmt.Errorf("result source table is unknown")
	}

	pkIndices := make([]int, 0)
	for i, col := range result.Columns {
		if col.PrimaryKey != nil {
			pkIndices = append(pkIndices, i)
		}
	}
	if len(pkIndices) > 1 {
		sortResultsPanePredicateIndices(result.Columns, pkIndices)
	}

	indices := sortedRowIndices(rowIndices)

	var whereClause string
	if len(pkIndices) == 1 {
		pkCol := quoteSlashIdentifier(dialect, resultsPaneColumnName(result.Columns, pkIndices[0]))
		vals := make([]string, 0, len(indices))
		for _, idx := range indices {
			if idx < 0 || idx >= len(result.Rows) {
				return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
			}
			vals = append(vals, resultsPaneValueLiteral(dialect, resultsPaneRowValue(result.Rows[idx], pkIndices[0])))
		}
		whereClause = fmt.Sprintf("  %s IN (%s)", pkCol, strings.Join(vals, ", "))
	} else {
		rowClauses := make([]string, 0, len(indices))
		for _, idx := range indices {
			if idx < 0 || idx >= len(result.Rows) {
				return resultsPaneComposeBulkResult{}, fmt.Errorf("row %d is out of range", idx+1)
			}
			predicates, _ := resultsPaneRowPredicates(dialect, result, result.Rows[idx])
			rowClauses = append(rowClauses, "("+strings.Join(trimLeadingSpaces(predicates), " AND ")+")")
		}
		whereClause = "  " + strings.Join(rowClauses, "\n  OR ")
	}

	return resultsPaneComposeBulkResult{
		SQL: fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
			quoteSlashTableRef(dialect, source),
			whereClause,
		),
		Count:  len(indices),
		Source: source,
		Action: resultsPaneComposeActionDelete,
	}, nil
}

func trimLeadingSpaces(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.TrimLeft(s, " \t")
	}
	return out
}

func resultsPaneComposeBulkStatus(result resultsPaneComposeBulkResult) string {
	return fmt.Sprintf("Loaded %s for %d rows from %s into command mode.", result.Action, result.Count, displaySlashTableRef(result.Source))
}

func resultsPaneComposeStatus(result resultsPaneComposeResult) string {
	if result.Action == resultsPaneComposeActionInsert {
		return fmt.Sprintf("Loaded INSERT for row %d from %s into command mode.", result.Row+1, displaySlashTableRef(result.Source))
	}

	predicate := "visible column predicate"
	if result.UsedPrimaryKeys {
		predicate = "primary key predicate"
	}
	action := string(result.Action)
	if action == "" {
		action = string(resultsPaneComposeActionUpdate)
	}
	return fmt.Sprintf("Loaded %s for row %d from %s into command mode using %s.", action, result.Row+1, displaySlashTableRef(result.Source), predicate)
}

func resultsPaneInsertColumns(dialect db.Dialect, result *db.ResultSet) []string {
	columns := make([]string, 0, len(result.Columns))
	for i := range result.Columns {
		columns = append(columns, "  "+quoteSlashIdentifier(dialect, resultsPaneColumnName(result.Columns, i)))
	}
	return columns
}

func resultsPaneInsertValues(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) []string {
	values := make([]string, 0, len(result.Columns))
	for i := range result.Columns {
		values = append(values, "  "+resultsPaneValueLiteral(dialect, resultsPaneRowValue(row, i)))
	}
	return values
}

func resultsPaneResultSource(latest *LatestResultContext) (db.TableRef, bool) {
	if latest == nil || latest.PreservedResult == nil {
		return db.TableRef{}, false
	}

	if latest.PreservedResult.Source != nil && strings.TrimSpace(latest.PreservedResult.Source.Name) != "" {
		return *latest.PreservedResult.Source, true
	}

	inferred := inferQuerySourceTable(latest.Statement)
	if inferred == nil || strings.TrimSpace(inferred.Name) == "" {
		return db.TableRef{}, false
	}
	return *inferred, true
}

func resultsPaneUpdateAssignments(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) []string {
	indices := make([]int, 0, len(result.Columns))
	for i, column := range result.Columns {
		if column.PrimaryKey != nil {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		indices = make([]int, len(result.Columns))
		for i := range result.Columns {
			indices[i] = i
		}
	}

	assignments := make([]string, 0, len(indices))
	for _, index := range indices {
		assignments = append(assignments, fmt.Sprintf("  %s = %s",
			quoteSlashIdentifier(dialect, resultsPaneColumnName(result.Columns, index)),
			resultsPaneValueLiteral(dialect, resultsPaneRowValue(row, index)),
		))
	}
	return assignments
}

func resultsPaneRowPredicates(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) ([]string, bool) {
	primaryKeyIndices := make([]int, 0, len(result.Columns))
	for i, column := range result.Columns {
		if column.PrimaryKey != nil {
			primaryKeyIndices = append(primaryKeyIndices, i)
		}
	}
	if len(primaryKeyIndices) > 1 {
		sortResultsPanePredicateIndices(result.Columns, primaryKeyIndices)
	}
	if len(primaryKeyIndices) > 0 {
		return resultsPanePredicateLines(dialect, result, row, primaryKeyIndices), true
	}

	indices := make([]int, len(result.Columns))
	for i := range result.Columns {
		indices[i] = i
	}
	return resultsPanePredicateLines(dialect, result, row, indices), false
}

func sortResultsPanePredicateIndices(columns []db.ResultColumn, indices []int) {
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			left := columns[indices[i]].PrimaryKey
			right := columns[indices[j]].PrimaryKey
			if left == nil || right == nil {
				continue
			}
			if left.Position <= right.Position {
				continue
			}
			indices[i], indices[j] = indices[j], indices[i]
		}
	}
}

func resultsPanePredicateLines(dialect db.Dialect, result *db.ResultSet, row db.ResultRow, indices []int) []string {
	predicates := make([]string, 0, len(indices))
	for _, index := range indices {
		column := quoteSlashIdentifier(dialect, resultsPaneColumnName(result.Columns, index))
		value := resultsPaneRowValue(row, index)
		if value.Kind == db.ValueKindNull || value.Value == nil {
			predicates = append(predicates, fmt.Sprintf("  %s IS NULL", column))
			continue
		}
		predicates = append(predicates, fmt.Sprintf("  %s = %s", column, resultsPaneValueLiteral(dialect, value)))
	}
	return predicates
}

func resultsPaneColumnName(columns []db.ResultColumn, index int) string {
	if index >= 0 && index < len(columns) && strings.TrimSpace(columns[index].Name) != "" {
		return columns[index].Name
	}
	return fmt.Sprintf("column_%d", index+1)
}

func resultsPaneRowValue(row db.ResultRow, index int) db.ResultValue {
	if index >= 0 && index < len(row.Values) {
		return row.Values[index]
	}
	return db.ResultValue{Kind: db.ValueKindNull}
}

func resultsPaneValueLiteral(dialect db.Dialect, value db.ResultValue) string {
	switch value.Kind {
	case db.ValueKindNull:
		return "NULL"
	case db.ValueKindBool:
		if typed, ok := value.Value.(bool); ok {
			if typed {
				return "TRUE"
			}
			return "FALSE"
		}
	case db.ValueKindInteger, db.ValueKindFloat, db.ValueKindDecimal:
		return fmt.Sprint(value.Value)
	case db.ValueKindString:
		return resultsPaneStringLiteral(fmt.Sprint(value.Value))
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return resultsPaneBytesLiteral(dialect, typed)
		}
	case db.ValueKindTime:
		t, state := extractTimeValue(value.Value)
		switch state {
		case timeValueValid:
			return resultsPaneTimeLiteral(t)
		case timeValueNull:
			return "NULL"
		}
		if s, ok := value.Value.(string); ok {
			if t, ok := parseTimestampLiteral(s); ok {
				return resultsPaneTimeLiteral(t)
			}
			return resultsPaneStringLiteral(s)
		}
	}

	if value.Value == nil {
		return "NULL"
	}
	t, state := extractTimeValue(value.Value)
	switch state {
	case timeValueValid:
		return resultsPaneTimeLiteral(t)
	case timeValueNull:
		return "NULL"
	}
	return resultsPaneStringLiteral(fmt.Sprint(value.Value))
}

// resultsPaneTimeLiteral renders a time.Time as a SQL string literal using a
// space-separated ISO-8601 form with an explicit numeric timezone offset. The
// resulting literal round-trips across PostgreSQL, MySQL (8.0.19+) and SQLite.
func resultsPaneTimeLiteral(t time.Time) string {
	return resultsPaneStringLiteral(t.Format("2006-01-02 15:04:05.999999999-07:00"))
}

type timeValueState int

const (
	// timeValueUnknown means the value was not recognised as a timestamp type
	// and should be handled by the generic fallback.
	timeValueUnknown timeValueState = iota
	// timeValueValid means a time.Time was successfully extracted.
	timeValueValid
	// timeValueNull means the value was recognised as a timestamp type whose
	// payload represents SQL NULL (e.g. sql.NullTime{Valid: false}).
	timeValueNull
)

// extractTimeValue unwraps a driver-specific timestamp value into a time.Time.
// It understands time.Time, *time.Time, sql.NullTime, and pgtype-style structs
// exposing Time (time.Time) and optional Valid (bool) fields such as
// pgtype.Timestamp and pgtype.Timestamptz.
func extractTimeValue(value any) (time.Time, timeValueState) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, timeValueUnknown
	case time.Time:
		return v, timeValueValid
	case *time.Time:
		if v == nil {
			return time.Time{}, timeValueNull
		}
		return *v, timeValueValid
	case sql.NullTime:
		if !v.Valid {
			return time.Time{}, timeValueNull
		}
		return v.Time, timeValueValid
	}

	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return time.Time{}, timeValueNull
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return time.Time{}, timeValueUnknown
	}
	timeField := rv.FieldByName("Time")
	if !timeField.IsValid() {
		return time.Time{}, timeValueUnknown
	}
	if _, ok := timeField.Interface().(time.Time); !ok {
		return time.Time{}, timeValueUnknown
	}
	if validField := rv.FieldByName("Valid"); validField.IsValid() && validField.Kind() == reflect.Bool && !validField.Bool() {
		return time.Time{}, timeValueNull
	}
	return timeField.Interface().(time.Time), timeValueValid
}

var resultsPaneTimestampParseLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999-0700",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseTimestampLiteral best-effort parses a textual timestamp coming from the
// driver into a time.Time so it can be reformatted into a canonical SQL
// literal.
func parseTimestampLiteral(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range resultsPaneTimestampParseLayouts {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func resultsPaneStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func resultsPaneBytesLiteral(dialect db.Dialect, value []byte) string {
	hex := fmt.Sprintf("%x", value)
	if slashDialectOrFallback(dialect).Name() == "postgres" {
		return fmt.Sprintf("decode('%s', 'hex')", hex)
	}
	return fmt.Sprintf("X'%s'", hex)
}

func inferQuerySourceTable(query string) *db.TableRef {
	tokens := currentQuerySourceTokens(sqlLex(query))
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) > 0 && tokens[len(tokens)-1].Symbol && tokens[len(tokens)-1].Text == ";" {
		tokens = tokens[:len(tokens)-1]
	}

	depth := 0
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Symbol {
			switch tokens[i].Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if depth != 0 || !tokens[i].Keyword || !strings.EqualFold(tokens[i].Text, "FROM") {
			continue
		}

		ref, next, ok := parseQuerySourceTableReference(tokens, i+1)
		if !ok {
			return nil
		}
		if querySourceHasAdditionalTables(tokens, next) {
			return nil
		}
		return &ref
	}

	return nil
}

func currentQuerySourceTokens(tokens []sqlToken) []sqlToken {
	end := len(tokens)
	for end > 0 && tokens[end-1].Symbol && tokens[end-1].Text == ";" {
		end--
	}

	start := 0
	for i := 0; i < end; i++ {
		token := tokens[i]
		if token.Symbol && token.Text == ";" {
			start = i + 1
		}
	}
	return tokens[start:end]
}

func parseQuerySourceTableReference(tokens []sqlToken, start int) (db.TableRef, int, bool) {
	parts := make([]string, 0, 3)
	i := start
	for i < len(tokens) {
		if !tokens[i].Ident {
			break
		}
		parts = append(parts, tokens[i].Text)
		i++
		if i >= len(tokens) || !tokens[i].Symbol || tokens[i].Text != "." {
			break
		}
		i++
	}

	if len(parts) == 0 {
		return db.TableRef{}, start, false
	}

	ref := db.TableRef{}
	switch len(parts) {
	case 1:
		ref.Name = parts[0]
	case 2:
		ref.Namespace = parts[0]
		ref.Name = parts[1]
	default:
		ref.Catalog = parts[len(parts)-3]
		ref.Namespace = parts[len(parts)-2]
		ref.Name = parts[len(parts)-1]
	}
	return ref, i, true
}

func querySourceHasAdditionalTables(tokens []sqlToken, start int) bool {
	depth := 0
	for i := start; i < len(tokens); i++ {
		token := tokens[i]
		if token.Symbol {
			switch token.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					return true
				}
			case ";":
				if depth == 0 {
					return false
				}
			}
			continue
		}
		if depth != 0 || !token.Keyword {
			continue
		}

		switch strings.ToUpper(token.Text) {
		case "JOIN":
			return true
		case "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "RETURNING", "UNION", "EXCEPT", "INTERSECT", "WINDOW":
			return false
		}
	}
	return false
}


