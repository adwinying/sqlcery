package app

import (
	"database/sql"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/db"
)

func TestComposeResultsPaneInsertSQLBuildsDialectAwareSQL(t *testing.T) {
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
			source:  &db.TableRef{Namespace: "public", Name: "widgets"},
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
			source:  &db.TableRef{Namespace: "warehouse", Name: "widgets"},
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
			result, err := composeResultsPaneInsertSQL(tt.dialect, &LatestResultContext{
				Statement: "select id, payload from widgets;",
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
				t.Fatalf("composeResultsPaneInsertSQL() error = %v", err)
			}
			if got, want := result.Action, resultsPaneComposeActionInsert; got != want {
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

func TestComposeResultsPaneUpdateSQLBuildsDialectAwareSQL(t *testing.T) {
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
			source:  &db.TableRef{Namespace: "public", Name: "widgets"},
			want: []string{
				`UPDATE "public"."widgets"`,
				`"payload" = decode('dead', 'hex')`,
				`"id" = 7`,
			},
		},
		{
			name:    "mysql",
			dialect: db.MySQLDialect(),
			source:  &db.TableRef{Namespace: "warehouse", Name: "widgets"},
			want: []string{
				"UPDATE `warehouse`.`widgets`",
				"`payload` = X'dead'",
				"`id` = 7",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := composeResultsPaneUpdateSQL(tt.dialect, &LatestResultContext{
				Statement: "select id, payload from widgets;",
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
				t.Fatalf("composeResultsPaneUpdateSQL() error = %v", err)
			}
			if got, want := result.Action, resultsPaneComposeActionUpdate; got != want {
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

func TestResultsPaneValueLiteralFormatsTimestamps(t *testing.T) {
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
			got := resultsPaneValueLiteral(db.PostgresDialect(), tt.value)
			if got != tt.want {
				t.Fatalf("resultsPaneValueLiteral() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeResultsPaneUpdateSQLQuotesTimestampColumns(t *testing.T) {
	result, err := composeResultsPaneUpdateSQL(db.PostgresDialect(), &LatestResultContext{
		Statement: "select id, updated_at from widgets;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Namespace: "public", Name: "widgets"},
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "updated_at", DriverColumnType: "TIMESTAMPTZ"},
			},
			Rows: []db.ResultRow{{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(7)},
				{Kind: db.ValueKindTime, Value: time.Date(2026, time.April, 22, 10, 30, 45, 123456000, time.UTC)},
			}}},
		},
	}, 0)
	if err != nil {
		t.Fatalf("composeResultsPaneUpdateSQL() error = %v", err)
	}
	want := `"updated_at" = '2026-04-22 10:30:45.123456+00:00'`
	if !containsLine(result.SQL, want) {
		t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
	}
}

func bulkWidgetLatest(source *db.TableRef) *LatestResultContext {
	return &LatestResultContext{
		Statement: "select id, name from widgets;",
		PreservedResult: &db.ResultSet{
			Source: source,
			Columns: []db.ResultColumn{
				{Name: "id", PrimaryKey: &db.PrimaryKey{Column: "id", Position: 1}},
				{Name: "name"},
			},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindString, Value: "Alice"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindString, Value: "Bob"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(3)}, {Kind: db.ValueKindString, Value: "Carol"}}},
			},
		},
	}
}

