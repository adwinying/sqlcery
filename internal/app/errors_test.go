package app

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFormatTerminalErrorClassifiesCommonFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "cancelled",
			err:  context.Canceled,
			want: "Operation cancelled.",
		},
		{
			name: "unknown connection",
			err:  &config.UnknownConnectionError{Name: "missing"},
			want: "Connection not found. Check the connection name in your config or CLI argument.",
		},
		{
			name: "invalid config",
			err:  &config.InvalidConfigError{Op: "validate", Path: "sqlcery.toml", Err: errors.New("connection must not be blank")},
			want: "Configuration error. Check your SQLcery config or connection string.",
		},
		{
			name: "authentication",
			err:  &db.AuthenticationError{Dialect: "postgres", Message: "ping postgres database \"warehouse\" on db.example.com:5432", Err: &pgconn.PgError{Code: "28P01", Message: "password authentication failed for user \"app\""}},
			want: "Authentication failed. Check your username, password, and database grants.",
		},
		{
			name: "network",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
			want: "Network error while reaching the database. Check the host, port, SSH tunnel, or VPN.",
		},
		{
			name: "query",
			err:  errors.New("no such table: widgets"),
			want: "SQL query failed. Check the statement and any referenced tables or columns.",
		},
		{
			name: "driver",
			err:  &mysqldriver.MySQLError{Number: 9999, Message: "driver exploded"},
			want: "Database driver error. Check the connection settings and retry.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTerminalError(tt.err)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("FormatTerminalError() = %q, want to contain %q", got, tt.want)
			}
			if !strings.Contains(got, tt.err.Error()) {
				t.Fatalf("FormatTerminalError() = %q, want to preserve detail %q", got, tt.err.Error())
			}
		})
	}
}

func TestFormatOperationFailure(t *testing.T) {
	err := context.DeadlineExceeded
	got := formatOperationFailure("Execution failed.", err)
	if !strings.Contains(got, "Execution failed. Network error while reaching the database.") {
		t.Fatalf("formatOperationFailure() = %q, want formatted prefix", got)
	}
	if !strings.Contains(got, err.Error()) {
		t.Fatalf("formatOperationFailure() = %q, want to preserve detail %q", got, err.Error())
	}
}
