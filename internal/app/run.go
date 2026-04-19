package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/db"
	apphistory "github.com/adwinying/sqlcery/internal/history"
)

type Session struct {
	ConnectionName string
	ConnectionType string
	WorkingDir     string
}

type Program interface {
	Run() (tea.Model, error)
}

type ProgramFactory func(model tea.Model, opts ...tea.ProgramOption) Program

type RunOptions struct {
	NewProgram     ProgramFactory
	ProgramOptions []tea.ProgramOption
	History        *apphistory.Session
}

func Run(ctx context.Context, session Session, adapter *db.SQLAdapter, options RunOptions) error {
	if adapter == nil {
		return fmt.Errorf("adapter is required")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	newProgram := options.NewProgram
	if newProgram == nil {
		newProgram = func(model tea.Model, opts ...tea.ProgramOption) Program {
			return tea.NewProgram(model, opts...)
		}
	}

	programOptions := make([]tea.ProgramOption, 0, len(options.ProgramOptions)+1)
	programOptions = append(programOptions, tea.WithContext(ctx))
	programOptions = append(programOptions, options.ProgramOptions...)

	model := newModelWithDependencies(session, adapter, modelDependencies{history: options.History})
	_, err := newProgram(model, programOptions...).Run()
	return err
}
