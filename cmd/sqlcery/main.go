package main

import (
	"context"
	"fmt"
	"os"

	"github.com/adwinying/sqlcery/internal/app"
	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/frecency"
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
		start: func(ctx context.Context, cwd string, autoConnectTarget config.ResolvedConnection, opts app.RunOptions) error {
			frecencyPath, err := frecency.DefaultPath()
			if err != nil {
				return fmt.Errorf("resolve frecency path: %w", err)
			}
			frecencyStore, err := frecency.Load(frecencyPath, nil)
			if err != nil {
				return fmt.Errorf("load frecency store: %w", err)
			}

			connections, err := config.LoadConnections[config.Connections](cwd)
			if err != nil {
				return err
			}

			// Shared in-memory cache so ConnectionsLoader stays a cheap read
			// (called once per visible row per render frame).
			cache := connections.Value
			opts.Version = buildVersion()
			opts.FrecencyStore = frecencyStore
			opts.ConnectionsLoader = func() (config.Connections, error) { return cache, nil }
			opts.ReloadConnections = func() error {
				r, err := config.LoadConnections[config.Connections](cwd)
				if err == nil {
					cache = r.Value
				}
				return err
			}
			opts.AutoConnectTarget = autoConnectTarget

			return app.Run(ctx, opts)
		},
	})
}

type runDependencies struct {
	open  func(context.Context, config.Connection) (*db.SQLAdapter, error)
	start func(context.Context, string, config.ResolvedConnection, app.RunOptions) error
}

func runWithDependencies(args []string, getwd func() (string, error), deps runDependencies) error {
	cwd, err := getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	resolved, err := config.ResolveCLIConnection(cwd, args)
	if err != nil {
		return err
	}

	cfg, err := config.Load[config.Config](cwd)
	if err != nil {
		return err
	}

	return deps.start(context.Background(), cwd, resolved, app.RunOptions{
		Open:              deps.open,
		MouseDisabled:     cfg.Value.MouseDisabled,
		WorkingDir:        cwd,
		AutoConnectTarget: resolved,
	})
}
