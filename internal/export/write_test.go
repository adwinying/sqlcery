package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestMarshalSupportsCSVTSVJSONAndMarkdown(t *testing.T) {
	stamp := time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC)
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "created_at"}, {Name: "payload"}},
		Rows: []db.ResultRow{{
			Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Ada"}, {Kind: db.ValueKindTime, Value: stamp}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}},
		}, {
			Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindNull}, {Kind: db.ValueKindTime, Value: stamp.Add(time.Hour)}, {Kind: db.ValueKindBytes, Value: []byte("ok")}},
		}},
	}

	csvData, rows, err := Marshal(result, nil, FormatCSV, nil)
	if err != nil {
		t.Fatalf("Marshal(csv) error = %v", err)
	}
	if rows != 2 {
		t.Fatalf("Marshal(csv) rows = %d, want 2", rows)
	}
	if got := string(csvData); got != "id,name,created_at,payload\n1,Ada,2026-04-08 12:34:56,0xdead\n2,NULL,2026-04-08 13:34:56,0x6f6b\n" {
		t.Fatalf("Marshal(csv) = %q", got)
	}

	tsvData, _, err := Marshal(result, []int{1}, FormatTSV, nil)
	if err != nil {
		t.Fatalf("Marshal(tsv) error = %v", err)
	}
	if got := string(tsvData); got != "id\tname\tcreated_at\tpayload\n2\tNULL\t2026-04-08 13:34:56\t0x6f6b\n" {
		t.Fatalf("Marshal(tsv) = %q", got)
	}

	jsonData, _, err := Marshal(result, []int{0}, FormatJSON, nil)
	if err != nil {
		t.Fatalf("Marshal(json) error = %v", err)
	}
	var payload []map[string]any
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("len(payload) = %d, want 1", len(payload))
	}
	if got, want := payload[0]["id"], float64(1); got != want {
		t.Fatalf("payload[0][id] = %#v, want %#v", got, want)
	}
	if got, want := payload[0]["payload"], "0xdead"; got != want {
		t.Fatalf("payload[0][payload] = %#v, want %#v", got, want)
	}

	markdownData, _, err := Marshal(result, []int{0}, FormatMarkdown, nil)
	if err != nil {
		t.Fatalf("Marshal(markdown) error = %v", err)
	}
	markdown := string(markdownData)
	for _, want := range []string{
		"| id | name | created_at | payload |",
		"| --- | --- | --- | --- |",
		"| 1 | Ada | 2026-04-08 12:34:56 | 0xdead |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("Marshal(markdown) = %q, want to contain %q", markdown, want)
		}
	}
}

func TestMarshalSQLGeneratesInsertStatements(t *testing.T) {
	stamp := time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC)
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}, {Name: "active"}, {Name: "score"}, {Name: "created_at"}},
		Rows: []db.ResultRow{{
			Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(1)},
				{Kind: db.ValueKindString, Value: "O'Brien"},
				{Kind: db.ValueKindBool, Value: true},
				{Kind: db.ValueKindFloat, Value: 3.14},
				{Kind: db.ValueKindTime, Value: stamp},
			},
		}, {
			Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(2)},
				{Kind: db.ValueKindNull},
				{Kind: db.ValueKindBool, Value: false},
				{Kind: db.ValueKindFloat, Value: 0.0},
				{Kind: db.ValueKindNull},
			},
		}},
	}

	data, rows, err := Marshal(result, nil, FormatSQL, nil)
	if err != nil {
		t.Fatalf("Marshal(sql) error = %v", err)
	}
	if rows != 2 {
		t.Fatalf("Marshal(sql) rows = %d, want 2", rows)
	}
	got := string(data)
	// One INSERT per row, rendered by the shared SQL Composer: the placeholder
	// table is dialect-quoted, columns are vertical, the apostrophe is escaped,
	// and the timestamp carries the canonical timezone offset.
	for _, want := range []string{
		`INSERT INTO "table_name" (`,
		`  "id",`,
		`  "created_at"`,
		`'O''Brien'`,
		`3.14`,
		`'2026-04-08 12:34:56+00:00'`,
		"FALSE",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Marshal(sql) = %q, want to contain %q", got, want)
		}
	}
}

func TestResolveExportPathResolvesRelativeAndAbsolutePaths(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "exports"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	// relative path within cwd
	path, err := ResolveExportPath(cwd, "exports/result.csv")
	if err != nil {
		t.Fatalf("ResolveExportPath() error = %v", err)
	}
	if got, want := path, filepath.Join(cwd, "exports", "result.csv"); got != want {
		t.Fatalf("ResolveExportPath() = %q, want %q", got, want)
	}

	// relative path escaping cwd is allowed
	if _, err := ResolveExportPath(cwd, "../result.csv"); err != nil {
		t.Fatalf("ResolveExportPath(\"../result.csv\") error = %v, want nil", err)
	}

	// absolute path outside cwd is allowed
	outsideAbs := filepath.Join(filepath.Dir(cwd), "outside.csv")
	if got, err := ResolveExportPath(cwd, outsideAbs); err != nil {
		t.Fatalf("ResolveExportPath(%q) error = %v, want nil", outsideAbs, err)
	} else if got != outsideAbs {
		t.Fatalf("ResolveExportPath(%q) = %q, want %q", outsideAbs, got, outsideAbs)
	}
}

func TestWriteRejectsMissingDirectoryAndPersistsExport(t *testing.T) {
	cwd := t.TempDir()
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}},
		Rows:    []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}}}},
	}

	if _, err := Export(ExportOptions{CWD: cwd, Filename: "missing/result.csv", Result: result}); err == nil {
		t.Fatal("Export() error = nil, want missing directory error")
	}

	written, err := Export(ExportOptions{CWD: cwd, Filename: "result.json", Result: result})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if got, want := written.Format, FormatJSON; got != want {
		t.Fatalf("written.Format = %q, want %q", got, want)
	}
	if got, want := written.Rows, 1; got != want {
		t.Fatalf("written.Rows = %d, want %d", got, want)
	}
	data, err := os.ReadFile(written.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); !strings.Contains(got, "\"id\": 7") {
		t.Fatalf("export contents = %q, want json row", got)
	}
}
