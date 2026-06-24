package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModelViewMouseModeEnabledByDefault(t *testing.T) {
	model := NewModel(Session{MouseDisabled: false})
	model.state.SetReady("", NotificationNone)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	if got, want := model.View().MouseMode, tea.MouseModeCellMotion; got != want {
		t.Fatalf("View().MouseMode = %v, want %v", got, want)
	}
}

func TestModelViewMouseModeDisabledWhenSessionDisabled(t *testing.T) {
	model := NewModel(Session{MouseDisabled: true})
	model.state.SetReady("", NotificationNone)
	next, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = next.(Model)

	if got, want := model.View().MouseMode, tea.MouseModeNone; got != want {
		t.Fatalf("View().MouseMode = %v, want %v", got, want)
	}
}
