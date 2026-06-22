package db

import (
	"database/sql"
	"testing"
	"time"
)

func TestValueLiteralFormatsTimestamps(t *testing.T) {
	pst := time.FixedZone("PST", -8*60*60)

	type pgTimestamp struct {
		Time  time.Time
		Valid bool
	}

	tests := []struct {
		name  string
		value ResultValue
		want  string
	}{
		{
			name:  "utc time.Time with microseconds",
			value: ResultValue{Kind: ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 123456000, time.UTC)},
			want:  "'2026-04-22 10:30:45.123456+00:00'",
		},
		{
			name:  "time.Time with fixed negative offset",
			value: ResultValue{Kind: ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 0, pst)},
			want:  "'2026-04-22 10:30:45-08:00'",
		},
		{
			name:  "time.Time with zero fractional seconds omits fraction",
			value: ResultValue{Kind: ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC)},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "nil value becomes NULL",
			value: ResultValue{Kind: ValueKindNull},
			want:  "NULL",
		},
		{
			name:  "time kind with nil value falls back to NULL",
			value: ResultValue{Kind: ValueKindTime, Value: nil},
			want:  "NULL",
		},
		{
			name:  "sql.NullTime valid round-trips",
			value: ResultValue{Kind: ValueKindTime, Value: sql.NullTime{Time: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC), Valid: true}},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "sql.NullTime invalid becomes NULL",
			value: ResultValue{Kind: ValueKindTime, Value: sql.NullTime{Valid: false}},
			want:  "NULL",
		},
		{
			name:  "pgtype-like struct valid",
			value: ResultValue{Kind: ValueKindUnknown, Value: pgTimestamp{Time: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC), Valid: true}},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "pgtype-like struct invalid becomes NULL",
			value: ResultValue{Kind: ValueKindUnknown, Value: pgTimestamp{Valid: false}},
			want:  "NULL",
		},
		{
			name:  "timestamp string is reformatted",
			value: ResultValue{Kind: ValueKindTime, Value: "2026-04-22T10:30:45Z"},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PostgresDialect().ValueLiteral(tt.value)
			if got != tt.want {
				t.Fatalf("ValueLiteral() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposerInsertSingleRow(t *testing.T) {
	got := NewComposer(PostgresDialect()).Insert(InsertSpec{
		Table:   TableRef{Namespace: "public", Name: "widgets"},
		Columns: []string{"id", "payload"},
		Rows: [][]ResultValue{{
			{Kind: ValueKindInteger, Value: int64(7)},
			{Kind: ValueKindBytes, Value: []byte{0xde, 0xad}},
		}},
	})
	want := "INSERT INTO \"public\".\"widgets\" (\n  \"id\",\n  \"payload\"\n) VALUES (\n  7,\n  decode('dead', 'hex')\n);"
	if got != want {
		t.Fatalf("Insert() =\n%q\nwant\n%q", got, want)
	}
}

func TestComposerInsertMultiRow(t *testing.T) {
	got := NewComposer(SQLiteDialect()).Insert(InsertSpec{
		Table:   TableRef{Name: "widgets"},
		Columns: []string{"id", "name"},
		Rows: [][]ResultValue{
			{{Kind: ValueKindInteger, Value: int64(1)}, {Kind: ValueKindString, Value: "Alice"}},
			{{Kind: ValueKindInteger, Value: int64(2)}, {Kind: ValueKindString, Value: "Bob"}},
		},
	})
	want := "INSERT INTO \"widgets\" (\n  \"id\",\n  \"name\"\n) VALUES\n  (1, 'Alice'),\n  (2, 'Bob');"
	if got != want {
		t.Fatalf("Insert() =\n%q\nwant\n%q", got, want)
	}
}

func TestComposerUpdate(t *testing.T) {
	got := NewComposer(MySQLDialect()).Update(UpdateSpec{
		Table:       TableRef{Name: "widgets"},
		Assignments: []ColumnValue{{Column: "name", Value: ResultValue{Kind: ValueKindString, Value: "Bob"}}},
		Predicates:  []ColumnValue{{Column: "id", Value: ResultValue{Kind: ValueKindInteger, Value: int64(3)}}},
	})
	want := "UPDATE `widgets`\nSET\n  `name` = 'Bob'\nWHERE\n  `id` = 3;"
	if got != want {
		t.Fatalf("Update() =\n%q\nwant\n%q", got, want)
	}
}

func TestComposerDeletePredicateNullRendersIsNull(t *testing.T) {
	got := NewComposer(SQLiteDialect()).Delete(DeleteSpec{
		Table:      TableRef{Name: "widgets"},
		Predicates: []ColumnValue{{Column: "deleted_at", Value: ResultValue{Kind: ValueKindNull}}},
	})
	want := "DELETE FROM \"widgets\"\nWHERE\n  \"deleted_at\" IS NULL;"
	if got != want {
		t.Fatalf("Delete() =\n%q\nwant\n%q", got, want)
	}
}

func TestComposerDeleteIn(t *testing.T) {
	got := NewComposer(SQLiteDialect()).DeleteIn(DeleteInSpec{
		Table:  TableRef{Name: "widgets"},
		Column: "id",
		Values: []ResultValue{{Kind: ValueKindInteger, Value: int64(1)}, {Kind: ValueKindInteger, Value: int64(3)}},
	})
	want := "DELETE FROM \"widgets\"\nWHERE\n  \"id\" IN (1, 3);"
	if got != want {
		t.Fatalf("DeleteIn() =\n%q\nwant\n%q", got, want)
	}
}

func TestComposerDeleteGroups(t *testing.T) {
	got := NewComposer(SQLiteDialect()).DeleteGroups(TableRef{Name: "memberships"}, [][]ColumnValue{
		{
			{Column: "user_id", Value: ResultValue{Kind: ValueKindInteger, Value: int64(1)}},
			{Column: "org_id", Value: ResultValue{Kind: ValueKindInteger, Value: int64(10)}},
		},
		{
			{Column: "user_id", Value: ResultValue{Kind: ValueKindInteger, Value: int64(2)}},
			{Column: "org_id", Value: ResultValue{Kind: ValueKindInteger, Value: int64(10)}},
		},
	})
	want := "DELETE FROM \"memberships\"\nWHERE\n  (\"user_id\" = 1 AND \"org_id\" = 10)\n  OR (\"user_id\" = 2 AND \"org_id\" = 10);"
	if got != want {
		t.Fatalf("DeleteGroups() =\n%q\nwant\n%q", got, want)
	}
}

// TestQuoteTableNilDialectFallsBackToSQLite pins the nil-dialect default the
// Composer relies on.
func TestQuoteTableNilDialectFallsBackToSQLite(t *testing.T) {
	if got := QuoteTable(nil, TableRef{Name: "widgets"}); got != `"widgets"` {
		t.Fatalf("QuoteTable(nil) = %q, want %q", got, `"widgets"`)
	}
	if got := QuoteTable(SQLiteDialect(), TableRef{}); got != "" {
		t.Fatalf("QuoteTable(empty) = %q, want empty", got)
	}
}

// TestValueLiteralBytesDivergesByDialect pins the one Value Literal that is
// dialect-specific: PostgreSQL renders bytes as decode('…','hex'); the others
// use the X'…' form.
func TestValueLiteralBytesDivergesByDialect(t *testing.T) {
	value := ResultValue{Kind: ValueKindBytes, Value: []byte{0xde, 0xad}}

	tests := []struct {
		dialect Dialect
		want    string
	}{
		{SQLiteDialect(), "X'dead'"},
		{PostgresDialect(), "decode('dead', 'hex')"},
		{MySQLDialect(), "X'dead'"},
	}

	for _, tt := range tests {
		t.Run(tt.dialect.Name(), func(t *testing.T) {
			if got := tt.dialect.ValueLiteral(value); got != tt.want {
				t.Fatalf("ValueLiteral(bytes) = %q, want %q", got, tt.want)
			}
		})
	}
}
