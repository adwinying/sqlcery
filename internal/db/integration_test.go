package db

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSQLiteAdapterIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databasePath := filepath.Join(t.TempDir(), "integration.db")
	adapter := openIntegrationAdapter(t, ctx, config.Connection{
		Type:     "sqlite",
		Database: databasePath,
	})

	runAdapterIntegrationSuite(t, ctx, adapter, adapterIntegrationExpectation{
		typeName:         "sqlite",
		schema:           "main",
		columnIDType:     "INTEGER",
		columnIDNullable: true,
		columnNameType:   "TEXT",
		columnPriceType:  "REAL",
		defaultValue:     "'draft'",
		nameValueKind:    ValueKindString,
		priceValueKind:   ValueKindFloat,
		priceValue:       7.5,
		typeChecks: []TypeInfo{
			{Name: "integer"},
			{Name: "text"},
		},
	})
}

func TestPostgresAdapterIntegration(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithDatabase("sqlcery"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("password"),
	)
	if err != nil {
		t.Fatalf("postgres.Run() error = %v", err)
	}
	testcontainers.CleanupContainer(t, container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container.Host() error = %v", err)
	}

	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort() error = %v", err)
	}

	adapter := openIntegrationAdapter(t, ctx, config.Connection{
		Type:     "postgres",
		Host:     host,
		Port:     port.Int(),
		Database: "sqlcery",
		Username: "postgres",
		Password: "password",
	})

	runAdapterIntegrationSuite(t, ctx, adapter, adapterIntegrationExpectation{
		typeName:         "postgres",
		schema:           "public",
		columnIDType:     "integer",
		columnIDNullable: false,
		columnNameType:   "text",
		columnPriceType:  "numeric(10,2)",
		defaultValue:     "'draft'::text",
		nameValueKind:    ValueKindString,
		priceValueKind:   ValueKindDecimal,
		priceValue:       "7.50",
		typeChecks: []TypeInfo{
			{Schema: "pg_catalog", Name: "text"},
			{Schema: "pg_catalog", Name: "uuid"},
		},
	})
}

func TestMySQLAdapterIntegration(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcmysql.Run(ctx,
		"mysql:8.0.36",
		tcmysql.WithDatabase("sqlcery"),
		tcmysql.WithUsername("app"),
		tcmysql.WithPassword("password"),
	)
	if err != nil {
		t.Fatalf("mysql.Run() error = %v", err)
	}
	testcontainers.CleanupContainer(t, container)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container.Host() error = %v", err)
	}

	port, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatalf("container.MappedPort() error = %v", err)
	}

	adapter := openIntegrationAdapter(t, ctx, config.Connection{
		Type:     "mysql",
		Host:     host,
		Port:     port.Int(),
		Database: "sqlcery",
		Username: "app",
		Password: "password",
	})

	runAdapterIntegrationSuite(t, ctx, adapter, adapterIntegrationExpectation{
		typeName:         "mysql",
		schema:           "sqlcery",
		columnIDType:     "int",
		columnIDNullable: false,
		columnNameType:   "varchar(64)",
		columnPriceType:  "decimal(10,2)",
		defaultValue:     "draft",
		nameValueKind:    ValueKindString,
		priceValueKind:   ValueKindDecimal,
		priceValue:       "7.50",
		typeChecks: []TypeInfo{
			{Name: "json"},
			{Name: "varchar"},
		},
	})
}

type adapterIntegrationExpectation struct {
	typeName         string
	schema           string
	columnIDType     string
	columnIDNullable bool
	columnNameType   string
	columnPriceType  string
	defaultValue     string
	nameValueKind    ValueKind
	priceValueKind   ValueKind
	priceValue       any
	typeChecks       []TypeInfo
}

