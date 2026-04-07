package main

import (
	"context"
	"fmt"
	"os"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
)

func main() {
	if err := run(os.Args[1:], os.Getwd); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, getwd func() (string, error)) error {
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

	adapter, err := db.Open(context.Background(), resolved.Connection)
	if err != nil {
		return err
	}

	return adapter.Close()
}
