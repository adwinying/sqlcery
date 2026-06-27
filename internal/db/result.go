package db

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type ResultOptions struct {
	Source      *TableRef
	Columns     []Column
	PrimaryKeys []PrimaryKey
}

type ResultSet struct {
	Source  *TableRef
	Columns []ResultColumn
	Rows    []ResultRow
}

type ResultColumn struct {
	Name             string
	Position         int
	DriverColumnType string
	ScanType         string
	Nullable         *bool
	Length           *int64
	DecimalSize      *DecimalSize
	Schema           *Column
	PrimaryKey       *PrimaryKey
}

type DecimalSize struct {
	Precision int64
	Scale     int64
}

type ResultRow struct {
	Position int
	Values   []ResultValue
}

type ResultValue struct {
	Kind  ValueKind
	Value any
}

type ValueKind string

const (
	ValueKindNull    ValueKind = "null"
	ValueKindBool    ValueKind = "bool"
	ValueKindInteger ValueKind = "integer"
	ValueKindFloat   ValueKind = "float"
	ValueKindString  ValueKind = "string"
	ValueKindBytes   ValueKind = "bytes"
	ValueKindDecimal ValueKind = "decimal"
	ValueKindTime    ValueKind = "time"
	ValueKindUnknown ValueKind = "unknown"
)

func NormalizeRows(rows Rows, options ResultOptions) (_ *ResultSet, err error) {
	if rows == nil {
		return nil, fmt.Errorf("rows are required")
	}

	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close result rows: %w", closeErr)
		}
	}()

	columnNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("list result columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("list result column types: %w", err)
	}

	result := &ResultSet{
		Source:  cloneTableRef(options.Source),
		Columns: buildResultColumns(columnNames, columnTypes, options),
		Rows:    make([]ResultRow, 0),
	}

	values := make([]any, len(columnNames))
	scanDestinations := make([]any, len(columnNames))
	for i := range values {
		scanDestinations[i] = &values[i]
	}

	for rows.Next() {
		for i := range values {
			values[i] = nil
		}

		if err := rows.Scan(scanDestinations...); err != nil {
			return nil, fmt.Errorf("scan result row: %w", err)
		}

		row := ResultRow{Position: len(result.Rows) + 1, Values: make([]ResultValue, len(values))}
		for i, value := range values {
			row.Values[i] = normalizeResultValue(value, result.Columns[i])
		}

		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result rows: %w", err)
	}

	return result, nil
}

func buildResultColumns(names []string, columnTypes []*sql.ColumnType, options ResultOptions) []ResultColumn {
	columns := make([]ResultColumn, 0, len(names))
	for i, name := range names {
		column := ResultColumn{Name: name, Position: i + 1}

		if i < len(columnTypes) && columnTypes[i] != nil {
			column = applyColumnTypeMetadata(column, columnTypes[i])
		}

		if schemaColumn, ok := matchColumnMetadata(options.Columns, column.Position, column.Name); ok {
			column.Schema = &schemaColumn
			if column.DriverColumnType == "" {
				column.DriverColumnType = schemaColumn.Type
			}
			if column.Nullable == nil {
				nullable := schemaColumn.Nullable
				column.Nullable = &nullable
			}
		}

		if primaryKey, ok := matchPrimaryKey(options.PrimaryKeys, column.Name); ok {
			column.PrimaryKey = &primaryKey
		}

		columns = append(columns, column)
	}

	return columns
}

func applyColumnTypeMetadata(column ResultColumn, columnType *sql.ColumnType) ResultColumn {
	column.DriverColumnType = strings.TrimSpace(columnType.DatabaseTypeName())

	if scanType := columnType.ScanType(); scanType != nil {
		column.ScanType = scanType.String()
	}

	if nullable, ok := columnType.Nullable(); ok {
		column.Nullable = boolPointer(nullable)
	}

	if length, ok := columnType.Length(); ok {
		column.Length = int64Pointer(length)
	}

	if precision, scale, ok := columnType.DecimalSize(); ok {
		column.DecimalSize = &DecimalSize{Precision: precision, Scale: scale}
	}

	return column
}

