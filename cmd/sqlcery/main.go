package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/adwinying/sqlcery/internal/app"
	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

func main() {
	if err := run(os.Args[1:], os.Getwd); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, getwd func() (string, error)) error {
	return runWithDependencies(args, getwd, runDependencies{
		open: db.Open,
		start: func(ctx context.Context, session app.Session, adapter *db.SQLAdapter) error {
			history, err := apphistory.NewPersistentSession(session.ConnectionName)
			if err != nil {
				return err
			}
			return app.Run(ctx, session, adapter, app.RunOptions{History: history})
		},
	})
}

type runDependencies struct {
	open  func(context.Context, config.Connection) (*db.SQLAdapter, error)
	start func(context.Context, app.Session, *db.SQLAdapter) error
}

func runWithDependencies(args []string, getwd func() (string, error), deps runDependencies) (err error) {
	cwd, err := getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	resolved, err := config.ResolveCLIConnection(cwd, args)
	if err != nil {
		return err
	}

	if resolved.Connection.Type == "" {
		return nil
	}

	adapter, err := deps.open(context.Background(), resolved.Connection)
	if err != nil {
		return errors.New(app.FormatTerminalError(err))
	}
	defer func() {
		closeErr := adapter.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	return deps.start(context.Background(), app.Session{
		ConnectionName:  resolved.Name,
		ConnectionType:  resolved.Connection.Type,
		ConnectionColor: resolved.Connection.Color,
		WorkingDir:      cwd,
	}, adapter)
}