func TestComposeResultsPaneInsertBulkSQLMultipleRows(t *testing.T) {
	latest := bulkWidgetLatest(&db.TableRef{Name: "widgets"})
	result, err := composeResultsPaneInsertBulkSQL(db.SQLiteDialect(), latest, []int{2, 0, 1})
	if err != nil {
		t.Fatalf("composeResultsPaneInsertBulkSQL() error = %v", err)
	}
	if result.Count != 3 {
		t.Fatalf("Count = %d, want 3", result.Count)
	}
	if result.Action != resultsPaneComposeActionInsert {
		t.Fatalf("Action = %q, want INSERT", result.Action)
	}
	wantContains := []string{
		`INSERT INTO "widgets"`,
		`"id"`,
		`"name"`,
		`(1, 'Alice')`,
		`(2, 'Bob')`,
		`(3, 'Carol')`,
	}
	for _, want := range wantContains {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
	// rows must appear in result-set order (0, 1, 2) not mark order (2, 0, 1)
	alicePos := indexOf(result.SQL, "Alice")
	bobPos := indexOf(result.SQL, "Bob")
	carolPos := indexOf(result.SQL, "Carol")
	if !(alicePos < bobPos && bobPos < carolPos) {
		t.Fatalf("rows not in result-set order: Alice=%d Bob=%d Carol=%d", alicePos, bobPos, carolPos)
	}
}

func TestComposeResultsPaneUpdateBulkSQLMultipleRows(t *testing.T) {
	latest := bulkWidgetLatest(&db.TableRef{Name: "widgets"})
	result, err := composeResultsPaneUpdateBulkSQL(db.SQLiteDialect(), latest, []int{0, 2})
	if err != nil {
		t.Fatalf("composeResultsPaneUpdateBulkSQL() error = %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("Count = %d, want 2", result.Count)
	}
	if result.Action != resultsPaneComposeActionUpdate {
		t.Fatalf("Action = %q, want UPDATE", result.Action)
	}
	wantContains := []string{
		`UPDATE "widgets"`,
		`"name" = 'Alice'`,
		`"id" = 1`,
		`"name" = 'Carol'`,
		`"id" = 3`,
	}
	for _, want := range wantContains {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestComposeResultsPaneDeleteBulkSQLSinglePKUsesIN(t *testing.T) {
	latest := bulkWidgetLatest(&db.TableRef{Name: "widgets"})
	result, err := composeResultsPaneDeleteBulkSQL(db.SQLiteDialect(), latest, []int{0, 2})
	if err != nil {
		t.Fatalf("composeResultsPaneDeleteBulkSQL() error = %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("Count = %d, want 2", result.Count)
	}
	if result.Action != resultsPaneComposeActionDelete {
		t.Fatalf("Action = %q, want DELETE", result.Action)
	}
	wantContains := []string{
		`DELETE FROM "widgets"`,
		`"id" IN (1, 3)`,
	}
	for _, want := range wantContains {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func TestComposeResultsPaneDeleteBulkSQLCompositePKUsesOR(t *testing.T) {
	latest := &LatestResultContext{
		Statement: "select user_id, org_id, name from memberships;",
		PreservedResult: &db.ResultSet{
			Source: &db.TableRef{Name: "memberships"},
			Columns: []db.ResultColumn{
				{Name: "user_id", PrimaryKey: &db.PrimaryKey{Column: "user_id", Position: 1}},
				{Name: "org_id", PrimaryKey: &db.PrimaryKey{Column: "org_id", Position: 2}},
				{Name: "name"},
			},
			Rows: []db.ResultRow{
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(1)}, {Kind: db.ValueKindInteger, Value: int64(10)}, {Kind: db.ValueKindString, Value: "admin"}}},
				{Values: []db.ResultValue{{Kind: db.ValueKindInteger, Value: int64(2)}, {Kind: db.ValueKindInteger, Value: int64(10)}, {Kind: db.ValueKindString, Value: "member"}}},
			},
		},
	}
	result, err := composeResultsPaneDeleteBulkSQL(db.SQLiteDialect(), latest, []int{0, 1})
	if err != nil {
		t.Fatalf("composeResultsPaneDeleteBulkSQL() error = %v", err)
	}
	wantContains := []string{
		`DELETE FROM "memberships"`,
		`"user_id" = 1`,
		`"org_id" = 10`,
		`"user_id" = 2`,
		`OR`,
	}
	for _, want := range wantContains {
		if !containsLine(result.SQL, want) {
			t.Fatalf("SQL = %q, want to contain %q", result.SQL, want)
		}
	}
}

func indexOf(s, substr string) int {
	for i := range s {
		if len(s)-i >= len(substr) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
