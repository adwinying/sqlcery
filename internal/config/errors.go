package config

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidConfig     = errors.New("invalid config")
	ErrUnknownConnection = errors.New("unknown connection")
)

type InvalidConfigError struct {
	Op   string
	Path string
	Err  error
}

func (e *InvalidConfigError) Error() string {
	if e == nil {
		return ErrInvalidConfig.Error()
	}

	if e.Op != "" && e.Path != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
	}

	if e.Path != "" {
		return fmt.Sprintf("%s: %v", e.Path, e.Err)
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	return ErrInvalidConfig.Error()
}

func (e *InvalidConfigError) Unwrap() error {
	if e == nil || e.Err == nil {
		return ErrInvalidConfig
	}

	return errors.Join(ErrInvalidConfig, e.Err)
}

type UnknownConnectionError struct {
	Name string
}

func (e *UnknownConnectionError) Error() string {
	if e == nil {
		return ErrUnknownConnection.Error()
	}

	return fmt.Sprintf("unknown connection %q", e.Name)
}

func (e *UnknownConnectionError) Unwrap() error {
	return ErrUnknownConnection
}

func IsInvalidConfig(err error) bool {
	return errors.Is(err, ErrInvalidConfig)
}

func IsUnknownConnection(err error) bool {
	return errors.Is(err, ErrUnknownConnection)
}
