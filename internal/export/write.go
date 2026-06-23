package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adwinying/sqlcery/internal/db"
)

type Format string

const (
	FormatCSV      Format = "CSV"
	FormatTSV      Format = "TSV"
	FormatJSON     Format = "JSON"
	FormatMarkdown Format = "Markdown"
	FormatSQL      Format = "SQL"
)

type ExportOptions struct {
	CWD        string
	Filename   string
	Result     *db.ResultSet
	RowIndices []int
	Format     Format     // when non-empty, skips DetectFormat so the chosen format wins over the file extension
	Dialect    db.Dialect // used by the SQL format to render dialect-correct literals and quoting; nil falls back to SQLite
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

	format := options.Format
	if format == "" {
		format, err = DetectFormat(path)
		if err != nil {
			return ExportResult{}, err
		}
	}

	data, rowCount, err := Marshal(options.Result, options.RowIndices, format, options.Dialect)
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
	if strings.HasPrefix(target, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			target = filepath.Join(home, target[2:])
		}
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwdAbs, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve export path: %w", err)
	}
	target = filepath.Clean(target)

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
	case ".sql":
		return FormatSQL, nil
	default:
		return "", fmt.Errorf("unsupported export format; use .csv, .tsv, .json, .md, or .sql")
	}
}

func Marshal(result *db.ResultSet, rowIndices []int, format Format, dialect db.Dialect) ([]byte, int, error) {
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

	writer, err := writerFor(format, dialect)
	if err != nil {
		return nil, 0, err
	}
	data, err := writer.Write(result, rows)
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
