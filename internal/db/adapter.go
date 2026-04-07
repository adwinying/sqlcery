package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrMetadataUnsupported = errors.New("metadata lookup unsupported")

type Rows interface {
	Close() error
	Columns() ([]string, error)
	ColumnTypes() ([]*sql.ColumnType, error)
	Err() error
	Next() bool
	Scan(dest ...any) error
}

type Row interface {
	Scan(dest ...any) error
}

type Runner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) Row
}

type MetadataProvider interface {
	Tables(ctx context.Context, filter TableFilter) ([]Table, error)
	Columns(ctx context.Context, table TableRef) ([]Column, error)
	PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error)
	Types(ctx context.Context) ([]TypeInfo, error)
}

type Adapter interface {
	Runner
	Dialect() Dialect
	QueryResultContext(ctx context.Context, query string, options ResultOptions, args ...any) (*ResultSet, error)
	Tables(ctx context.Context, filter TableFilter) ([]Table, error)
	Columns(ctx context.Context, table TableRef) ([]Column, error)
	PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error)
	Types(ctx context.Context) ([]TypeInfo, error)
	HealthCheck(ctx context.Context) error
	Close() error
}

type TableFilter struct {
	Catalog string
	Schema  string
}

type Table struct {
	Catalog string
	Schema  string
	Name    string
	Type    string
}

type TableRef struct {
	Catalog string
	Schema  string
	Name    string
}

func (t TableRef) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("table name is required")
	}

	return nil
}

type Column struct {
	Name         string
	Position     int
	Type         string
	Nullable     bool
	DefaultValue *string
}

type PrimaryKey struct {
	Name     string
	Column   string
	Position int
}

type TypeInfo struct {
	Schema string
	Name   string
}

type SQLAdapter struct {
	runner   Runner
	metadata MetadataProvider
	dialect  Dialect
	health   func(context.Context) error
	close    func() error
}

func Wrap(db *sql.DB, dialect Dialect, metadata MetadataProvider) (*SQLAdapter, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	return newAdapter(sqlRunner{db: db}, dialect, metadata, db.PingContext, db.Close)
}

func newAdapter(runner Runner, dialect Dialect, metadata MetadataProvider, healthCheckFn func(context.Context) error, closeFn func() error) (*SQLAdapter, error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	if dialect == nil {
		return nil, fmt.Errorf("dialect is required")
	}

	if metadata == nil {
		metadata = unsupportedMetadata{}
	}

	if closeFn == nil {
		closeFn = func() error { return nil }
	}

	if healthCheckFn == nil {
		healthCheckFn = func(context.Context) error { return nil }
	}

	return &SQLAdapter{
		runner:   runner,
		metadata: metadata,
		dialect:  dialect,
		health:   healthCheckFn,
		close:    closeFn,
	}, nil
}

func (a *SQLAdapter) Dialect() Dialect {
	return a.dialect
}

func (a *SQLAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return a.runner.ExecContext(ctx, query, args...)
}

func (a *SQLAdapter) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return a.runner.QueryContext(ctx, query, args...)
}

func (a *SQLAdapter) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return a.runner.QueryRowContext(ctx, query, args...)
}

func (a *SQLAdapter) QueryResultContext(ctx context.Context, query string, options ResultOptions, args ...any) (*ResultSet, error) {
	if options.Source != nil {
		if len(options.Columns) == 0 {
			columns, err := a.Columns(ctx, *options.Source)
			if err != nil {
				return nil, err
			}
			options.Columns = columns
		}

		if len(options.PrimaryKeys) == 0 {
			primaryKeys, err := a.PrimaryKeys(ctx, *options.Source)
			if err != nil {
				return nil, err
			}
			options.PrimaryKeys = primaryKeys
		}
	}

	rows, err := a.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	return NormalizeRows(rows, options)
}

func (a *SQLAdapter) Tables(ctx context.Context, filter TableFilter) ([]Table, error) {
	return a.metadata.Tables(ctx, filter)
}

func (a *SQLAdapter) Columns(ctx context.Context, table TableRef) ([]Column, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}

	return a.metadata.Columns(ctx, table)
}

func (a *SQLAdapter) PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}

	return a.metadata.PrimaryKeys(ctx, table)
}

func (a *SQLAdapter) Types(ctx context.Context) ([]TypeInfo, error) {
	return a.metadata.Types(ctx)
}

func (a *SQLAdapter) HealthCheck(ctx context.Context) error {
	return healthCheckError(a.dialect.Name(), a.health(ctx))
}

func (a *SQLAdapter) Close() error {
	return a.close()
}

type sqlRunner struct {
	db *sql.DB
}

func (r sqlRunner) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return r.db.ExecContext(ctx, query, args...)
}

func (r sqlRunner) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	return sqlRows{rows: rows}, nil
}

func (r sqlRunner) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return sqlRow{row: r.db.QueryRowContext(ctx, query, args...)}
}

type sqlRows struct {
	rows *sql.Rows
}

func (r sqlRows) Close() error {
	return r.rows.Close()
}

func (r sqlRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r sqlRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r sqlRows) Err() error {
	return r.rows.Err()
}

func (r sqlRows) Next() bool {
	return r.rows.Next()
}

func (r sqlRows) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

type sqlRow struct {
	row *sql.Row
}

func (r sqlRow) Scan(dest ...any) error {
	return r.row.Scan(dest...)
}

type unsupportedMetadata struct{}

func (unsupportedMetadata) Tables(context.Context, TableFilter) ([]Table, error) {
	return nil, ErrMetadataUnsupported
}

func (unsupportedMetadata) Columns(context.Context, TableRef) ([]Column, error) {
	return nil, ErrMetadataUnsupported
}

func (unsupportedMetadata) PrimaryKeys(context.Context, TableRef) ([]PrimaryKey, error) {
	return nil, ErrMetadataUnsupported
}

func (unsupportedMetadata) Types(context.Context) ([]TypeInfo, error) {
	return nil, ErrMetadataUnsupported
}
