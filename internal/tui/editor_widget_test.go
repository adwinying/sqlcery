package tui_test

import (
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/tui"
	"github.com/charmbracelet/x/ansi"
)

func TestEditorWidgetViewRendersPromptAndContent(t *testing.T) {
	widget := tui.NewEditorWidget()
	ctx := tui.EditorViewContext{
		Value:       "SELECT 1",
		Lines:       []string{"SELECT 1"},
		CursorLine:  0,
		RowOffset:   0,
		ColOffset:   8,
		Width:       80,
		Height:      10,
		Prompt:      "> ",
		PromptWidth: 2,
	}

	output := ansi.Strip(widget.View(ctx))

	if !strings.Contains(output, ">") {
		t.Fatalf("View() = %q, want prompt '>'", output)
	}
	if !strings.Contains(output, "SELECT") {
		t.Fatalf("View() = %q, want SQL keyword in output", output)
	}
	if !strings.Contains(output, "1") {
		t.Fatalf("View() = %q, want number literal in output", output)
	}
}

func TestEditorWidgetViewRendersDropdownFromSuggestions(t *testing.T) {
	widget := tui.NewEditorWidget()
	ctx := tui.EditorViewContext{
		Value:       "SEL",
		Lines:       []string{"SEL"},
		CursorLine:  0,
		RowOffset:   0,
		ColOffset:   3,
		Width:       80,
		Height:      10,
		Prompt:      "> ",
		PromptWidth: 2,
		AutocompleteSuggestions: []tui.AutocompleteSuggestion{
			{Label: "SELECT", InsertText: "SELECT", Kind: "kw"},
			{Label: "SET", InsertText: "SET", Kind: "kw"},
		},
	}

	output := ansi.Strip(widget.View(ctx))

	if !strings.Contains(output, "SELECT") {
		t.Fatalf("View() = %q, want 'SELECT' in autocomplete dropdown", output)
	}
	if !strings.Contains(output, "SET") {
		t.Fatalf("View() = %q, want 'SET' in autocomplete dropdown", output)
	}
	if !strings.Contains(output, "kw") {
		t.Fatalf("View() = %q, want kind label 'kw' in dropdown", output)
	}
}

func TestEditorWidgetViewRendersTranscriptEntries(t *testing.T) {
	widget := tui.NewEditorWidget()
	widget.AppendEntry("> ", "SELECT 1", "Results:\n1 row.")

	ctx := tui.EditorViewContext{
		Value:       "",
		Lines:       []string{""},
		CursorLine:  0,
		ColOffset:   0,
		Width:       80,
		Height:      20,
		Prompt:      "> ",
		PromptWidth: 2,
	}

	output := ansi.Strip(widget.View(ctx))

	if !strings.Contains(output, "SELECT 1") {
		t.Fatalf("View() = %q, want transcript SQL in output", output)
	}
	if !strings.Contains(output, "Results:") {
		t.Fatalf("View() = %q, want transcript output in output", output)
	}
}
