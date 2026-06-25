package db

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrAuthentication = errors.New("authentication failed")

type AuthenticationError struct {
	Dialect string
	Message string
	Err     error
}

func (e *AuthenticationError) Error() string {
	if e == nil {
		return ErrAuthentication.Error()
	}

	if e.Message != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	return ErrAuthentication.Error()
}

func (e *AuthenticationError) Unwrap() error {
	if e == nil || e.Err == nil {
		return ErrAuthentication
	}

	return errors.Join(ErrAuthentication, e.Err)
}

func wrapConnectionError(dialect string, message string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrAuthentication) {
		return err
	}

	if isAuthenticationFailure(dialect, err) {
		return &AuthenticationError{Dialect: dialect, Message: message, Err: err}
	}

	return fmt.Errorf("%s: %w", message, err)
}

func isAuthenticationFailure(dialect string, err error) bool {
	switch dialect {
	case "postgres":
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return strings.HasPrefix(pgErr.Code, "28")
		}
	case "mysql":
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) {
			if mysqlErr.Number == 1045 {
				return true
			}

			if strings.TrimRight(string(mysqlErr.SQLState[:]), "\x00") == "28000" {
				return true
			}
		}
	}

	return containsAuthenticationText(err.Error())
}

func containsAuthenticationText(message string) bool {
	message = strings.ToLower(message)

	return strings.Contains(message, "password authentication failed") ||
		strings.Contains(message, "authentication failed") ||
		strings.Contains(message, "access denied for user")
}

// ErrorKind classifies a database-layer error by its user-facing category.
type ErrorKind int

const (
	// ErrorKindUnknown is returned for errors that do not match any known category.
	ErrorKindUnknown ErrorKind = iota
	// ErrorKindAuthentication covers credential failures (wrong password, access denied).
	ErrorKindAuthentication
	// ErrorKindConnectionConfig covers misconfigured connection targets (unknown database, bad DSN).
	ErrorKindConnectionConfig
	// ErrorKindNetwork covers connectivity failures (timeouts, refused connections).
	ErrorKindNetwork
	// ErrorKindQuery covers SQL-level failures (syntax errors, unknown tables/columns).
	ErrorKindQuery
	// ErrorKindDriver covers any other driver-reported error not in the above categories.
	ErrorKindDriver
)

// ClassifyError maps an error returned from the database layer to an ErrorKind.
// It is the single authoritative place for driver-specific error classification;
// callers do not need to import database driver packages directly.
func ClassifyError(err error) ErrorKind {
	if err == nil {
		return ErrorKindUnknown
	}
	if errors.Is(err, ErrAuthentication) {
		return ErrorKindAuthentication
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case strings.HasPrefix(pgErr.Code, "28"):
			return ErrorKindAuthentication
		case pgErr.Code == "3D000":
			return ErrorKindConnectionConfig
		case strings.HasPrefix(pgErr.Code, "42"):
			return ErrorKindQuery
		default:
			return ErrorKindDriver
		}
	}

	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		sqlState := strings.TrimRight(string(mysqlErr.SQLState[:]), "\x00")
		switch {
		case mysqlErr.Number == 1045 || sqlState == "28000":
			return ErrorKindAuthentication
		case mysqlErr.Number == 1049:
			return ErrorKindConnectionConfig
		case mysqlErr.Number == 1054 || mysqlErr.Number == 1064 || mysqlErr.Number == 1146:
			return ErrorKindQuery
		default:
			return ErrorKindDriver
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorKindNetwork
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrorKindNetwork
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case containsAuthenticationText(msg):
		return ErrorKindAuthentication
	case containsAnyText(msg,
		"invalid connection string",
		"unsupported connection string scheme",
		"parse postgres connection config",
		"unable to open database file",
	):
		return ErrorKindConnectionConfig
	case containsAnyText(msg,
		"dial tcp",
		"lookup ",
		"no such host",
		"connection refused",
		"connection reset by peer",
		"network is unreachable",
		"i/o timeout",
		"broken pipe",
	):
		return ErrorKindNetwork
	case containsAnyText(msg,
		"syntax error",
		"sql logic error",
		"no such table",
		"no such column",
		"unknown column",
		"relation ",
	):
		return ErrorKindQuery
	case containsAnyText(msg, "driver:", "bad connection"):
		return ErrorKindDriver
	}

	return ErrorKindUnknown
}

func containsAnyText(message string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(message, strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}
