package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func collectCommandMessagesForTest(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if msg == nil {
		return nil
	}

	switch typed := msg.(type) {
	case tea.BatchMsg:
		messages := make([]tea.Msg, 0, len(typed))
		for _, nested := range typed {
			messages = append(messages, collectCommandMessagesForTest(t, nested)...)
		}
		return messages
	default:
		return []tea.Msg{typed}
	}
}

func firstCommandMessageForTest[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	var zero T
	for _, msg := range collectCommandMessagesForTest(t, cmd) {
		if typed, ok := msg.(T); ok {
			return typed
		}
	}

	t.Fatalf("command produced no message of type %T", zero)
	return zero
}
