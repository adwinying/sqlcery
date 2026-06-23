package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

// Writer serializes a Result Set — or a selected subset of its rows — into the
// byte output of a single Export Format. Each Export Format has exactly one
// Writer adapter, so adding a format is a new adapter (and one registry entry),
// not another arm of a switch. Marshal selects a Writer via writerFor.
type Writer interface {
	Write(result *db.ResultSet, rows []db.ResultRow) ([]byte, error)
}

// writers maps each Export Format to the constructor for its Writer adapter.
// The constructor receives the Dialect so the SQL adapter can render
// dialect-correct literals via the shared SQL Composer; the other adapters
// ignore it. Register a new format here alongside its adapter.
var writers = map[Format]func(db.Dialect) Writer{
	FormatCSV:      func(db.Dialect) Writer { return delimitedWriter{delimiter: ','} },
	FormatTSV:      func(db.Dialect) Writer { return delimitedWriter{delimiter: '\t'} },
	FormatJSON:     func(db.Dialect) Writer { return jsonWriter{} },
	FormatMarkdown: func(db.Dialect) Writer { return markdownWriter{} },
	FormatSQL:      func(dialect db.Dialect) Writer { return sqlWriter{dialect: dialect} },
}

// writerFor returns the Writer adapter registered for an Export Format.
func writerFor(format Format, dialect db.Dialect) (Writer, error) {
	construct, ok := writers[format]
	if !ok {
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
	return construct(dialect), nil
}

// delimitedWriter serializes to CSV/TSV; the two formats differ only by their
// field delimiter.
type delimitedWriter struct {
	delimiter rune
}

func (w delimitedWriter) Write(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.Comma = w.delimiter

	record := make([]string, len(result.Columns))
	for i := range result.Columns {
		record[i] = columnName(result.Columns, i)
	}
	if err := writer.Write(record); err != nil {
		return nil, fmt.Errorf("write header row: %w", err)
	}

	for _, row := range rows {
		record = make([]string, len(result.Columns))
		for i := range result.Columns {
			record[i] = formatTextValue(rowValue(row, i))
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write result row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush export rows: %w", err)
	}

	return buffer.Bytes(), nil
}

// jsonWriter serializes to a hand-formatted JSON array of row objects.
type jsonWriter struct{}

func (jsonWriter) Write(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("[\n")
	for rowIndex, row := range rows {
		buffer.WriteString("  {")
		for columnIndex := range result.Columns {
			if columnIndex > 0 {
				buffer.WriteString(",")
			}
			buffer.WriteString("\n    ")

			name, err := json.Marshal(columnName(result.Columns, columnIndex))
			if err != nil {
				return nil, fmt.Errorf("marshal json field name: %w", err)
			}
			value, err := json.Marshal(jsonValue(rowValue(row, columnIndex)))
			if err != nil {
				return nil, fmt.Errorf("marshal json field value: %w", err)
			}
			buffer.Write(name)
			buffer.WriteString(": ")
			buffer.Write(value)
		}
		if len(result.Columns) > 0 {
			buffer.WriteString("\n  }")
		} else {
			buffer.WriteString("}")
		}
		if rowIndex < len(rows)-1 {
			buffer.WriteString(",")
		}
		buffer.WriteString("\n")
	}
	buffer.WriteString("]\n")
	return buffer.Bytes(), nil
}

// markdownWriter serializes to a GitHub-flavored Markdown table.
type markdownWriter struct{}

func (markdownWriter) Write(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
	var lines []string
	header := make([]string, len(result.Columns))
	separator := make([]string, len(result.Columns))
	for i := range result.Columns {
		header[i] = escapeMarkdownCell(columnName(result.Columns, i))
		separator[i] = "---"
	}
	lines = append(lines,
		"| "+strings.Join(header, " | ")+" |",
		"| "+strings.Join(separator, " | ")+" |",
	)

	for _, row := range rows {
		values := make([]string, len(result.Columns))
		for i := range result.Columns {
			values[i] = escapeMarkdownCell(formatTextValue(rowValue(row, i)))
		}
		lines = append(lines, "| "+strings.Join(values, " | ")+" |")
	}

	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

// sqlWriter renders one INSERT statement per row via the shared SQL Composer,
// so byte literals, timestamps, and identifier quoting match the Statement
// Expander exactly. The target table is a "table_name" placeholder for the
// user to replace — an export has no single source table.
type sqlWriter struct {
	dialect db.Dialect
}

func (w sqlWriter) Write(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
	columns := make([]string, len(result.Columns))
	for i := range result.Columns {
		columns[i] = columnName(result.Columns, i)
	}

	composer := db.NewComposer(w.dialect)
	table := db.TableRef{Name: "table_name"}

	var buf strings.Builder
	for _, row := range rows {
		values := make([]db.ResultValue, len(result.Columns))
		for i := range result.Columns {
			values[i] = rowValue(row, i)
		}
		buf.WriteString(composer.Insert(db.InsertSpec{
			Table:   table,
			Columns: columns,
			Rows:    [][]db.ResultValue{values},
		}))
		buf.WriteString("\n")
	}
	return []byte(buf.String()), nil
}

// columnName returns the trimmed name of a Result Set column, falling back to a
// positional column_N placeholder when the driver reported no name.
func columnName(columns []db.ResultColumn, index int) string {
	if index >= 0 && index < len(columns) {
		name := strings.TrimSpace(columns[index].Name)
		if name != "" {
			return name
		}
	}
	return fmt.Sprintf("column_%d", index+1)
}

// rowValue returns the cell at index, or a null Value when the row is short.
func rowValue(row db.ResultRow, index int) db.ResultValue {
	if index >= 0 && index < len(row.Values) {
		return row.Values[index]
	}
	return db.ResultValue{Kind: db.ValueKindNull}
}

// formatTextValue renders a cell as plain text, shared by the delimited and
// Markdown adapters. Keyed off the Value Kind so null/bool/bytes/time format
// consistently across both.
func formatTextValue(value db.ResultValue) string {
	switch value.Kind {
	case db.ValueKindNull:
		return "NULL"
	case db.ValueKindBool:
		if typed, ok := value.Value.(bool); ok {
			if typed {
				return "true"
			}
			return "false"
		}
	case db.ValueKindInteger, db.ValueKindFloat, db.ValueKindDecimal, db.ValueKindString:
		return fmt.Sprint(value.Value)
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindTime:
		if typed, ok := value.Value.(time.Time); ok {
			return typed.Format("2006-01-02 15:04:05")
		}
	}

	if value.Value == nil {
		return "NULL"
	}
	return fmt.Sprint(value.Value)
}

// jsonValue maps a cell onto a JSON-encodable Go value for the JSON adapter,
// keyed off the Value Kind.
func jsonValue(value db.ResultValue) any {
	switch value.Kind {
	case db.ValueKindNull:
		return nil
	case db.ValueKindBytes:
		if typed, ok := value.Value.([]byte); ok {
			return fmt.Sprintf("0x%x", typed)
		}
	case db.ValueKindUnknown:
		if value.Value != nil {
			return fmt.Sprint(value.Value)
		}
		return nil
	}
	return value.Value
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "\r", "<br>")
	return value
}
