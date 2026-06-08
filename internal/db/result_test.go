package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
)

func TestNormalizeRowsCapturesTypedValuesAndMetadata(t *testing.T) {
	createdAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	payload := []byte("abc")
	rows := &scriptedRows{
		columns: []string{"id", "name", "active", "score", "payload", "created_at", "note"},
		values:  [][]any{{int64(7), "gizmo", true, 12.5, payload, createdAt, nil}},
	}

	result, err := NormalizeRows(rows, ResultOptions{
		Source: &TableRef{Namespace: "main", Name: "widgets"},
		Columns: []Column{
			{Name: "id", Position: 1, Type: "INTEGER", Nullable: true},
			{Name: "name", Position: 2, Type: "TEXT", Nullable: false},
		},
		PrimaryKeys: []PrimaryKey{{Column: "id", Position: 1}},
	})
	if err != nil {
		t.Fatalf("NormalizeRows() error = %v", err)
	}

	if !rows.closed {
		t.Fatal("NormalizeRows() did not close rows")
	}

	if result.Source == nil || *result.Source != (TableRef{Namespace: "main", Name: "widgets"}) {
		t.Fatalf("NormalizeRows() source = %#v, want widgets source", result.Source)
	}

	if got, want := len(result.Columns), 7; got != want {
		t.Fatalf("len(result.Columns) = %d, want %d", got, want)
	}

	if got := result.Columns[0]; got.Name != "id" || got.Position != 1 || got.Schema == nil || got.Schema.Type != "INTEGER" || got.PrimaryKey == nil || got.PrimaryKey.Column != "id" {
		t.Fatalf("result.Columns[0] = %#v, want schema and primary key metadata", got)
	}

	if got, want := len(result.Rows), 1; got != want {
		t.Fatalf("len(result.Rows) = %d, want %d", got, want)
	}
	if got, want := result.Rows[0].Position, 1; got != want {
		t.Fatalf("result.Rows[0].Position = %d, want %d", got, want)
	}

	values := result.Rows[0].Values
	assertResultValue(t, values[0], ValueKindInteger, int64(7))
	assertResultValue(t, values[1], ValueKindString, "gizmo")
	assertResultValue(t, values[2], ValueKindBool, true)
	assertResultValue(t, values[3], ValueKindFloat, 12.5)
	assertResultValue(t, values[5], ValueKindTime, createdAt)

	if got, want := values[4].Kind, ValueKindBytes; got != want {
		t.Fatalf("payload kind = %q, want %q", got, want)
	}

	gotPayload, ok := values[4].Value.([]byte)
	if !ok {
		t.Fatalf("payload value type = %T, want []byte", values[4].Value)
	}

	payload[0] = 'z'
	if string(gotPayload) != "abc" {
		t.Fatalf("payload value = %q, want %q", string(gotPayload), "abc")
	}

	if got, want := values[6].Kind, ValueKindNull; got != want {
		t.Fatalf("note kind = %q, want %q", got, want)
	}
	if values[6].Value != nil {
		t.Fatalf("note value = %#v, want nil", values[6].Value)
	}
}

