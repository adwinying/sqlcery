package app

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

type recordViewerPendingAction string

const (
	recordViewerPendingActionNone          recordViewerPendingAction = ""
	recordViewerPendingActionComposeInsert recordViewerPendingAction = "compose-insert"
	recordViewerPendingActionComposeUpdate recordViewerPendingAction = "compose-update"
	recordViewerPendingActionComposeDelete recordViewerPendingAction = "compose-delete"
	recordViewerPendingActionWrite         recordViewerPendingAction = "write"
)

type recordViewerComposeAction string

const (
	recordViewerComposeActionInsert recordViewerComposeAction = "INSERT"
	recordViewerComposeActionUpdate recordViewerComposeAction = "UPDATE"
	recordViewerComposeActionDelete recordViewerComposeAction = "DELETE"
)

type recordViewerComposeResult struct {
	SQL             string
	Row             int
	UsedPrimaryKeys bool
	Source          db.TableRef
	Action          recordViewerComposeAction
}

func composeRecordViewerUpdateSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (recordViewerComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return recordViewerComposeResult{}, fmt.Errorf("record viewer has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return recordViewerComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := recordViewerResultSource(latest)
	if !ok {
		return recordViewerComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	assignments := recordViewerUpdateAssignments(dialect, result, result.Rows[rowIndex])
	predicates, usedPrimaryKeys := recordViewerRowPredicates(dialect, result, result.Rows[rowIndex])
	if len(assignments) == 0 {
		return recordViewerComposeResult{}, fmt.Errorf("selected row has no columns to update")
	}
	if !usedPrimaryKeys {
		return recordViewerComposeResult{}, fmt.Errorf("selected row has no primary key columns; cannot compose a safe UPDATE")
	}
	if len(predicates) == 0 {
		return recordViewerComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return recordViewerComposeResult{
		SQL: fmt.Sprintf("UPDATE %s\nSET\n%s\nWHERE\n%s;",
			quoteSlashTableRef(dialect, source),
			strings.Join(assignments, ",\n"),
			strings.Join(predicates, "\n  AND "),
		),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          recordViewerComposeActionUpdate,
	}, nil
}

func composeRecordViewerInsertSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (recordViewerComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return recordViewerComposeResult{}, fmt.Errorf("record viewer has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return recordViewerComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := recordViewerResultSource(latest)
	if !ok {
		return recordViewerComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	columns := recordViewerInsertColumns(dialect, result)
	values := recordViewerInsertValues(dialect, result, result.Rows[rowIndex])
	if len(columns) == 0 {
		return recordViewerComposeResult{}, fmt.Errorf("selected row has no columns to insert")
	}

	return recordViewerComposeResult{
		SQL: fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES (\n%s\n);",
			quoteSlashTableRef(dialect, source),
			strings.Join(columns, ",\n"),
			strings.Join(values, ",\n"),
		),
		Row:    rowIndex,
		Source: source,
		Action: recordViewerComposeActionInsert,
	}, nil
}

func composeRecordViewerDeleteSQL(dialect db.Dialect, latest *LatestResultContext, rowIndex int) (recordViewerComposeResult, error) {
	if latest == nil || latest.PreservedResult == nil {
		return recordViewerComposeResult{}, fmt.Errorf("record viewer has no rows to compose")
	}

	result := latest.PreservedResult
	if rowIndex < 0 || rowIndex >= len(result.Rows) {
		return recordViewerComposeResult{}, fmt.Errorf("selected row is out of range")
	}

	source, ok := recordViewerResultSource(latest)
	if !ok {
		return recordViewerComposeResult{}, fmt.Errorf("result source table is unknown")
	}

	predicates, usedPrimaryKeys := recordViewerRowPredicates(dialect, result, result.Rows[rowIndex])
	if len(predicates) == 0 {
		return recordViewerComposeResult{}, fmt.Errorf("selected row has no identifying predicate")
	}

	return recordViewerComposeResult{
		SQL: fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
			quoteSlashTableRef(dialect, source),
			strings.Join(predicates, "\n  AND "),
		),
		Row:             rowIndex,
		UsedPrimaryKeys: usedPrimaryKeys,
		Source:          source,
		Action:          recordViewerComposeActionDelete,
	}, nil
}

func recordViewerComposeStatus(result recordViewerComposeResult) string {
	if result.Action == recordViewerComposeActionInsert {
		return fmt.Sprintf("Loaded INSERT for row %d from %s into command mode.", result.Row+1, displaySlashTableRef(result.Source))
	}

	predicate := "visible column predicate"
	if result.UsedPrimaryKeys {
		predicate = "primary key predicate"
	}
	action := string(result.Action)
	if action == "" {
		action = string(recordViewerComposeActionUpdate)
	}
	return fmt.Sprintf("Loaded %s for row %d from %s into command mode using %s.", action, result.Row+1, displaySlashTableRef(result.Source), predicate)
}

func recordViewerInsertColumns(dialect db.Dialect, result *db.ResultSet) []string {
	columns := make([]string, 0, len(result.Columns))
	for i := range result.Columns {
		columns = append(columns, "  "+quoteSlashIdentifier(dialect, recordViewerColumnName(result.Columns, i)))
	}
	return columns
}

func recordViewerInsertValues(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) []string {
	values := make([]string, 0, len(result.Columns))
	for i := range result.Columns {
		values = append(values, "  "+recordViewerValueLiteral(dialect, recordViewerRowValue(row, i)))
	}
	return values
}

func recordViewerResultSource(latest *LatestResultContext) (db.TableRef, bool) {
	if latest == nil || latest.PreservedResult == nil {
		return db.TableRef{}, false
	}

	if latest.PreservedResult.Source != nil && strings.TrimSpace(latest.PreservedResult.Source.Name) != "" {
		return *latest.PreservedResult.Source, true
	}

	inferred := inferQuerySourceTable(latest.Query)
	if inferred == nil || strings.TrimSpace(inferred.Name) == "" {
		return db.TableRef{}, false
	}
	return *inferred, true
}

func recordViewerUpdateAssignments(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) []string {
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
			quoteSlashIdentifier(dialect, recordViewerColumnName(result.Columns, index)),
			recordViewerValueLiteral(dialect, recordViewerRowValue(row, index)),
		))
	}
	return assignments
}

