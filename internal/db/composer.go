package db

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// ColumnValue pairs a column name with the value bound to it. The Composer
// quotes the column and renders the value as a Value Literal; callers supply
// already-resolved names (no schema lookup happens here).
type ColumnValue struct {
	Column string
	Value  ResultValue
}

// InsertSpec describes an INSERT. A single row renders the vertical
// single-statement form; multiple rows render one multi-row VALUES statement.
type InsertSpec struct {
	Table   TableRef
	Columns []string
	Rows    [][]ResultValue
}

// UpdateSpec describes a single-row UPDATE: SET assignments and a WHERE built
// from AND-ed predicates.
type UpdateSpec struct {
	Table       TableRef
	Assignments []ColumnValue
	Predicates  []ColumnValue
}

// DeleteSpec describes a single-row DELETE whose WHERE is AND-ed predicates.
type DeleteSpec struct {
	Table      TableRef
	Predicates []ColumnValue
}

// DeleteInSpec describes a DELETE whose WHERE is a single `column IN (...)`
// clause — the compact shape used for bulk deletes keyed on one column.
type DeleteInSpec struct {
	Table  TableRef
	Column string
	Values []ResultValue
}

// Composer assembles INSERT/UPDATE/DELETE statement text. It is mechanical:
// it renders strings using the Dialect's quoting and Value Literal primitives
// and makes no decisions about keys, source tables, or statement safety —
// that policy belongs to its callers. Construct with NewComposer so the
// Dialect is injected once.
type Composer struct {
	dialect Dialect
}

func NewComposer(dialect Dialect) Composer {
	return Composer{dialect: dialectOrDefault(dialect)}
}

func (c Composer) Insert(spec InsertSpec) string {
	table := QuoteTable(c.dialect, spec.Table)

	columns := make([]string, 0, len(spec.Columns))
	for _, name := range spec.Columns {
		columns = append(columns, "  "+c.dialect.QuoteIdentifier(name))
	}

	if len(spec.Rows) == 1 {
		values := make([]string, 0, len(spec.Rows[0]))
		for _, value := range spec.Rows[0] {
			values = append(values, "  "+c.dialect.ValueLiteral(value))
		}
		return fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES (\n%s\n);",
			table,
			strings.Join(columns, ",\n"),
			strings.Join(values, ",\n"),
		)
	}

	tuples := make([]string, 0, len(spec.Rows))
	for _, row := range spec.Rows {
		values := make([]string, 0, len(row))
		for _, value := range row {
			values = append(values, c.dialect.ValueLiteral(value))
		}
		tuples = append(tuples, "  ("+strings.Join(values, ", ")+")")
	}
	return fmt.Sprintf("INSERT INTO %s (\n%s\n) VALUES\n%s;",
		table,
		strings.Join(columns, ",\n"),
		strings.Join(tuples, ",\n"),
	)
}

func (c Composer) Update(spec UpdateSpec) string {
	assignments := make([]string, 0, len(spec.Assignments))
	for _, assignment := range spec.Assignments {
		assignments = append(assignments, fmt.Sprintf("  %s = %s",
			c.dialect.QuoteIdentifier(assignment.Column),
			c.dialect.ValueLiteral(assignment.Value),
		))
	}

	return fmt.Sprintf("UPDATE %s\nSET\n%s\nWHERE\n%s;",
		QuoteTable(c.dialect, spec.Table),
		strings.Join(assignments, ",\n"),
		strings.Join(c.predicateLines(spec.Predicates), "\n  AND "),
	)
}

func (c Composer) Delete(spec DeleteSpec) string {
	return fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
		QuoteTable(c.dialect, spec.Table),
		strings.Join(c.predicateLines(spec.Predicates), "\n  AND "),
	)
}

func (c Composer) DeleteIn(spec DeleteInSpec) string {
	values := make([]string, 0, len(spec.Values))
	for _, value := range spec.Values {
		values = append(values, c.dialect.ValueLiteral(value))
	}
	where := fmt.Sprintf("  %s IN (%s)",
		c.dialect.QuoteIdentifier(spec.Column),
		strings.Join(values, ", "),
	)
	return fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
		QuoteTable(c.dialect, spec.Table), where)
}

