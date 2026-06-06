package app

import (
	"context"
	"errors"
	"strings"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
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
	case errors.Is(err, config.ErrInvalidConfig):
		return withErrorDetails("Configuration error. Check your SQLcery config or connection string.", detail)
	default:
		switch db.ClassifyError(err) {
		case db.ErrorKindConnectionConfig:
			return withErrorDetails("Configuration error. Check your SQLcery config or connection string.", detail)
		case db.ErrorKindAuthentication:
			return withErrorDetails("Authentication failed. Check your username, password, and database grants.", detail)
		case db.ErrorKindNetwork:
			return withErrorDetails("Network error while reaching the database. Check the host, port, SSH tunnel, or VPN.", detail)
		case db.ErrorKindQuery:
			return withErrorDetails("SQL query failed. Check the statement and any referenced tables or columns.", detail)
		case db.ErrorKindDriver:
			return withErrorDetails("Database driver error. Check the connection settings and retry.", detail)
		default:
			return detail
		}
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
