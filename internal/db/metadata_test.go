package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

func TestPostgresMetadataHelpers(t *testing.T) {
	runner := &metadataRunner{responses: []metadataResponse{
		{
			query: "SELECT table_catalog, table_schema, table_name, table_type FROM information_schema.tables WHERE table_type IN ('BASE TABLE', 'VIEW') AND table_schema = $1 AND table_catalog = $2 ORDER BY table_schema, table_name",
			args:  []any{"public", "appdb"},
			rows: metadataRows{
				values: [][]any{{"appdb", "public", "widgets", "BASE TABLE"}, {"appdb", "public", "widget_rollup", "VIEW"}},
			},
		},
		{
			query: "SELECT a.attname, a.attnum, pg_catalog.format_type(a.atttypid, a.atttypmod), NOT a.attnotnull, pg_catalog.pg_get_expr(ad.adbin, ad.adrelid) FROM pg_catalog.pg_attribute AS a JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace LEFT JOIN pg_catalog.pg_attrdef AS ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum",
			args:  []any{"public", "widgets"},
			rows: metadataRows{
				values: [][]any{{"id", 1, "integer", false, "nextval('widgets_id_seq'::regclass)"}, {"name", 2, "text", true, nil}},
			},
		},
		{
			query: "SELECT tc.constraint_name, kcu.column_name, kcu.ordinal_position FROM information_schema.table_constraints AS tc JOIN information_schema.key_column_usage AS kcu ON tc.constraint_catalog = kcu.constraint_catalog AND tc.constraint_schema = kcu.constraint_schema AND tc.constraint_name = kcu.constraint_name WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1 AND tc.table_name = $2 ORDER BY kcu.ordinal_position",
			args:  []any{"public", "widgets"},
			rows: metadataRows{
				values: [][]any{{"widgets_pkey", "id", 1}},
			},
		},
	}}

	metadata := postgresMetadata{runner: runner}
	ctx := context.Background()

	tables, err := metadata.Tables(ctx, TableFilter{Catalog: "appdb", Schema: "public"})
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}

	if got, want := tables, []Table{{Catalog: "appdb", Schema: "public", Name: "widgets", Type: "table"}, {Catalog: "appdb", Schema: "public", Name: "widget_rollup", Type: "view"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Tables() = %#v, want %#v", got, want)
	}

	columns, err := metadata.Columns(ctx, TableRef{Schema: "public", Name: "widgets"})
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}

	if got, want := columns, []Column{{Name: "id", Position: 1, Type: "integer", Nullable: false, DefaultValue: stringPointer("nextval('widgets_id_seq'::regclass)")}, {Name: "name", Position: 2, Type: "text", Nullable: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}

	primaryKeys, err := metadata.PrimaryKeys(ctx, TableRef{Schema: "public", Name: "widgets"})
	if err != nil {
		t.Fatalf("PrimaryKeys() error = %v", err)
	}

	if got, want := primaryKeys, []PrimaryKey{{Name: "widgets_pkey", Column: "id", Position: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrimaryKeys() = %#v, want %#v", got, want)
	}

	types, err := metadata.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}

	if !containsType(types, TypeInfo{Schema: "pg_catalog", Name: "text"}) || !containsType(types, TypeInfo{Schema: "pg_catalog", Name: "uuid"}) {
		t.Fatalf("Types() = %#v, want text and uuid entries", types)
	}

	types[0].Name = "mutated"
	again, err := metadata.Types(ctx)
	if err != nil {
		t.Fatalf("Types() second call error = %v", err)
	}

	if got, want := again[0].Name, postgresTypeInfo[0].Name; got != want {
		t.Fatalf("Types() returned shared slice, got %q want %q", got, want)
	}
}

