package app

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func FormatTerminalError(err error) string {
	if err == nil {
		return ""
	}

	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return "An unexpected error occurred."
	}

	switch {
	case errors.Is(err, context.Canceled):
		return withErrorDetails("Operation cancelled.", detail)
	case errors.Is(err, config.ErrUnknownConnection):
		return withErrorDetails("Connection not found. Check the connection name in your config or CLI argument.", detail)
	case errors.Is(err, config.ErrInvalidConfig), isConnectionConfigError(err):
		return withErrorDetails("Configuration error. Check your SQLcery config or connection string.", detail)
	case db.IsAuthenticationError(err), isAuthenticationDriverError(err), containsErrorText(detail,
		"password authentication failed",
		"authentication failed",
		"access denied for user",
	):
		return withErrorDetails("Authentication failed. Check your username, password, and database grants.", detail)
	case isNetworkError(err):
		return withErrorDetails("Network error while reaching the database. Check the host, port, SSH tunnel, or VPN.", detail)
	case isQueryError(err):
		return withErrorDetails("SQL query failed. Check the statement and any referenced tables or columns.", detail)
	case isDriverError(err):
		return withErrorDetails("Database driver error. Check the connection settings and retry.", detail)
	default:
		return detail
	}
}

func formatOperationFailure(prefix string, err error) string {
	prefix = strings.TrimSpace(prefix)
	formatted := strings.TrimSpace(FormatTerminalError(err))
	if prefix == "" {
		return formatted
	}
	if formatted == "" {
		return prefix
	}
	if strings.HasSuffix(prefix, ".") || strings.HasSuffix(prefix, "!") || strings.HasSuffix(prefix, "?") {
		return prefix + " " + formatted
	}
	return prefix + ": " + formatted
}

func withErrorDetails(summary, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" || detail == summary {
		return summary
	}
	return summary + " Details: " + detail
}

func isAuthenticationDriverError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "28")
	}

	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.Number == 1045 {
			return true
		}
		return strings.TrimRight(string(mysqlErr.SQLState[:]), "\x00") == "28000"
	}

	return false
}

func isNetworkError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return containsErrorText(err.Error(),
		"dial tcp",
		"lookup ",
		"no such host",
		"connection refused",
		"connection reset by peer",
		"network is unreachable",
		"i/o timeout",
		"broken pipe",
	)
}

func isConnectionConfigError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
		return true
	}

	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1049 {
		return true
	}

	return containsErrorText(err.Error(),
		"invalid connection string",
		"unsupported connection string scheme",
		"parse postgres connection config",
		"unable to open database file",
	)
}

func isQueryError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "42")
	}

	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1054, 1064, 1146:
			return true
		}
	}

	return containsErrorText(err.Error(),
		"syntax error",
		"sql logic error",
		"no such table",
		"no such column",
		"unknown column",
		"relation ",
	)
}

func isDriverError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return true
	}

	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return true
	}

	return containsErrorText(err.Error(), "driver:", "bad connection")
}

func containsErrorText(message string, fragments ...string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, fragment := range fragments {
		if strings.Contains(message, strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}