func recordViewerRowPredicates(dialect db.Dialect, result *db.ResultSet, row db.ResultRow) ([]string, bool) {
	primaryKeyIndices := make([]int, 0, len(result.Columns))
	for i, column := range result.Columns {
		if column.PrimaryKey != nil {
			primaryKeyIndices = append(primaryKeyIndices, i)
		}
	}
	if len(primaryKeyIndices) > 1 {
		sortRecordViewerPredicateIndices(result.Columns, primaryKeyIndices)
	}
	if len(primaryKeyIndices) > 0 {
		return recordViewerPredicateLines(dialect, result, row, primaryKeyIndices), true
	}

	indices := make([]int, len(result.Columns))
	for i := range result.Columns {
		indices[i] = i
	}
	return recordViewerPredicateLines(dialect, result, row, indices), false
}

func sortRecordViewerPredicateIndices(columns []db.ResultColumn, indices []int) {
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

func recordViewerPredicateLines(dialect db.Dialect, result *db.ResultSet, row db.ResultRow, indices []int) []string {
	predicates := make([]string, 0, len(indices))
	for _, index := range indices {
		column := quoteSlashIdentifier(dialect, recordViewerColumnName(result.Columns, index))
		value := recordViewerRowValue(row, index)
		if value.Kind == db.ValueKindNull || value.Value == nil {
			predicates = append(predicates, fmt.Sprintf("  %s IS NULL", column))
			continue
		}
		predicates = append(predicates, fmt.Sprintf("  %s = %s", column, recordViewerValueLiteral(dialect, value)))
	}
	return predicates
}

func recordViewerColumnName(columns []db.ResultColumn, index int) string {
	if index >= 0 && index < len(columns) && strings.TrimSpace(columns[index].Name) != "" {
		return columns[index].Name
	}
	return fmt.Sprintf("column_%d", index+1)
}

func recordViewerRowValue(row db.ResultRow, index int) db.ResultValue {
	if index >= 0 && index < len(row.Values) {
		return row.Values[index]
	}
	return db.ResultValue{Kind: db.ValueKindNull}
}

func recordViewerValueLiteral(dialect db.Dialect, value db.ResultValue) string {
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
		return recordViewerStringLiteral(fmt.Sprint(value.Value))
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return recordViewerBytesLiteral(dialect, typed)
		}
	case db.ValueKindTime:
		t, state := extractTimeValue(value.Value)
		switch state {
		case timeValueValid:
			return recordViewerTimeLiteral(t)
		case timeValueNull:
			return "NULL"
		}
		if s, ok := value.Value.(string); ok {
			if t, ok := parseTimestampLiteral(s); ok {
				return recordViewerTimeLiteral(t)
			}
			return recordViewerStringLiteral(s)
		}
	}

	if value.Value == nil {
		return "NULL"
	}
	t, state := extractTimeValue(value.Value)
	switch state {
	case timeValueValid:
		return recordViewerTimeLiteral(t)
	case timeValueNull:
		return "NULL"
	}
	return recordViewerStringLiteral(fmt.Sprint(value.Value))
}

// recordViewerTimeLiteral renders a time.Time as a SQL string literal using a
// space-separated ISO-8601 form with an explicit numeric timezone offset. The
// resulting literal round-trips across PostgreSQL, MySQL (8.0.19+) and SQLite.
func recordViewerTimeLiteral(t time.Time) string {
	return recordViewerStringLiteral(t.Format("2006-01-02 15:04:05.999999999-07:00"))
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

var recordViewerTimestampParseLayouts = []string{
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
	for _, layout := range recordViewerTimestampParseLayouts {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func recordViewerStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func recordViewerBytesLiteral(dialect db.Dialect, value []byte) string {
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
		ref.Schema = parts[0]
		ref.Name = parts[1]
	default:
		ref.Catalog = parts[len(parts)-3]
		ref.Schema = parts[len(parts)-2]
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