func normalizeResultValue(value any, column ResultColumn) ResultValue {
	switch typed := value.(type) {
	case nil:
		return ResultValue{Kind: ValueKindNull}
	case bool:
		return ResultValue{Kind: ValueKindBool, Value: typed}
	case int:
		return ResultValue{Kind: ValueKindInteger, Value: int64(typed)}
	case int8:
		return ResultValue{Kind: ValueKindInteger, Value: int64(typed)}
	case int16:
		return ResultValue{Kind: ValueKindInteger, Value: int64(typed)}
	case int32:
		return ResultValue{Kind: ValueKindInteger, Value: int64(typed)}
	case int64:
		return ResultValue{Kind: ValueKindInteger, Value: typed}
	case uint:
		return ResultValue{Kind: ValueKindInteger, Value: uint64(typed)}
	case uint8:
		return ResultValue{Kind: ValueKindInteger, Value: uint64(typed)}
	case uint16:
		return ResultValue{Kind: ValueKindInteger, Value: uint64(typed)}
	case uint32:
		return ResultValue{Kind: ValueKindInteger, Value: uint64(typed)}
	case uint64:
		return ResultValue{Kind: ValueKindInteger, Value: typed}
	case float32:
		return ResultValue{Kind: ValueKindFloat, Value: float64(typed)}
	case float64:
		return ResultValue{Kind: ValueKindFloat, Value: typed}
	case string:
		if resultColumnLooksDecimal(column) {
			return ResultValue{Kind: ValueKindDecimal, Value: typed}
		}
		return ResultValue{Kind: ValueKindString, Value: typed}
	case []byte:
		return normalizeByteResultValue(typed, column)
	case time.Time:
		return ResultValue{Kind: ValueKindTime, Value: typed}
	default:
		return ResultValue{Kind: classifyResultValueKind(typed), Value: typed}
	}
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02",
}

func normalizeByteResultValue(value []byte, column ResultColumn) ResultValue {
	copy := append([]byte(nil), value...)
	if resultColumnLooksBinary(column) {
		return ResultValue{Kind: ValueKindBytes, Value: copy}
	}
	if !utf8.Valid(copy) {
		return ResultValue{Kind: ValueKindBytes, Value: copy}
	}
	if resultColumnLooksTime(column) {
		text := string(copy)
		for _, layout := range timeLayouts {
			if t, err := time.Parse(layout, text); err == nil {
				return ResultValue{Kind: ValueKindTime, Value: t}
			}
		}
	}
	if !resultColumnLooksText(column) && !resultColumnLooksDecimal(column) {
		return ResultValue{Kind: ValueKindBytes, Value: copy}
	}

	text := string(copy)
	if resultColumnLooksDecimal(column) {
		return ResultValue{Kind: ValueKindDecimal, Value: text}
	}

	return ResultValue{Kind: ValueKindString, Value: text}
}

func classifyResultValueKind(value any) ValueKind {
	if value == nil {
		return ValueKindNull
	}

	switch reflect.TypeOf(value).Kind() {
	case reflect.Bool:
		return ValueKindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return ValueKindInteger
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return ValueKindInteger
	case reflect.Float32, reflect.Float64:
		return ValueKindFloat
	case reflect.String:
		return ValueKindString
	case reflect.Slice:
		if reflect.TypeOf(value).Elem().Kind() == reflect.Uint8 {
			return ValueKindBytes
		}
	}

	return ValueKindUnknown
}

func resultColumnLooksBinary(column ResultColumn) bool {
	return resultColumnTypeMatches(column, "BINARY", "VARBINARY", "BLOB", "BYTEA")
}

func resultColumnLooksText(column ResultColumn) bool {
	return resultColumnTypeMatches(column, "CHAR", "TEXT", "CLOB", "JSON", "XML")
}

func resultColumnLooksDecimal(column ResultColumn) bool {
	return resultColumnTypeMatches(column, "DECIMAL", "NUMERIC")
}

func resultColumnLooksTime(column ResultColumn) bool {
	return resultColumnTypeMatches(column, "DATE", "TIME", "TIMESTAMP", "DATETIME")
}

func resultColumnTypeMatches(column ResultColumn, fragments ...string) bool {
	for _, typeName := range resultColumnTypeNames(column) {
		for _, fragment := range fragments {
			if strings.Contains(typeName, fragment) {
				return true
			}
		}
	}

	return false
}

func resultColumnTypeNames(column ResultColumn) []string {
	typeNames := make([]string, 0, 2)
	if column.DriverColumnType != "" {
		typeNames = append(typeNames, strings.ToUpper(strings.TrimSpace(column.DriverColumnType)))
	}
	if column.Schema != nil && column.Schema.Type != "" {
		typeNames = append(typeNames, strings.ToUpper(strings.TrimSpace(column.Schema.Type)))
	}
	return typeNames
}

func cloneTableRef(table *TableRef) *TableRef {
	if table == nil {
		return nil
	}

	copy := *table
	return &copy
}

func matchColumnMetadata(columns []Column, position int, name string) (Column, bool) {
	for _, column := range columns {
		if column.Position == position && column.Position != 0 {
			return column, true
		}
	}

	for _, column := range columns {
		if strings.EqualFold(column.Name, name) {
			return column, true
		}
	}

	return Column{}, false
}

func matchPrimaryKey(primaryKeys []PrimaryKey, name string) (PrimaryKey, bool) {
	for _, primaryKey := range primaryKeys {
		if strings.EqualFold(primaryKey.Column, name) {
			return primaryKey, true
		}
	}

	return PrimaryKey{}, false
}

func boolPointer(value bool) *bool {
	copy := value
	return &copy
}

func int64Pointer(value int64) *int64 {
	copy := value
	return &copy
}
