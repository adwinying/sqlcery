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
	ExecuteStatementContext(ctx context.Context, query string, options ResultOptions, args ...any) (*StatementResult, error)
	QueryResultContext(ctx context.Context, query string, options ResultOptions, args ...any) (*ResultSet, error)
	Tables(ctx context.Context, filter TableFilter) ([]Table, error)
	Columns(ctx context.Context, table TableRef) ([]Column, error)
	PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error)
	Types(ctx context.Context) ([]TypeInfo, error)
	HealthCheck(ctx context.Context) error
	Close() error
}

type StatementResultKind string

const (
	StatementResultKindQuery StatementResultKind = "query"
	StatementResultKindExec  StatementResultKind = "exec"
)

type StatementResult struct {
	Kind         StatementResultKind
	ResultSet    *ResultSet
	RowsAffected *int64
	LastInsertID *int64
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

func (a *SQLAdapter) ExecuteStatementContext(ctx context.Context, query string, options ResultOptions, args ...any) (*StatementResult, error) {
	if statementReturnsRows(query) {
		resultSet, err := a.QueryResultContext(ctx, query, options, args...)
		if err != nil {
			return nil, err
		}

		return &StatementResult{
			Kind:      StatementResultKindQuery,
			ResultSet: resultSet,
		}, nil
	}

	execResult, err := a.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	result := &StatementResult{Kind: StatementResultKindExec}
	if rowsAffected, err := execResult.RowsAffected(); err == nil {
		result.RowsAffected = int64Pointer(rowsAffected)
	}
	if lastInsertID, err := execResult.LastInsertId(); err == nil {
		result.LastInsertID = int64Pointer(lastInsertID)
	}

	return result, nil
}

func (a *SQLAdapter) QueryResultContext(ctx context.Context, query string, options ResultOptions, args ...any) (*ResultSet, error) {
	if options.Source != nil {
		if len(options.Columns) == 0 {
			columns, err := a.Columns(ctx, *options.Source)
			if err != nil {
				if !errors.Is(err, ErrMetadataUnsupported) {
					return nil, err
				}
			} else {
				options.Columns = columns
			}
		}

		if len(options.PrimaryKeys) == 0 {
			primaryKeys, err := a.PrimaryKeys(ctx, *options.Source)
			if err != nil {
				if !errors.Is(err, ErrMetadataUnsupported) {
					return nil, err
				}
			} else {
				options.PrimaryKeys = primaryKeys
			}
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

func statementReturnsRows(query string) bool {
	switch statementLeadingKeyword(query) {
	case "DESC", "DESCRIBE", "EXPLAIN", "PRAGMA", "SELECT", "SHOW", "VALUES", "WITH":
		return true
	default:
		return false
	}
}

func statementLeadingKeyword(query string) string {
	runes := []rune(query)
	for i := 0; i < len(runes); {
		switch {
		case isSpaceRune(runes[i]):
			i++
		case hasRunePrefix(runes, i, '-', '-'):
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case hasRunePrefix(runes, i, '/', '*'):
			i += 2
			for i < len(runes) && !hasRunePrefix(runes, i, '*', '/') {
				i++
			}
			if i < len(runes) {
				i += 2
			}
		case isKeywordRune(runes[i]):
			start := i
			for i < len(runes) && isKeywordRune(runes[i]) {
				i++
			}
			return strings.ToUpper(string(runes[start:i]))
		default:
			return ""
		}
	}

	return ""
}

func hasRunePrefix(runes []rune, index int, first, second rune) bool {
	return index+1 < len(runes) && runes[index] == first && runes[index+1] == second
}

func isSpaceRune(value rune) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func isKeywordRune(value rune) bool {
	return value == '_' || value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
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