// DeleteGroups renders one DELETE whose WHERE OR-s together one AND-group of
// predicates per row — the fallback shape for bulk deletes with no single key
// column.
func (c Composer) DeleteGroups(table TableRef, groups [][]ColumnValue) string {
	rowClauses := make([]string, 0, len(groups))
	for _, group := range groups {
		predicates := c.predicateLines(group)
		for i := range predicates {
			predicates[i] = strings.TrimLeft(predicates[i], " \t")
		}
		rowClauses = append(rowClauses, "("+strings.Join(predicates, " AND ")+")")
	}
	where := "  " + strings.Join(rowClauses, "\n  OR ")
	return fmt.Sprintf("DELETE FROM %s\nWHERE\n%s;",
		QuoteTable(c.dialect, table), where)
}

func (c Composer) predicateLines(predicates []ColumnValue) []string {
	lines := make([]string, 0, len(predicates))
	for _, predicate := range predicates {
		column := c.dialect.QuoteIdentifier(predicate.Column)
		if predicate.Value.Kind == ValueKindNull || predicate.Value.Value == nil {
			lines = append(lines, fmt.Sprintf("  %s IS NULL", column))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s = %s", column, c.dialect.ValueLiteral(predicate.Value)))
	}
	return lines
}

// QuoteTable renders a TableRef as a dialect-quoted, dot-joined identifier
// (catalog.namespace.name), skipping empty parts. Returns "" when no part is
// set. A nil Dialect falls back to the SQLite dialect.
func QuoteTable(dialect Dialect, table TableRef) string {
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

	return dialectOrDefault(dialect).QuoteIdentifier(parts...)
}

func dialectOrDefault(dialect Dialect) Dialect {
	if dialect != nil {
		return dialect
	}
	return SQLiteDialect()
}

// renderValueLiteral renders a ResultValue as a SQL literal for the named
// dialect. Most kinds render identically across dialects; only bytes diverges.
func renderValueLiteral(dialectName string, value ResultValue) string {
	switch value.Kind {
	case ValueKindNull:
		return "NULL"
	case ValueKindBool:
		if typed, ok := value.Value.(bool); ok {
			if typed {
				return "TRUE"
			}
			return "FALSE"
		}
	case ValueKindInteger, ValueKindFloat, ValueKindDecimal:
		return fmt.Sprint(value.Value)
	case ValueKindString:
		return stringLiteral(fmt.Sprint(value.Value))
	case ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return bytesLiteral(dialectName, typed)
		}
	case ValueKindTime:
		t, state := extractTimeValue(value.Value)
		switch state {
		case timeValueValid:
			return timeLiteral(t)
		case timeValueNull:
			return "NULL"
		}
		if s, ok := value.Value.(string); ok {
			if t, ok := parseTimestampLiteral(s); ok {
				return timeLiteral(t)
			}
			return stringLiteral(s)
		}
	}

	if value.Value == nil {
		return "NULL"
	}
	t, state := extractTimeValue(value.Value)
	switch state {
	case timeValueValid:
		return timeLiteral(t)
	case timeValueNull:
		return "NULL"
	}
	return stringLiteral(fmt.Sprint(value.Value))
}

// timeLiteral renders a time.Time as a SQL string literal using a
// space-separated ISO-8601 form with an explicit numeric timezone offset. The
// resulting literal round-trips across PostgreSQL, MySQL (8.0.19+) and SQLite.
func timeLiteral(t time.Time) string {
	return stringLiteral(t.Format("2006-01-02 15:04:05.999999999-07:00"))
}

func stringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func bytesLiteral(dialectName string, value []byte) string {
	hex := fmt.Sprintf("%x", value)
	if dialectName == "postgres" {
		return fmt.Sprintf("decode('%s', 'hex')", hex)
	}
	return fmt.Sprintf("X'%s'", hex)
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

var timestampParseLayouts = []string{
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
	for _, layout := range timestampParseLayouts {
		if t, err := time.Parse(layout, trimmed); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
