package app

import (
	"database/sql"

	"github.com/BurntSushi/toml"
	bubbleshelp "charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

type Stack struct {
	DB     *sql.DB
	Root   tea.Model
	Help   bubbleshelp.Model
	Style  lipgloss.Style
	Config ConfigCodec
}

type ConfigCodec interface {
	Decode(data []byte, target any) (toml.MetaData, error)
}

type BurntSushiCodec struct{}

func NewStack() Stack {
	return Stack{
		Help:   bubbleshelp.New(),
		Style:  lipgloss.NewStyle(),
		Config: BurntSushiCodec{},
	}
}

func (BurntSushiCodec) Decode(data []byte, target any) (toml.MetaData, error) {
	return toml.Decode(string(data), target)
}
