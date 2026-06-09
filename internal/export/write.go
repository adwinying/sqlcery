package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

type Format string

const (
	FormatCSV      Format = "CSV"
	FormatTSV      Format = "TSV"
	FormatJSON     Format = "JSON"
	FormatMarkdown Format = "Markdown"
)

type ExportOptions struct {
	CWD        string
	Filename   string
	Result     *db.ResultSet
	RowIndices []int
}

type ExportResult struct {
	Path    string
	Format  Format
	Rows    int
	Columns int
}

func Export(options ExportOptions) (ExportResult, error) {
	if options.Result == nil {
		return ExportResult{}, fmt.Errorf("result is required")
	}
	if len(options.Result.Columns) == 0 {
		return ExportResult{}, fmt.Errorf("result has no columns to export")
	}

	path, err := ResolveExportPath(options.CWD, options.Filename)
	if err != nil {
		return ExportResult{}, err
	}

	format, err := DetectFormat(path)
	if err != nil {
		return ExportResult{}, err
	}

	data, rowCount, err := Marshal(options.Result, options.RowIndices, format)
	if err != nil {
		return ExportResult{}, err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ExportResult{}, fmt.Errorf("write export file: %w", err)
	}

	return ExportResult{
		Path:    path,
		Format:  format,
		Rows:    rowCount,
		Columns: len(options.Result.Columns),
	}, nil
}

func ResolveExportPath(cwd, name string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("working directory is required")
	}

	targetName := strings.TrimSpace(name)
	if targetName == "" {
		return "", fmt.Errorf("filename is required")
	}

	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	cwdAbs = filepath.Clean(cwdAbs)

	target := targetName
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwdAbs, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve export path: %w", err)
	}
	target = filepath.Clean(target)

	rel, err := filepath.Rel(cwdAbs, target)
	if err != nil {
		return "", fmt.Errorf("validate export path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("export path must stay within %s", cwdAbs)
	}

	parent := filepath.Dir(target)
	info, err := os.Stat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("export directory does not exist: %s", parent)
		}
		return "", fmt.Errorf("stat export directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("export directory is not a directory: %s", parent)
	}

	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return "", fmt.Errorf("export path points to a directory: %s", target)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat export path: %w", err)
	}

	return target, nil
}

func DetectFormat(path string) (Format, error) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".csv":
		return FormatCSV, nil
	case ".tsv":
		return FormatTSV, nil
	case ".json":
		return FormatJSON, nil
	case ".md", ".markdown":
		return FormatMarkdown, nil
	default:
		return "", fmt.Errorf("unsupported export format; use .csv, .tsv, .json, or .md")
	}
}

func Marshal(result *db.ResultSet, rowIndices []int, format Format) ([]byte, int, error) {
	if result == nil {
		return nil, 0, fmt.Errorf("result is required")
	}
	if len(result.Columns) == 0 {
		return nil, 0, fmt.Errorf("result has no columns to export")
	}

	rows, err := rowsForIndices(result, rowIndices)
	if err != nil {
		return nil, 0, err
	}

	var data []byte
	switch format {
	case FormatCSV:
		data, err = marshalDelimited(result, rows, ',')
	case FormatTSV:
		data, err = marshalDelimited(result, rows, '\t')
	case FormatJSON:
		data, err = marshalJSON(result, rows)
	case FormatMarkdown:
		data, err = marshalMarkdown(result, rows)
	default:
		err = fmt.Errorf("unsupported export format %q", format)
	}
	if err != nil {
		return nil, 0, err
	}

	return data, len(rows), nil
}

func rowsForIndices(result *db.ResultSet, rowIndices []int) ([]db.ResultRow, error) {
	if len(rowIndices) == 0 {
		return append([]db.ResultRow(nil), result.Rows...), nil
	}

	rows := make([]db.ResultRow, 0, len(rowIndices))
	for _, index := range rowIndices {
		if index < 0 || index >= len(result.Rows) {
			return nil, fmt.Errorf("row %d is out of range", index+1)
		}
		rows = append(rows, result.Rows[index])
	}
	return rows, nil
}

func marshalDelimited(result *db.ResultSet, rows []db.ResultRow, delimiter rune) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.Comma = delimiter

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

func marshalJSON(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
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

func marshalMarkdown(result *db.ResultSet, rows []db.ResultRow) ([]byte, error) {
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

func columnName(columns []db.ResultColumn, index int) string {
	if index >= 0 && index < len(columns) {
		name := strings.TrimSpace(columns[index].Name)
		if name != "" {
			return name
		}
	}
	return fmt.Sprintf("column_%d", index+1)
}

func rowValue(row db.ResultRow, index int) db.ResultValue {
	if index >= 0 && index < len(row.Values) {
		return row.Values[index]
	}
	return db.ResultValue{Kind: db.ValueKindNull}
}

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
