package app

import (
	"testing"

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
