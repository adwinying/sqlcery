package tui_test

import (
	"testing"

	"github.com/adwinying/sqlcery/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

func TestResolveColor_namedColor(t *testing.T) {
	got := tui.ResolveColor("red")
	if got != lipgloss.Color("1") {
		t.Fatalf("ResolveColor(\"red\") = %q, want %q", got, "1")
	}
}

func TestResolveColor_brightVariant(t *testing.T) {
	got := tui.ResolveColor("bright-blue")
	if got != lipgloss.Color("12") {
		t.Fatalf("ResolveColor(\"bright-blue\") = %q, want %q", got, "12")
	}
}

func TestResolveColor_alias(t *testing.T) {
	got := tui.ResolveColor("orange")
	if got != lipgloss.Color("214") {
		t.Fatalf("ResolveColor(\"orange\") = %q, want %q", got, "214")
	}
}

func TestResolveColor_hexPassthrough(t *testing.T) {
	got := tui.ResolveColor("#FF0000")
	if got != lipgloss.Color("#FF0000") {
		t.Fatalf("ResolveColor(\"#FF0000\") = %q, want %q", got, "#FF0000")
	}
}

func TestResolveColor_ansiNumberPassthrough(t *testing.T) {
	got := tui.ResolveColor("196")
	if got != lipgloss.Color("196") {
		t.Fatalf("ResolveColor(\"196\") = %q, want %q", got, "196")
	}
}

func TestResolveColor_caseInsensitive(t *testing.T) {
	got := tui.ResolveColor("RED")
	if got != lipgloss.Color("1") {
		t.Fatalf("ResolveColor(\"RED\") = %q, want %q", got, "1")
	}
}

func TestResolveColor_trimsWhitespace(t *testing.T) {
	got := tui.ResolveColor("  red  ")
	if got != lipgloss.Color("1") {
		t.Fatalf("ResolveColor(\"  red  \") = %q, want %q", got, "1")
	}
}
