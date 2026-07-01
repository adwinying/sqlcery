package app

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/config"
	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

// Session carries the live runtime connection once an Adapter is open.
// It is populated inside the Model after a successful open — not before.
type Session struct {
	ConnectionName     string
	ConnectionIdentity config.ConnectionIdentity
	DatabaseType       string
	ConnectionColor    string
	WorkingDir         string
	Adapter            *db.SQLAdapter
	MouseDisabled      bool
}

type Program interface {
	Run() (tea.Model, error)
}

type ProgramFactory func(model tea.Model, opts ...tea.ProgramOption) Program

// FrecencyStore is the interface the Model uses to record and order opens.
type FrecencyStore interface {
	RecordOpen(name string) error
	Order(names []string) []string
}

// RunOptions configures the TUI program. Open, ConnectionsLoader, and
// FrecencyStore are injected so they can be faked in tests.
type RunOptions struct {
	// Open opens a database Adapter from a Connection config. Required.
	Open func(context.Context, config.Connection) (*db.SQLAdapter, error)
	// NewHistory builds a persistent History for the given opaque Connection Identity.
	// Defaults to apphistory.NewPersistentHistory when nil.
	NewHistory func(config.ConnectionIdentity) (*apphistory.History, error)
	// ConnectionsLoader returns the current named Connections for the Picker.
	// Defaults to an empty Connections when nil.
	ConnectionsLoader func() (config.Connections, error)
	// ReloadConnections re-reads the connections files from disk and refreshes
	// the ConnectionsLoader cache. Called after a successful wizard write.
	// When nil the cache is not updated (safe to omit in tests).
	ReloadConnections func() error
	// FrecencyStore records and orders Connection opens. When nil frecency
	// recording and ordering are skipped (no-op).
	FrecencyStore FrecencyStore
	// AutoConnectTarget, when non-empty (Type != ""), causes the Model to
	// attempt an open immediately instead of showing the Connection Picker.
	AutoConnectTarget config.ResolvedConnection
	// NewProgram overrides the tea.Program factory (for testing).
	NewProgram ProgramFactory
	// ProgramOptions are extra options passed to the tea.Program.
	ProgramOptions []tea.ProgramOption
	// Version string shown in the empty-state.
	Version string
	// MouseDisabled mirrors the config setting.
	MouseDisabled bool
	// WorkingDir is the working directory for the session.
	WorkingDir string
}

// Run launches the TUI. A nil Adapter is now the normal startup path when
// no auto-connect target is given — the Model will show the Connection Picker.
func Run(ctx context.Context, options RunOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	newProgram := options.NewProgram
	if newProgram == nil {
		newProgram = func(model tea.Model, opts ...tea.ProgramOption) Program {
			return tea.NewProgram(model, opts...)
		}
	}

	if options.NewHistory == nil {
		options.NewHistory = func(identity config.ConnectionIdentity) (*apphistory.History, error) {
			return apphistory.NewPersistentHistory(string(identity))
		}
	}

	programOptions := make([]tea.ProgramOption, 0, len(options.ProgramOptions)+1)
	programOptions = append(programOptions, tea.WithContext(ctx))
	programOptions = append(programOptions, options.ProgramOptions...)

	model := newModelWithDependencies(Session{
		WorkingDir:    options.WorkingDir,
		MouseDisabled: options.MouseDisabled,
	}, modelDependencies{
		version:           options.Version,
		open:              options.Open,
		newHistory:        options.NewHistory,
		connectionsLoader: options.ConnectionsLoader,
		reloadConnections: options.ReloadConnections,
		frecencyStore:     options.FrecencyStore,
		autoConnectTarget: options.AutoConnectTarget,
	})
	_, err := newProgram(model, programOptions...).Run()
	return err
}