func TestMySQLMetadataHelpers(t *testing.T) {
	runner := &metadataRunner{responses: []metadataResponse{
		{
			query: "SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = ? AND table_type IN ('BASE TABLE', 'VIEW') ORDER BY table_name",
			args:  []any{"warehouse"},
			rows: metadataRows{
				values: [][]any{{"warehouse", "widgets", "BASE TABLE"}, {"warehouse", "widget_rollup", "VIEW"}},
			},
		},
		{
			query: "SELECT column_name, ordinal_position, column_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position",
			args:  []any{"warehouse", "widgets"},
			rows: metadataRows{
				values: [][]any{{"id", 1, "bigint unsigned", "NO", nil}, {"name", 2, "varchar(255)", "YES", "guest"}},
			},
		},
		{
			query: "SELECT constraint_name, column_name, ordinal_position FROM information_schema.key_column_usage WHERE table_schema = ? AND table_name = ? AND constraint_name = 'PRIMARY' ORDER BY ordinal_position",
			args:  []any{"warehouse", "widgets"},
			rows: metadataRows{
				values: [][]any{{"PRIMARY", "id", 1}},
			},
		},
	}}

	metadata := mysqlMetadata{runner: runner, database: "warehouse"}
	ctx := context.Background()

	tables, err := metadata.Tables(ctx, TableFilter{})
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}

	if got, want := tables, []Table{{Schema: "warehouse", Name: "widgets", Type: "table"}, {Schema: "warehouse", Name: "widget_rollup", Type: "view"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Tables() = %#v, want %#v", got, want)
	}

	columns, err := metadata.Columns(ctx, TableRef{Name: "widgets"})
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}

	if got, want := columns, []Column{{Name: "id", Position: 1, Type: "bigint unsigned", Nullable: false}, {Name: "name", Position: 2, Type: "varchar(255)", Nullable: true, DefaultValue: stringPointer("guest")}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}

	primaryKeys, err := metadata.PrimaryKeys(ctx, TableRef{Name: "widgets"})
	if err != nil {
		t.Fatalf("PrimaryKeys() error = %v", err)
	}

	if got, want := primaryKeys, []PrimaryKey{{Name: "PRIMARY", Column: "id", Position: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrimaryKeys() = %#v, want %#v", got, want)
	}

	types, err := metadata.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}

	if !containsType(types, TypeInfo{Name: "json"}) || !containsType(types, TypeInfo{Name: "varchar"}) {
		t.Fatalf("Types() = %#v, want json and varchar entries", types)
	}

	types[0].Name = "mutated"
	again, err := metadata.Types(ctx)
	if err != nil {
		t.Fatalf("Types() second call error = %v", err)
	}

	if got, want := again[0].Name, mysqlTypeInfo[0].Name; got != want {
		t.Fatalf("Types() returned shared slice, got %q want %q", got, want)
	}
}

type metadataRunner struct {
	responses []metadataResponse
	index     int
}

func (*metadataRunner) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext() should not be called")
}

func (r *metadataRunner) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	if r.index >= len(r.responses) {
		return nil, fmt.Errorf("unexpected query %q", query)
	}

	response := r.responses[r.index]
	r.index++

	if got, want := query, response.query; got != want {
		return nil, fmt.Errorf("query = %q, want %q", got, want)
	}

	if got, want := args, response.args; !reflect.DeepEqual(got, want) {
		return nil, fmt.Errorf("args = %#v, want %#v", got, want)
	}

	rows := response.rows
	return &rows, nil
}

func (*metadataRunner) QueryRowContext(context.Context, string, ...any) Row {
	return stubRow{}
}

type metadataResponse struct {
	query string
	args  []any
	rows  metadataRows
}

type metadataRows struct {
	values [][]any
	index  int
}

func (metadataRows) Close() error                            { return nil }
func (metadataRows) Columns() ([]string, error)              { return nil, nil }
func (metadataRows) ColumnTypes() ([]*sql.ColumnType, error) { return nil, nil }
func (metadataRows) Err() error                              { return nil }

func (r *metadataRows) Next() bool {
	return r.index < len(r.values)
}

func (r *metadataRows) Scan(dest ...any) error {
	if r.index >= len(r.values) {
		return fmt.Errorf("Scan() called with no remaining rows")
	}

	row := r.values[r.index]
	r.index++
	if len(dest) != len(row) {
		return fmt.Errorf("Scan() got %d destinations, want %d", len(dest), len(row))
	}

	for i := range dest {
		if err := assignMetadataValue(dest[i], row[i]); err != nil {
			return fmt.Errorf("column %d: %w", i, err)
		}
	}

	return nil
}

func assignMetadataValue(dest any, value any) error {
	switch ptr := dest.(type) {
	case *string:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("value type = %T, want string", value)
		}
		*ptr = text
		return nil
	case *int:
		number, ok := value.(int)
		if !ok {
			return fmt.Errorf("value type = %T, want int", value)
		}
		*ptr = number
		return nil
	case *bool:
		flag, ok := value.(bool)
		if !ok {
			return fmt.Errorf("value type = %T, want bool", value)
		}
		*ptr = flag
		return nil
	case *sql.NullString:
		if value == nil {
			*ptr = sql.NullString{}
			return nil
		}

		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("value type = %T, want string or nil", value)
		}

		*ptr = sql.NullString{String: text, Valid: true}
		return nil
	default:
		return fmt.Errorf("unsupported destination type %T", dest)
	}
}