func TestNormalizeRowsNormalizesDriverByteValuesUsingColumnMetadata(t *testing.T) {
	amount := []byte("12.3400")
	name := []byte("gizmo")
	payload := []byte{0x00, 0x01, 0x02}
	rows := &scriptedRows{
		columns: []string{"amount", "name", "payload"},
		values: [][]any{
			{amount, name, payload},
			{[]byte("9.5000"), []byte("widget"), []byte{0x03}},
		},
	}

	result, err := NormalizeRows(rows, ResultOptions{
		Columns: []Column{
			{Name: "amount", Position: 1, Type: "DECIMAL(10,4)"},
			{Name: "name", Position: 2, Type: "VARCHAR(255)"},
			{Name: "payload", Position: 3, Type: "BLOB"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeRows() error = %v", err)
	}

	if got, want := len(result.Rows), 2; got != want {
		t.Fatalf("len(result.Rows) = %d, want %d", got, want)
	}
	if got, want := result.Rows[1].Position, 2; got != want {
		t.Fatalf("result.Rows[1].Position = %d, want %d", got, want)
	}

	assertResultValue(t, result.Rows[0].Values[0], ValueKindDecimal, "12.3400")
	assertResultValue(t, result.Rows[0].Values[1], ValueKindString, "gizmo")

	amount[0] = '9'
	name[0] = 'w'
	payload[0] = 0xff

	assertResultValue(t, result.Rows[0].Values[0], ValueKindDecimal, "12.3400")
	assertResultValue(t, result.Rows[0].Values[1], ValueKindString, "gizmo")

	if got, want := result.Rows[0].Values[2].Kind, ValueKindBytes; got != want {
		t.Fatalf("payload kind = %q, want %q", got, want)
	}
	gotPayload, ok := result.Rows[0].Values[2].Value.([]byte)
	if !ok {
		t.Fatalf("payload value type = %T, want []byte", result.Rows[0].Values[2].Value)
	}
	if got, want := gotPayload, []byte{0x00, 0x01, 0x02}; !equalBytes(got, want) {
		t.Fatalf("payload value = %#v, want %#v", got, want)
	}
}

func TestQueryResultContextSQLiteIncludesResultMetadata(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "result.db")

	adapter, err := Open(ctx, config.Connection{
		Type:     "sqlite",
		Database: databasePath,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer adapter.Close()

	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if _, err := adapter.ExecContext(ctx, "create table widgets (id integer primary key, name text not null, score real, note text)"); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	if _, err := adapter.ExecContext(ctx, "insert into widgets (name, score, note) values (?, ?, ?)", "gizmo", 9.5, nil); err != nil {
		t.Fatalf("ExecContext(insert) error = %v", err)
	}

	result, err := adapter.QueryResultContext(ctx, "select id, name, score, note from widgets order by id", ResultOptions{
		Source: &TableRef{Namespace: "main", Name: "widgets"},
	})
	if err != nil {
		t.Fatalf("QueryResultContext() error = %v", err)
	}

	if got, want := len(result.Columns), 4; got != want {
		t.Fatalf("len(result.Columns) = %d, want %d", got, want)
	}

	idColumn := result.Columns[0]
	if idColumn.PrimaryKey == nil || idColumn.PrimaryKey.Column != "id" {
		t.Fatalf("id column primary key = %#v, want id primary key", idColumn.PrimaryKey)
	}
	if idColumn.Schema == nil || idColumn.Schema.Type != "INTEGER" {
		t.Fatalf("id column schema = %#v, want INTEGER schema metadata", idColumn.Schema)
	}
	if idColumn.DatabaseType == "" {
		t.Fatal("id column database type is empty")
	}
	if idColumn.ScanType == "" {
		t.Fatal("id column scan type is empty")
	}

	if got, want := len(result.Rows), 1; got != want {
		t.Fatalf("len(result.Rows) = %d, want %d", got, want)
	}

	row := result.Rows[0].Values
	assertResultValue(t, row[0], ValueKindInteger, int64(1))
	assertResultValue(t, row[1], ValueKindString, "gizmo")
	assertResultValue(t, row[2], ValueKindFloat, 9.5)
	if got, want := row[3].Kind, ValueKindNull; got != want {
		t.Fatalf("row[3].Kind = %q, want %q", got, want)
	}
}

func assertResultValue(t *testing.T, value ResultValue, wantKind ValueKind, wantValue any) {
	t.Helper()

	if got := value.Kind; got != wantKind {
		t.Fatalf("value.Kind = %q, want %q", got, wantKind)
	}

	if got := value.Value; got != wantValue {
		t.Fatalf("value.Value = %#v, want %#v", got, wantValue)
	}
}

func equalBytes(got, want []byte) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type scriptedRows struct {
	columns []string
	values  [][]any
	index   int
	closed  bool
}

func (r *scriptedRows) Close() error {
	r.closed = true
	return nil
}

func (r *scriptedRows) Columns() ([]string, error) {
	return append([]string(nil), r.columns...), nil
}

func (*scriptedRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return nil, nil
}

func (*scriptedRows) Err() error {
	return nil
}

func (r *scriptedRows) Next() bool {
	return r.index < len(r.values)
}

func (r *scriptedRows) Scan(dest ...any) error {
	if r.index >= len(r.values) {
		return fmt.Errorf("Scan() called with no remaining rows")
	}

	row := r.values[r.index]
	r.index++
	if len(dest) != len(row) {
		return fmt.Errorf("Scan() got %d destinations, want %d", len(dest), len(row))
	}

	for i := range dest {
		ptr, ok := dest[i].(*any)
		if !ok {
			return fmt.Errorf("destination %d type = %T, want *any", i, dest[i])
		}
		*ptr = row[i]
	}

	return nil
}
