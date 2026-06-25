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

var (
	version string
	commit  string
)

func buildVersion() string {
	v := version
	if v == "" {
		v = "dev"
	}
	c := commit
	if c == "" {
		c = "unknown"
	}
	return fmt.Sprintf("%s (%s)", v, c)
}

func main() {
	if err := run(os.Args[1:], os.Getwd); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, getwd func() (string, error)) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(buildVersion())
		return nil
	}
	return runWithDependencies(args, getwd, runDependencies{
		open: db.Open,
		start: func(ctx context.Context, session app.Session) error {
			history, err := apphistory.NewPersistentHistory(session.ConnectionName)
			if err != nil {
				return err
			}
			return app.Run(ctx, session, app.RunOptions{History: history, Version: buildVersion()})
		},
	})
}

type runDependencies struct {
	open  func(context.Context, config.Connection) (*db.SQLAdapter, error)
	start func(context.Context, app.Session) error
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

	cfg, err := config.Load[config.Config](cwd)
	if err != nil {
		return err
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
		DatabaseType:    resolved.Connection.Type,
		ConnectionColor: resolved.Connection.Color,
		WorkingDir:      cwd,
		Adapter:         adapter,
		MouseDisabled:   cfg.Value.MouseDisabled,
	})
}
