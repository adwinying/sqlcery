package db

import (
	"errors"
	"fmt"
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

func IsAuthenticationError(err error) bool {
	return errors.Is(err, ErrAuthentication)
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
