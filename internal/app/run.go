package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adwinying/sqlcery/internal/db"
)

type Session struct {
	ConnectionName string
	ConnectionType string
}

type Program interface {
	Run() (tea.Model, error)
}

type ProgramFactory func(model tea.Model, opts ...tea.ProgramOption) Program

type RunOptions struct {
	NewProgram     ProgramFactory
	ProgramOptions []tea.ProgramOption
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

	_, err := newProgram(NewModel(session, adapter), programOptions...).Run()
	return err
}
