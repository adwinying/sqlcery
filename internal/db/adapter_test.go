package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewAdapterDelegatesQueryExecutionAndMetadata(t *testing.T) {
	ctx := context.Background()
	rows := stubRows{}
	runner := &stubRunner{
		execResult: stubResult(3),
		queryRows:  rows,
		queryRow:   stubRow{value: "sqlite"},
	}
	metadata := &stubMetadataProvider{
		tables:      []Table{{Namespace: "main", Name: "widgets", Type: "table"}},
		columns:     []Column{{Name: "id", Position: 1, Type: "integer"}},
		primaryKeys: []PrimaryKey{{Column: "id", Position: 1}},
		types:       []TypeInfo{{Name: "integer"}},
	}

	adapter, err := newAdapter(runner, SQLiteDialect(), metadata, func(context.Context) error { return nil }, func() error { return nil })
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	result, err := adapter.ExecContext(ctx, "update widgets set name = ? where id = ?", "gizmo", 7)
	if err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() error = %v", err)
	}

	if got, want := affected, int64(3); got != want {
		t.Fatalf("RowsAffected() = %d, want %d", got, want)
	}

	queriedRows, err := adapter.QueryContext(ctx, "select id from widgets where id = ?", 7)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}

	if !reflect.DeepEqual(queriedRows, rows) {
		t.Fatalf("QueryContext() rows = %#v, want %#v", queriedRows, rows)
	}

	var engine string
	if err := adapter.QueryRowContext(ctx, "select ?", "sqlite").Scan(&engine); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}

	if got, want := engine, "sqlite"; got != want {
		t.Fatalf("engine = %q, want %q", got, want)
	}

	tables, err := adapter.Tables(ctx, TableFilter{Namespace: "main"})
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}

	if !reflect.DeepEqual(tables, metadata.tables) {
		t.Fatalf("Tables() = %#v, want %#v", tables, metadata.tables)
	}

	columns, err := adapter.Columns(ctx, TableRef{Namespace: "main", Name: "widgets"})
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}

	if !reflect.DeepEqual(columns, metadata.columns) {
		t.Fatalf("Columns() = %#v, want %#v", columns, metadata.columns)
	}

	primaryKeys, err := adapter.PrimaryKeys(ctx, TableRef{Namespace: "main", Name: "widgets"})
	if err != nil {
		t.Fatalf("PrimaryKeys() error = %v", err)
	}

	if !reflect.DeepEqual(primaryKeys, metadata.primaryKeys) {
		t.Fatalf("PrimaryKeys() = %#v, want %#v", primaryKeys, metadata.primaryKeys)
	}

	types, err := adapter.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}

	if !reflect.DeepEqual(types, metadata.types) {
		t.Fatalf("Types() = %#v, want %#v", types, metadata.types)
	}

	if got, want := runner.execCalls, []stubCall{{query: "update widgets set name = ? where id = ?", args: []any{"gizmo", 7}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.execCalls = %#v, want %#v", got, want)
	}

	if got, want := runner.queryCalls, []stubCall{{query: "select id from widgets where id = ?", args: []any{7}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.queryCalls = %#v, want %#v", got, want)
	}

	if got, want := runner.queryRowCalls, []stubCall{{query: "select ?", args: []any{"sqlite"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.queryRowCalls = %#v, want %#v", got, want)
	}

	if got, want := metadata.tableFilters, []TableFilter{{Namespace: "main"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata.tableFilters = %#v, want %#v", got, want)
	}

	if got, want := metadata.tableRefs, []TableRef{{Namespace: "main", Name: "widgets"}, {Namespace: "main", Name: "widgets"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata.tableRefs = %#v, want %#v", got, want)
	}

	if got, want := metadata.typeCalls, 1; got != want {
		t.Fatalf("metadata.typeCalls = %d, want %d", got, want)
	}
}

func TestNewAdapterDefaultsToUnsupportedMetadata(t *testing.T) {
	adapter, err := newAdapter(&stubRunner{}, SQLiteDialect(), nil, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	_, err = adapter.Tables(context.Background(), TableFilter{})
	if !errors.Is(err, ErrMetadataUnsupported) {
		t.Fatalf("Tables() error = %v, want %v", err, ErrMetadataUnsupported)
	}

	_, err = adapter.PrimaryKeys(context.Background(), TableRef{Name: "widgets"})
	if !errors.Is(err, ErrMetadataUnsupported) {
		t.Fatalf("PrimaryKeys() error = %v, want %v", err, ErrMetadataUnsupported)
	}

	_, err = adapter.Types(context.Background())
	if !errors.Is(err, ErrMetadataUnsupported) {
		t.Fatalf("Types() error = %v, want %v", err, ErrMetadataUnsupported)
	}
}

func TestAdapterColumnsValidatesTableName(t *testing.T) {
	adapter, err := newAdapter(&stubRunner{}, SQLiteDialect(), &stubMetadataProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	_, err = adapter.Columns(context.Background(), TableRef{Namespace: "main"})
	if err == nil {
		t.Fatal("Columns() error = nil, want error")
	}

	if got, want := err.Error(), "table name is required"; got != want {
		t.Fatalf("Columns() error = %q, want %q", got, want)
	}
}

func TestAdapterPrimaryKeysValidatesTableName(t *testing.T) {
	adapter, err := newAdapter(&stubRunner{}, SQLiteDialect(), &stubMetadataProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	_, err = adapter.PrimaryKeys(context.Background(), TableRef{Namespace: "main"})
	if err == nil {
		t.Fatal("PrimaryKeys() error = nil, want error")
	}

	if got, want := err.Error(), "table name is required"; got != want {
		t.Fatalf("PrimaryKeys() error = %q, want %q", got, want)
	}
}

func TestQueryResultContextContinuesWithoutMetadataSupport(t *testing.T) {
	ctx := context.Background()
	runner := &stubRunner{
		queryRows: &scriptedRows{
			columns: []string{"id", "name"},
			values:  [][]any{{int64(7), []byte("gizmo")}},
		},
	}

	adapter, err := newAdapter(runner, SQLiteDialect(), nil, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	result, err := adapter.QueryResultContext(ctx, "select id, name from widgets", ResultOptions{
		Source: &TableRef{Namespace: "main", Name: "widgets"},
	})
	if err != nil {
		t.Fatalf("QueryResultContext() error = %v", err)
	}

	if result.Source == nil || *result.Source != (TableRef{Namespace: "main", Name: "widgets"}) {
		t.Fatalf("result.Source = %#v, want widgets source", result.Source)
	}
	if got, want := len(result.Columns), 2; got != want {
		t.Fatalf("len(result.Columns) = %d, want %d", got, want)
	}
	if result.Columns[0].Schema != nil {
		t.Fatalf("result.Columns[0].Schema = %#v, want nil without metadata support", result.Columns[0].Schema)
	}
	if result.Columns[0].PrimaryKey != nil {
		t.Fatalf("result.Columns[0].PrimaryKey = %#v, want nil without metadata support", result.Columns[0].PrimaryKey)
	}
	if got, want := len(result.Rows), 1; got != want {
		t.Fatalf("len(result.Rows) = %d, want %d", got, want)
	}
	if got, want := result.Rows[0].Position, 1; got != want {
		t.Fatalf("result.Rows[0].Position = %d, want %d", got, want)
	}
	if got, want := result.Rows[0].Values[1].Kind, ValueKindBytes; got != want {
		t.Fatalf("result.Rows[0].Values[1].Kind = %q, want %q", got, want)
	}
	gotName, ok := result.Rows[0].Values[1].Value.([]byte)
	if !ok {
		t.Fatalf("result.Rows[0].Values[1].Value type = %T, want []byte", result.Rows[0].Values[1].Value)
	}
	if got, want := gotName, []byte("gizmo"); !reflect.DeepEqual(got, want) {
		t.Fatalf("result.Rows[0].Values[1].Value = %#v, want %#v", got, want)
	}

	if got, want := runner.queryCalls, []stubCall{{query: "select id, name from widgets", args: nil}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.queryCalls = %#v, want %#v", got, want)
	}
}

func TestExecuteStatementContextUsesQueryPathForSelect(t *testing.T) {
	ctx := context.Background()
	runner := &stubRunner{
		queryRows: &scriptedRows{
			columns: []string{"id"},
			values:  [][]any{{int64(7)}},
		},
	}

	adapter, err := newAdapter(runner, SQLiteDialect(), nil, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	result, err := adapter.ExecuteStatementContext(ctx, "select id from widgets", ResultOptions{})
	if err != nil {
		t.Fatalf("ExecuteStatementContext() error = %v", err)
	}

	if got, want := result.Kind, StatementResultKindQuery; got != want {
		t.Fatalf("result.Kind = %q, want %q", got, want)
	}
	if result.ResultSet == nil {
		t.Fatal("result.ResultSet = nil, want query result")
	}
	if got, want := len(result.ResultSet.Rows), 1; got != want {
		t.Fatalf("len(result.ResultSet.Rows) = %d, want %d", got, want)
	}
	if got, want := runner.queryCalls, []stubCall{{query: "select id from widgets", args: nil}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.queryCalls = %#v, want %#v", got, want)
	}
	if len(runner.execCalls) != 0 {
		t.Fatalf("runner.execCalls = %#v, want none", runner.execCalls)
	}
}

func TestExecuteStatementContextUsesExecPathForUpdate(t *testing.T) {
	ctx := context.Background()
	runner := &stubRunner{execResult: stubResult(4)}

	adapter, err := newAdapter(runner, SQLiteDialect(), nil, nil, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	result, err := adapter.ExecuteStatementContext(ctx, "update widgets set name = ?", ResultOptions{})
	if err != nil {
		t.Fatalf("ExecuteStatementContext() error = %v", err)
	}

	if got, want := result.Kind, StatementResultKindExec; got != want {
		t.Fatalf("result.Kind = %q, want %q", got, want)
	}
	if result.ResultSet != nil {
		t.Fatalf("result.ResultSet = %#v, want nil", result.ResultSet)
	}
	if result.RowsAffected == nil || *result.RowsAffected != 4 {
		t.Fatalf("result.RowsAffected = %#v, want 4", result.RowsAffected)
	}
	if got, want := runner.execCalls, []stubCall{{query: "update widgets set name = ?", args: nil}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runner.execCalls = %#v, want %#v", got, want)
	}
	if len(runner.queryCalls) != 0 {
		t.Fatalf("runner.queryCalls = %#v, want none", runner.queryCalls)
	}
}

func TestAdapterHealthCheck(t *testing.T) {
	called := false
	adapter, err := newAdapter(&stubRunner{}, SQLiteDialect(), nil, func(context.Context) error {
		called = true
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	if err := adapter.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	if !called {
		t.Fatal("HealthCheck() did not call health check function")
	}
}

func TestAdapterHealthCheckWrapsErrors(t *testing.T) {
	adapter, err := newAdapter(&stubRunner{}, PostgresDialect(), nil, func(context.Context) error {
		return errors.New("ping failed")
	}, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	err = adapter.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() error = nil, want error")
	}

	if got, want := err.Error(), "health check postgres connection: ping failed"; got != want {
		t.Fatalf("HealthCheck() error = %q, want %q", got, want)
	}
}

func TestAdapterHealthCheckPreservesDriverErrors(t *testing.T) {
	adapter, err := newAdapter(&stubRunner{}, PostgresDialect(), nil, func(context.Context) error {
		return &pgconn.PgError{Code: "28P01", Message: "password authentication failed for user \"app\""}
	}, nil)
	if err != nil {
		t.Fatalf("newAdapter() error = %v", err)
	}

	err = adapter.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() error = nil, want error")
	}

	if got, want := err.Error(), "password authentication failed for user \"app\""; !strings.Contains(got, want) {
		t.Fatalf("HealthCheck() error = %q, want to contain %q", got, want)
	}
}

func TestWrapUsesDatabaseSQL(t *testing.T) {
	driverName := registerStubDriver(t)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	adapter, err := Wrap(db, PostgresDialect(), nil)
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	defer adapter.Close()

	result, err := adapter.ExecContext(context.Background(), "update widgets set name = $1 where id = $2", "gizmo", 7)
	if err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() error = %v", err)
	}

	if got, want := affected, int64(1); got != want {
		t.Fatalf("RowsAffected() = %d, want %d", got, want)
	}

	rows, err := adapter.QueryContext(context.Background(), "select id, name from widgets where id = $1", 7)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}

	if got, want := columns, []string{"id", "name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}

	if !rows.Next() {
		t.Fatal("rows.Next() = false, want true")
	}

	var id int64
	var name string
	if err := rows.Scan(&id, &name); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if got, want := id, int64(7); got != want {
		t.Fatalf("id = %d, want %d", got, want)
	}

	if got, want := name, "gizmo"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}

	if rows.Next() {
		t.Fatal("rows.Next() = true, want false")
	}

	var one int64
	if err := adapter.QueryRowContext(context.Background(), "select 1").Scan(&one); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}

	if got, want := one, int64(1); got != want {
		t.Fatalf("one = %d, want %d", got, want)
	}
}

func TestOpenSQLiteAdapterExecutesQueriesAndMetadata(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "sqlcery.db")

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

	if got, want := adapter.Dialect().Name(), "sqlite"; got != want {
		t.Fatalf("adapter.Dialect().Name() = %q, want %q", got, want)
	}

	if _, err := adapter.ExecContext(ctx, "create table widgets (id integer primary key, name text not null default 'unknown', note text)"); err != nil {
		t.Fatalf("ExecContext(create table) error = %v", err)
	}

	if _, err := adapter.ExecContext(ctx, "insert into widgets (name, note) values (?, ?)", "gizmo", "ready"); err != nil {
		t.Fatalf("ExecContext(insert) error = %v", err)
	}

	var count int
	if err := adapter.QueryRowContext(ctx, "select count(*) from widgets where name = ?", "gizmo").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}

	if got, want := count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}

	tables, err := adapter.Tables(ctx, TableFilter{Namespace: "main"})
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}

	if !containsTable(tables, Table{Namespace: "main", Name: "widgets", Type: "table"}) {
		t.Fatalf("Tables() = %#v, want widgets table", tables)
	}

	columns, err := adapter.Columns(ctx, TableRef{Namespace: "main", Name: "widgets"})
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}

	if got, want := columns, []Column{
		{Name: "id", Position: 1, Type: "INTEGER", Nullable: true},
		{Name: "name", Position: 2, Type: "TEXT", Nullable: false, DefaultValue: stringPointer("'unknown'")},
		{Name: "note", Position: 3, Type: "TEXT", Nullable: true},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Columns() = %#v, want %#v", got, want)
	}

	primaryKeys, err := adapter.PrimaryKeys(ctx, TableRef{Namespace: "main", Name: "widgets"})
	if err != nil {
		t.Fatalf("PrimaryKeys() error = %v", err)
	}

	if got, want := primaryKeys, []PrimaryKey{{Column: "id", Position: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrimaryKeys() = %#v, want %#v", got, want)
	}

	types, err := adapter.Types(ctx)
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}

	if !containsType(types, TypeInfo{Name: "integer"}) || !containsType(types, TypeInfo{Name: "text"}) {
		t.Fatalf("Types() = %#v, want integer and text entries", types)
	}

	if err := adapter.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestPostgresConnectionString(t *testing.T) {
	got := postgresConnectionString(config.Connection{
		Host:     "db.example.com",
		Port:     5432,
		Database: "warehouse",
		Username: "app-user",
		Password: "s3cr et:@",
	})

	if want := "postgres://app-user:s3cr%20et%3A%40@db.example.com:5432/warehouse"; got != want {
		t.Fatalf("postgresConnectionString() = %q, want %q", got, want)
	}
}

func TestPostgresConnConfig(t *testing.T) {
	connConfig, err := postgresConnConfig(config.Connection{
		Host:     "db.example.com",
		Port:     5433,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("postgresConnConfig() error = %v", err)
	}

	if got, want := connConfig.Host, "db.example.com"; got != want {
		t.Fatalf("connConfig.Host = %q, want %q", got, want)
	}

	if got, want := connConfig.Port, uint16(5433); got != want {
		t.Fatalf("connConfig.Port = %d, want %d", got, want)
	}

	if got, want := connConfig.Database, "warehouse"; got != want {
		t.Fatalf("connConfig.Database = %q, want %q", got, want)
	}

	if got, want := connConfig.User, "app"; got != want {
		t.Fatalf("connConfig.User = %q, want %q", got, want)
	}

	if got, want := connConfig.Password, "secret"; got != want {
		t.Fatalf("connConfig.Password = %q, want %q", got, want)
	}

	if got, want := connConfig.ConnectTimeout, defaultConnectTimeout; got != want {
		t.Fatalf("connConfig.ConnectTimeout = %s, want %s", got, want)
	}
}

func TestPostgresConnConfigWithLifecycle(t *testing.T) {
	connConfig, err := postgresConnConfigWithLifecycle(config.Connection{
		Host:     "db.example.com",
		Port:     5433,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	}, config.ConnectionLifecycleOptions{
		ConnectTimeout: config.Duration(9 * time.Second),
	})
	if err != nil {
		t.Fatalf("postgresConnConfigWithLifecycle() error = %v", err)
	}

	if got, want := connConfig.ConnectTimeout, 9*time.Second; got != want {
		t.Fatalf("connConfig.ConnectTimeout = %s, want %s", got, want)
	}
}

func TestOpenPostgresAdapterUsesPGXStdlib(t *testing.T) {
	driverName := registerStubPingDriver(t, false)
	originalOpenPostgresDB := openPostgresDB
	originalOpenSSHTunnel := openSSHTunnel
	t.Cleanup(func() {
		openPostgresDB = originalOpenPostgresDB
		openSSHTunnel = originalOpenSSHTunnel
	})

	openPostgresDB = func(connConfig pgx.ConnConfig) *sql.DB {
		if got, want := connConfig.Host, "db.example.com"; got != want {
			t.Fatalf("connConfig.Host = %q, want %q", got, want)
		}

		if got, want := connConfig.Port, uint16(5432); got != want {
			t.Fatalf("connConfig.Port = %d, want %d", got, want)
		}

		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db
	}

	openSSHTunnel = func(context.Context, string) (*sshTunnel, error) {
		return nil, errors.New("ssh tunnel should not be opened")
	}

	adapter, err := Open(context.Background(), config.Connection{
		Type:     "postgres",
		Host:     "db.example.com",
		Port:     5432,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
		Lifecycle: config.ConnectionLifecycleOptions{
			ConnectTimeout:     config.Duration(9 * time.Second),
			HealthCheckTimeout: config.Duration(time.Second),
			MaxOpenConns:       6,
			MaxIdleConns:       2,
			ConnMaxLifetime:    config.Duration(10 * time.Minute),
			ConnMaxIdleTime:    config.Duration(2 * time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer adapter.Close()

	if got, want := adapter.Dialect().Name(), "postgres"; got != want {
		t.Fatalf("adapter.Dialect().Name() = %q, want %q", got, want)
	}

	stats := dbStats(adapter)
	if got, want := stats.MaxOpenConnections, 6; got != want {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, want)
	}

	if err := adapter.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestOpenPostgresAdapterUsesSSHTunnelWhenConfigured(t *testing.T) {
	driverName := registerStubPingDriver(t, false)
	originalOpenPostgresDB := openPostgresDB
	originalOpenSSHTunnel := openSSHTunnel
	t.Cleanup(func() {
		openPostgresDB = originalOpenPostgresDB
		openSSHTunnel = originalOpenSSHTunnel
	})

	tunnelDialCalled := false
	tunnelClosed := false
	openSSHTunnel = func(_ context.Context, sshHost string) (*sshTunnel, error) {
		if got, want := sshHost, "bastion"; got != want {
			t.Fatalf("sshHost = %q, want %q", got, want)
		}

		return &sshTunnel{
			dialContext: func(context.Context, string, string) (net.Conn, error) {
				tunnelDialCalled = true
				return nil, nil
			},
			close: func() error {
				tunnelClosed = true
				return nil
			},
		}, nil
	}

	openPostgresDB = func(connConfig pgx.ConnConfig) *sql.DB {
		if connConfig.DialFunc == nil {
			t.Fatal("connConfig.DialFunc = nil, want tunnel dialer")
		}

		if _, err := connConfig.DialFunc(context.Background(), "tcp", "db.internal:5432"); err != nil {
			t.Fatalf("connConfig.DialFunc() error = %v", err)
		}

		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db
	}

	adapter, err := Open(context.Background(), config.Connection{
		Type:     "postgres",
		SSHHost:  "bastion",
		Host:     "db.internal",
		Port:     5432,
		Database: "warehouse",
		Username: "app",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if !tunnelDialCalled {
		t.Fatal("tunnel dialer was not used")
	}

	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !tunnelClosed {
		t.Fatal("tunnel was not closed")
	}
}

func TestOpenPostgresReturnsPingError(t *testing.T) {
	driverName := registerStubPingDriver(t, true)
	originalOpenPostgresDB := openPostgresDB
	t.Cleanup(func() {
		openPostgresDB = originalOpenPostgresDB
	})

	openPostgresDB = func(pgx.ConnConfig) *sql.DB {
		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db
	}

	_, err := Open(context.Background(), config.Connection{
		Type:     "postgres",
		Host:     "db.example.com",
		Port:     5432,
		Database: "warehouse",
		Username: "app",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if got, want := err.Error(), "ping postgres database \"warehouse\" on db.example.com:5432: ping failed"; got != want {
		t.Fatalf("Open() error = %q, want %q", got, want)
	}
}

func TestOpenPostgresReturnsAuthenticationError(t *testing.T) {
	driverName := registerStubPingDriverWithError(t, &pgconn.PgError{
		Severity: "FATAL",
		Code:     "28P01",
		Message:  "password authentication failed for user \"app\"",
	})
	originalOpenPostgresDB := openPostgresDB
	t.Cleanup(func() {
		openPostgresDB = originalOpenPostgresDB
	})

	openPostgresDB = func(pgx.ConnConfig) *sql.DB {
		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db
	}

	_, err := Open(context.Background(), config.Connection{
		Type:     "postgres",
		Host:     "db.example.com",
		Port:     5432,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Open() error = %v, want errors.Is(..., ErrAuthentication)", err)
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("Open() error = %v, want AuthenticationError", err)
	}

	if got, want := authErr.Dialect, "postgres"; got != want {
		t.Fatalf("authErr.Dialect = %q, want %q", got, want)
	}

	if got, want := err.Error(), "ping postgres database \"warehouse\" on db.example.com:5432"; !strings.Contains(got, want) {
		t.Fatalf("Open() error = %q, want to contain %q", got, want)
	}

	if got, want := err.Error(), "password authentication failed"; !strings.Contains(got, want) {
		t.Fatalf("Open() error = %q, want to contain %q", got, want)
	}
}

func TestOpenRejectsUnsupportedDatabaseTypes(t *testing.T) {
	_, err := Open(context.Background(), config.Connection{
		Type: "oracle",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if got, want := err.Error(), "type must be one of sqlite, postgres, mysql"; got != want {
		t.Fatalf("Open() error = %q, want %q", got, want)
	}
}

func stringPointer(value string) *string {
	return &value
}

func containsTable(tables []Table, want Table) bool {
	for _, table := range tables {
		if table == want {
			return true
		}
	}

	return false
}

func containsType(types []TypeInfo, want TypeInfo) bool {
	for _, typeInfo := range types {
		if typeInfo == want {
			return true
		}
	}

	return false
}

func dbStats(adapter *SQLAdapter) sql.DBStats {
	runner, ok := adapter.runner.(sqlRunner)
	if !ok {
		return sql.DBStats{}
	}

	return runner.db.Stats()
}

type stubRunner struct {
	execCalls     []stubCall
	queryCalls    []stubCall
	queryRowCalls []stubCall
	execResult    sql.Result
	queryRows     Rows
	queryRow      Row
}

func (s *stubRunner) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.execCalls = append(s.execCalls, stubCall{query: query, args: append([]any(nil), args...)})
	if s.execResult == nil {
		return stubResult(0), nil
	}
	return s.execResult, nil
}

func (s *stubRunner) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	s.queryCalls = append(s.queryCalls, stubCall{query: query, args: append([]any(nil), args...)})
	if s.queryRows == nil {
		return stubRows{}, nil
	}
	return s.queryRows, nil
}

func (s *stubRunner) QueryRowContext(_ context.Context, query string, args ...any) Row {
	s.queryRowCalls = append(s.queryRowCalls, stubCall{query: query, args: append([]any(nil), args...)})
	if s.queryRow == nil {
		return stubRow{}
	}
	return s.queryRow
}

type stubMetadataProvider struct {
	tableFilters []TableFilter
	tableRefs    []TableRef
	typeCalls    int
	tables       []Table
	columns      []Column
	primaryKeys  []PrimaryKey
	types        []TypeInfo
}

func (s *stubMetadataProvider) Tables(_ context.Context, filter TableFilter) ([]Table, error) {
	s.tableFilters = append(s.tableFilters, filter)
	return s.tables, nil
}

func (s *stubMetadataProvider) Columns(_ context.Context, table TableRef) ([]Column, error) {
	s.tableRefs = append(s.tableRefs, table)
	return s.columns, nil
}

func (s *stubMetadataProvider) PrimaryKeys(_ context.Context, table TableRef) ([]PrimaryKey, error) {
	s.tableRefs = append(s.tableRefs, table)
	return s.primaryKeys, nil
}

func (s *stubMetadataProvider) Types(context.Context) ([]TypeInfo, error) {
	s.typeCalls++
	return s.types, nil
}

type stubRows struct{}

func (stubRows) Close() error                            { return nil }
func (stubRows) Columns() ([]string, error)              { return nil, nil }
func (stubRows) ColumnTypes() ([]*sql.ColumnType, error) { return nil, nil }
func (stubRows) Err() error                              { return nil }
func (stubRows) Next() bool                              { return false }
func (stubRows) Scan(...any) error                       { return nil }

type stubRow struct {
	value string
}

func (r stubRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("Scan() got %d destinations, want 1", len(dest))
	}

	ptr, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("Scan() destination type = %T, want *string", dest[0])
	}

	*ptr = r.value
	return nil
}

type stubCall struct {
	query string
	args  []any
}

type stubResult int64

func (r stubResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r stubResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

var registerStubDriverOnce sync.Once

func registerStubDriver(t *testing.T) string {
	t.Helper()

	const name = "sqlcery-internal-db-stub"
	registerStubDriverOnce.Do(func() {
		sql.Register(name, stubDriver{})
	})

	return name
}

type stubDriver struct{}

func (stubDriver) Open(string) (driver.Conn, error) {
	return stubConn{}, nil
}

type stubConn struct{}

func (stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not implemented")
}

func (stubConn) Close() error {
	return nil
}

func (stubConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin not implemented")
}

func (stubConn) Ping(context.Context) error {
	return nil
}

func (stubConn) ExecContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("ExecContext() got %d args, want 2", len(args))
	}

	return driver.RowsAffected(1), nil
}

func (stubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case "select id, name from widgets where id = $1":
		if len(args) != 1 {
			return nil, fmt.Errorf("QueryContext() got %d args, want 1", len(args))
		}
		return &stubDriverRows{
			columns: []string{"id", "name"},
			values:  [][]driver.Value{{int64(7), "gizmo"}},
		}, nil
	case "select 1":
		return &stubDriverRows{
			columns: []string{"?column?"},
			values:  [][]driver.Value{{int64(1)}},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
}

type stubDriverRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *stubDriverRows) Columns() []string {
	return r.columns
}

func (*stubDriverRows) Close() error {
	return nil
}

func (r *stubDriverRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}

	copy(dest, r.values[r.index])
	r.index++
	return nil
}

var registerStubPingDriverOnce sync.Once
var stubPingDriverCounter uint64

func registerStubPingDriver(t *testing.T, failPing bool) string {
	t.Helper()

	name := "sqlcery-internal-db-stub-ping-success"
	if failPing {
		name = "sqlcery-internal-db-stub-ping-failure"
	}

	registerStubPingDriverOnce.Do(func() {
		sql.Register("sqlcery-internal-db-stub-ping-success", stubPingDriver{failPing: false})
		sql.Register("sqlcery-internal-db-stub-ping-failure", stubPingDriver{failPing: true})
	})

	return name
}

func registerStubPingDriverWithError(t *testing.T, pingErr error) string {
	t.Helper()

	name := fmt.Sprintf("sqlcery-internal-db-stub-ping-custom-%d", atomic.AddUint64(&stubPingDriverCounter, 1))
	sql.Register(name, stubPingDriver{pingErr: pingErr})
	return name
}

type stubPingDriver struct {
	failPing bool
	pingErr  error
}

func (d stubPingDriver) Open(string) (driver.Conn, error) {
	return stubPingConn{failPing: d.failPing, pingErr: d.pingErr}, nil
}

type stubPingConn struct {
	failPing bool
	pingErr  error
}

func (stubPingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not implemented")
}

func (stubPingConn) Close() error {
	return nil
}

func (stubPingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin not implemented")
}

func (c stubPingConn) Ping(context.Context) error {
	if c.pingErr != nil {
		return c.pingErr
	}

	if c.failPing {
		return errors.New("ping failed")
	}

	return nil
}
