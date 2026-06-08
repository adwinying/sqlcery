package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type sqliteMetadata struct {
	runner Runner
}

func (m sqliteMetadata) Tables(ctx context.Context, filter TableFilter) ([]Table, error) {
	schemas, err := m.schemas(ctx, filter.Namespace)
	if err != nil {
		return nil, err
	}

	tables := make([]Table, 0)
	for _, schema := range schemas {
		query := fmt.Sprintf(
			"SELECT name, type FROM %s WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%%' ORDER BY name",
			SQLiteDialect().QuoteIdentifier(schema, "sqlite_schema"),
		)

		rows, err := m.runner.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("list sqlite tables for schema %q: %w", schema, err)
		}

		for rows.Next() {
			var name string
			var kind string
			if err := rows.Scan(&name, &kind); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan sqlite table metadata: %w", err)
			}

			tables = append(tables, Table{Namespace: schema, Name: name, Type: kind})
		}

		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate sqlite tables for schema %q: %w", schema, err)
		}

		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close sqlite table metadata rows: %w", err)
		}
	}

	return tables, nil
}

func (m sqliteMetadata) Columns(ctx context.Context, table TableRef) ([]Column, error) {
	columnInfo, _, err := m.tableInfo(ctx, table)
	if err != nil {
		return nil, err
	}

	columns := make([]Column, 0, len(columnInfo))
	for _, info := range columnInfo {
		columns = append(columns, Column{
			Name:         info.Name,
			Position:     info.Position,
			Type:         info.Type,
			Nullable:     info.Nullable,
			DefaultValue: info.DefaultValue,
		})
	}

	return columns, nil
}

func (m sqliteMetadata) PrimaryKeys(ctx context.Context, table TableRef) ([]PrimaryKey, error) {
	columnInfo, _, err := m.tableInfo(ctx, table)
	if err != nil {
		return nil, err
	}

	primaryKeys := make([]PrimaryKey, 0)
	for _, info := range columnInfo {
		if info.PrimaryKeyPosition == 0 {
			continue
		}

		primaryKeys = append(primaryKeys, PrimaryKey{
			Column:   info.Name,
			Position: info.PrimaryKeyPosition,
		})
	}

	sort.Slice(primaryKeys, func(i, j int) bool {
		return primaryKeys[i].Position < primaryKeys[j].Position
	})

	return primaryKeys, nil
}

func (sqliteMetadata) Types(context.Context) ([]TypeInfo, error) {
	return cloneTypes(sqliteTypeInfo), nil
}

func (m sqliteMetadata) tableInfo(ctx context.Context, table TableRef) ([]sqliteColumnInfo, string, error) {
	schema := strings.TrimSpace(table.Namespace)
	if schema == "" {
		schema = "main"
	}

	query := fmt.Sprintf("PRAGMA %s.table_info(%s)", SQLiteDialect().QuoteIdentifier(schema), sqliteStringLiteral(table.Name))
	rows, err := m.runner.QueryContext(ctx, query)
	if err != nil {
		return nil, schema, fmt.Errorf("list sqlite columns for %s: %w", SQLiteDialect().QuoteIdentifier(schema, table.Name), err)
	}
	defer rows.Close()

	columns := make([]sqliteColumnInfo, 0)
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int

		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, schema, fmt.Errorf("scan sqlite column metadata: %w", err)
		}

		columns = append(columns, sqliteColumnInfo{
			Name:               name,
			Position:           cid + 1,
			Type:               columnType,
			Nullable:           notNull == 0,
			DefaultValue:       nullStringPointer(defaultValue),
			PrimaryKeyPosition: primaryKey,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, schema, fmt.Errorf("iterate sqlite columns for %s: %w", SQLiteDialect().QuoteIdentifier(schema, table.Name), err)
	}

	return columns, schema, nil
}

func (m sqliteMetadata) schemas(ctx context.Context, explicit string) ([]string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return []string{explicit}, nil
	}

	rows, err := m.runner.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, fmt.Errorf("list sqlite databases: %w", err)
	}
	defer rows.Close()

	schemas := make([]string, 0)
	for rows.Next() {
		var seq int
		var name string
		var file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return nil, fmt.Errorf("scan sqlite database metadata: %w", err)
		}

		schemas = append(schemas, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite databases: %w", err)
	}

	return schemas, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	copy := value.String
	return &copy
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type sqliteColumnInfo struct {
	Name               string
	Position           int
	Type               string
	Nullable           bool
	DefaultValue       *string
	PrimaryKeyPosition int
}

var sqliteTypeInfo = []TypeInfo{
	{Name: "blob"},
	{Name: "boolean"},
	{Name: "date"},
	{Name: "datetime"},
	{Name: "integer"},
	{Name: "json"},
	{Name: "numeric"},
	{Name: "real"},
	{Name: "text"},
}
