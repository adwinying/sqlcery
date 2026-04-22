package app

import (
	"database/sql"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestComposeRecordViewerInsertSQLBuildsDialectAwareSQL(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		source  *db.TableRef
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			source:  &db.TableRef{Name: "widgets"},
			want: []string{
				`INSERT INTO "widgets"`,
				`"id"`,
				`"payload"`,
				"7",
				"X'dead'",
			},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			source:  &db.TableRef{Schema: "public", Name: "widgets"},
			want: []string{
				`INSERT INTO "public"."widgets"`,
				`"id"`,
				`"payload"`,
				"7",
				"decode('dead', 'hex')",
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			source:  &db.TableRef{Schema: "warehouse", Name: "widgets"},
			want: []string{
				"INSERT INTO `warehouse`.`widgets`",
				"`id`",
				"`payload`",
				"7",
				"X'dead'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := composeRecordViewerInsertSQL(tt.dialect, &LatestResultContext{
				Query: "select id, payload from widgets;",
				PreservedResult: &db.ResultSet{
					Source: tt.source,
					Columns: []db.ResultColumn{
						{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
						{Name: "payload"},
					},
					Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
				},
			}, 0)
			if err != nil {
				t.Fatalf("composeRecordViewerInsertSQL() error = %v", err)
			}
			if got, want := result.Action, recordViewerComposeActionInsert; got != want {
				t.Fatalf("Action = %q, want %q", got, want)
			}
			for _, want := range tt.want {
				if !containsLine(result.SQL, want) {
					t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
				}
			}
		})
	}
}

func TestComposeRecordViewerUpdateSQLBuildsDialectAwareSQL(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		source  *db.TableRef
		want    []string
	}{
		{
			name:    "sqlite",
			dialect: db.SQLiteDialect(),
			source:  &db.TableRef{Name: "widgets"},
			want: []string{
				`UPDATE "widgets"`,
				`"payload" = X'dead'`,
				`"id" = 7`,
			},
		},
		{
			name:    "postgres",
			dialect: db.PostgresDialect(),
			source:  &db.TableRef{Schema: "public", Name: "widgets"},
			want: []string{
				`UPDATE "public"."widgets"`,
				`"payload" = decode('dead', 'hex')`,
				`"id" = 7`,
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			source:  &db.TableRef{Schema: "warehouse", Name: "widgets"},
			want: []string{
				"UPDATE `warehouse`.`widgets`",
				"`payload` = X'dead'",
				"`id` = 7",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := composeRecordViewerUpdateSQL(tt.dialect, &LatestResultContext{
				Query: "select id, payload from widgets;",
				PreservedResult: &db.ResultSet{
					Source: tt.source,
					Columns: []db.ResultColumn{
						{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
						{Name: "payload"},
					},
					Rows: []db.ResultRow{{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(7)}, {Kind: db.ValueKindBytes, Value: []byte{0xde, 0xad}}}}},
				},
			}, 0)
			if err != nil {
				t.Fatalf("composeRecordViewerUpdateSQL() error = %v", err)
			}
			if got, want := result.Action, recordViewerComposeActionUpdate; got != want {
				t.Fatalf("Action = %q, want %q", got, want)
			}
			if !result.UsedPrimaryKeys {
				t.Fatal("UsedPrimaryKeys = false, want true")
			}
			for _, want := range tt.want {
				if !containsLine(result.SQL, want) {
					t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
				}
			}
		})
	}
}

func TestRecordViewerValueLiteralFormatsTimestamps(t *testing.T) {
	pst := time.FixedZone("PST", -8*60*60)

	type pgTimestamp struct {
		Time  time.Time
		Valid bool
	}

	tests := []struct {
		name  string
		value db.ResultValue
		want  string
	}{
		{
			name:  "utc time.Time with microseconds",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 123456000, time.UTC)},
			want:  "'2026-04-22 10:30:45.123456+00:00'",
		},
		{
			name:  "time.Time with fixed negative offset",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 0, pst)},
			want:  "'2026-04-22 10:30:45-08:00'",
		},
		{
			name:  "time.Time with zero fractional seconds omits fraction",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC)},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "nil value becomes NULL",
			value: db.ResultValue{Kind: db.ValueKindNull},
			want:  "NULL",
		},
		{
			name:  "time kind with nil value falls back to NULL",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: nil},
			want:  "NULL",
		},
		{
			name:  "sql.NullTime valid round-trips",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: sql.NullTime{Time: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC), Valid: true}},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "sql.NullTime invalid becomes NULL",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: sql.NullTime{Valid: false}},
			want:  "NULL",
		},
		{
			name:  "pgtype-like struct valid",
			value: db.ResultValue{Kind: db.ValueKindUnknown, Value: pgTimestamp{Time: time.Date(2026, time.April, 22, 10, 30, 45, 0, time.UTC), Valid: true}},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
		{
			name:  "pgtype-like struct invalid becomes NULL",
			value: db.ResultValue{Kind: db.ValueKindUnknown, Value: pgTimestamp{Valid: false}},
			want:  "NULL",
		},
		{
			name:  "timestamp string is reformatted",
			value: db.ResultValue{Kind: db.ValueKindTime, Value: "2026-04-22T10:30:45Z"},
			want:  "'2026-04-22 10:30:45+00:00'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordViewerValueLiteral(db.PostgresDialect(), tt.value)
			if got != tt.want {
				t.Fatalf("recordViewerValueLiteral() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeRecordViewerUpdateSQLQuotesTimestampColumns(t *testing.T) {
	result, err := composeRecordViewerUpdateSQL(db.PostgresDialect(), &LatestResultContext{
		Query: "select id, updated_at from widgets;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Schema: "public", Name: "widgets"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "updated_at", DatabaseType: "TIMESTAMPTZ"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(7)},
				{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 123456000, time.UTC)},
			}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeRecordViewerUpdateSQL() error = %v", err)
	}
	want := `"updated_at" = '2026-04-22 10:30:45.123456+00:00'`
	if !containsLine(result.SQL, want) {
		t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
	}
}