func openIntegrationAdapter(t *testing.T, ctx context.Context, connection config.Connection) *SQLAdapter {
	t.Helper()

	adapter, err := Open(ctx, connection)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	t.Cleanup(func() {
		if err := adapter.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return adapter
}

func runAdapterIntegrationSuite(t *testing.T, ctx context.Context, adapter *SQLAdapter, expect adapterIntegrationExpectation) {
	t.Helper()

	if got := adapter.Dialect().Name(); got != expect.typeName {
		t.Fatalf("adapter.Dialect().Name() = %q, want %q", got, expect.typeName)
	}

	createTableStatement, insertStatement, countStatement := integrationStatements(expect.typeName)
	if _, err := adapter.ExecContext(ctx, "drop table if exists widgets"); err != nil {
		t.Fatalf("ExecContext(drop table) error = %v", err)
	}
	if _, err := adapter.ExecContext(ctx, createTableStatement); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}
	if _, err := adapter.ExecContext(ctx, insertStatement, "gizmo", 7.5); err != nil {
		t.Fatalf("ExecContext(insert) error = %v", err)
	}
	if _, err := adapter.ExecContext(ctx, insertStatement, "widget", 9.25); err != nil {
		t.Fatalf("ExecContext(insert second) error = %v", err)
	}

	var count int
	if err := adapter.QueryRowContext(ctx, countStatement, "gizmo").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}
	if got, want := count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}

	tables, err := adapter.Tables(ctx, TableFilter{Schema: expect.schema})
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}
	if !containsTableByIdentity(tables, expect.schema, "widgets", "table") {
		t.Fatalf("Tables() = %#v, want widgets table", tables)
	}

	columns, err := adapter.Columns(ctx, TableRef{Schema: expect.schema, Name: "widgets"})
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}
	if got, want := columns, []Column{
		{Name: "id", Position: 1, Type: expect.columnIDType, Nullable: expect.columnIDNullable},
		{Name: "name", Position: 2, Type: expect.columnNameType, Nullable: false},
		{Name: "price", Position: 3, Type: expect.columnPriceType, Nullable: false},
		{Name: "status", Position: 4, Type: expect.columnNameType, Nullable: true, DefaultValue: stringPointer(expect.defaultValue)},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}

	primaryKeys, err := adapter.PrimaryKeys(ctx, TableRef{Schema: expect.schema, Name: "widgets"})
	if err != nil {
		t.Fatalf("PrimaryKeys() error = %v", err)
	}
	if got, want := primaryKeys, []PrimaryKey{{Name: integrationPrimaryKeyName(expect.typeName), Column: "id", Position: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrimaryKeys() = %#v, want %#v", got, want)
	}

	types, err := adapter.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}
	for _, want := range expect.typeChecks {
		if !containsType(types, want) {
			t.Fatalf("Types() = %#v, want %#v entry", types, want)
		}
	}

	result, err := adapter.QueryResultContext(ctx, queryWidgetsStatement(expect.typeName), ResultOptions{
		Source: &TableRef{Schema: expect.schema, Name: "widgets"},
	})
	if err != nil {
		t.Fatalf("QueryResultContext() error = %v", err)
	}
	if got, want := len(result.Rows), 2; got != want {
		t.Fatalf("len(result.Rows) = %d, want %d", got, want)
	}
	if result.Columns[0].PrimaryKey == nil || result.Columns[0].PrimaryKey.Column != "id" {
		t.Fatalf("result.Columns[0].PrimaryKey = %#v, want id primary key", result.Columns[0].PrimaryKey)
	}
	if result.Columns[0].Schema == nil || result.Columns[0].Schema.Type != expect.columnIDType {
		t.Fatalf("result.Columns[0].Schema = %#v, want id schema type %q", result.Columns[0].Schema, expect.columnIDType)
	}
	assertResultValue(t, result.Rows[0].Values[0], ValueKindInteger, int64(1))
	assertResultValue(t, result.Rows[0].Values[1], expect.nameValueKind, "gizmo")
	assertResultValue(t, result.Rows[0].Values[2], expect.priceValueKind, expect.priceValue)
	assertResultValue(t, result.Rows[0].Values[3], ValueKindString, "draft")

	statementResult, err := adapter.ExecuteStatementContext(ctx, queryWidgetsStatement(expect.typeName), ResultOptions{
		Source: &TableRef{Schema: expect.schema, Name: "widgets"},
	})
	if err != nil {
		t.Fatalf("ExecuteStatementContext(query) error = %v", err)
	}
	if got, want := statementResult.Kind, StatementResultKindQuery; got != want {
		t.Fatalf("statementResult.Kind = %q, want %q", got, want)
	}
	if statementResult.ResultSet == nil {
		t.Fatal("statementResult.ResultSet = nil, want result set")
	}

	updateStatement, updateArgs := integrationUpdate(expect.typeName)
	execResult, err := adapter.ExecuteStatementContext(ctx, updateStatement, ResultOptions{}, updateArgs...)
	if err != nil {
		t.Fatalf("ExecuteStatementContext(update) error = %v", err)
	}
	if got, want := execResult.Kind, StatementResultKindExec; got != want {
		t.Fatalf("execResult.Kind = %q, want %q", got, want)
	}
	if execResult.RowsAffected == nil || *execResult.RowsAffected != 1 {
		t.Fatalf("execResult.RowsAffected = %#v, want 1", execResult.RowsAffected)
	}

	var status string
	statusQuery, statusArgs := integrationStatusQuery(expect.typeName)
	if err := adapter.QueryRowContext(ctx, statusQuery, statusArgs...).Scan(&status); err != nil {
		t.Fatalf("QueryRowContext(status).Scan() error = %v", err)
	}
	if got, want := status, "live"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}

	if err := adapter.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func integrationStatements(dialect string) (createTable string, insert string, count string) {
	switch dialect {
	case "postgres":
		return "create table widgets (id integer generated always as identity primary key, name text not null, price numeric(10,2) not null, status text default 'draft')", "insert into widgets (name, price) values ($1, $2)", "select count(*) from widgets where name = $1"
	case "mysql":
		return "create table widgets (id int not null auto_increment primary key, name varchar(64) not null, price decimal(10,2) not null, status varchar(64) default 'draft')", "insert into widgets (name, price) values (?, ?)", "select count(*) from widgets where name = ?"
	default:
		return "create table widgets (id integer primary key, name text not null, price real not null, status text default 'draft')", "insert into widgets (name, price) values (?, ?)", "select count(*) from widgets where name = ?"
	}
}

func integrationPrimaryKeyName(dialect string) string {
	if dialect == "mysql" {
		return "PRIMARY"
	}
	if dialect == "postgres" {
		return "widgets_pkey"
	}
	return ""
}

func queryWidgetsStatement(string) string {
	return "select id, name, price, coalesce(status, 'draft') as status from widgets order by id"
}

func integrationUpdate(dialect string) (string, []any) {
	if dialect == "postgres" {
		return "update widgets set status = $1 where id = $2", []any{"live", 1}
	}
	return "update widgets set status = ? where id = ?", []any{"live", 1}
}

func integrationStatusQuery(dialect string) (string, []any) {
	if dialect == "postgres" {
		return "select status from widgets where id = $1", []any{1}
	}
	return "select status from widgets where id = ?", []any{1}
}

func containsTableByIdentity(tables []Table, schema string, name string, tableType string) bool {
	for _, table := range tables {
		if table.Schema == schema && table.Name == name && table.Type == tableType {
			return true
		}
	}

	return false
}
