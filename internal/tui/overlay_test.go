package tui_test

import (
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/tui"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderTitledBox_fixedHeight(t *testing.T) {
	got := tui.RenderTitledBox("", "single line", "", 40, tui.ModalFixedRows)
	lines := strings.Split(got, "\n")
	// top border + 16 content rows + bottom border = 18
	if len(lines) != 18 {
		t.Fatalf("RenderTitledBox produced %d lines, want 18 (16 content + 2 border)", len(lines))
	}
}

func TestRenderTitledBox_topAndBottomBorders(t *testing.T) {
	got := tui.RenderTitledBox("", "", "", 40, tui.ModalFixedRows)
	lines := strings.Split(got, "\n")
	top := lines[0]
	bottom := lines[len(lines)-1]
	if !strings.Contains(top, "╭") || !strings.Contains(top, "╮") {
		t.Fatalf("top border missing rounded corners, got: %q", top)
	}
	if !strings.Contains(bottom, "╰") || !strings.Contains(bottom, "╯") {
		t.Fatalf("bottom border missing rounded corners, got: %q", bottom)
	}
}

func TestRenderTitledBox_contentLinesHaveSideBorders(t *testing.T) {
	got := tui.RenderTitledBox("", "hello", "", 40, tui.ModalFixedRows)
	lines := strings.Split(got, "\n")
	for i, line := range lines[1 : len(lines)-1] {
		if !strings.Contains(line, "│") {
			t.Fatalf("content line %d missing side border: %q", i+1, line)
		}
	}
}

func TestRenderTitledBox_titleInBorder(t *testing.T) {
	got := tui.RenderTitledBox("History", "item", "", 40, tui.ModalFixedRows)
	lines := strings.Split(got, "\n")
	top := lines[0]
	if !strings.Contains(top, "History") {
		t.Fatalf("top border does not contain title, got: %q", top)
	}
	if !strings.Contains(top, "╭") || !strings.Contains(top, "╮") {
		t.Fatalf("titled top border missing rounded corners, got: %q", top)
	}
}

func TestRenderTitledBox_counterInBottomBorder(t *testing.T) {
	got := tui.RenderTitledBox("", "item", "1 of 6", 40, tui.ModalFixedRows)
	lines := strings.Split(got, "\n")
	bottom := lines[len(lines)-1]
	if !strings.Contains(bottom, "1 of 6") {
		t.Fatalf("bottom border does not contain counter, got: %q", bottom)
	}
	if !strings.Contains(bottom, "╰") || !strings.Contains(bottom, "╯") {
		t.Fatalf("bottom border with counter missing rounded corners, got: %q", bottom)
	}
}

func TestOverlayLine_overlaysAtOffset(t *testing.T) {
	bg := "0123456789"
	fg := "XY"
	got := tui.OverlayLine(bg, fg, 3, 10)
	// columns 3-4 should be replaced with "XY"
	if !strings.Contains(got, "XY") {
		t.Fatalf("OverlayLine result does not contain fg %q: %q", fg, got)
	}
	// columns 0-2 preserved
	if !strings.HasPrefix(got, "012") {
		t.Fatalf("OverlayLine did not preserve left side, got: %q", got)
	}
	// columns 5-9 preserved
	if !strings.HasSuffix(got, "56789") {
		t.Fatalf("OverlayLine did not preserve right side, got: %q", got)
	}
}

// TestOverlayLine_wideCharAtLeftBoundary covers the case where a 2-wide CJK
// character straddles xOffset. ansi.Truncate clips it without padding, which
// would make the output line 1 column short; the fix pads with a space.
func TestOverlayLine_wideCharAtLeftBoundary(t *testing.T) {
	// "0123" (4 cols) + "東" (2 cols) + "789" (3 cols) = 9 visual cols
	bg := "0123東789"
	fg := "XY"
	bgW := 9
	// xOffset=5: "東" occupies cols 4-5, so it straddles the left boundary.
	xOffset := 5
	got := tui.OverlayLine(bg, fg, xOffset, bgW)
	if gotW := ansi.StringWidth(got); gotW != bgW {
		t.Fatalf("OverlayLine width = %d, want %d (wide char at left boundary); result: %q", gotW, bgW, got)
	}
	if !strings.Contains(got, fg) {
		t.Fatalf("OverlayLine result does not contain fg %q: %q", fg, got)
	}
}

// TestOverlayLine_wideCharAtRightBoundary covers the case where a 2-wide CJK
// character straddles xOffset+fgWidth. ansi.TruncateLeft includes it, making
// the output line 1 column too wide; the fix replaces it with a space.
func TestOverlayLine_wideCharAtRightBoundary(t *testing.T) {
	// "012" (3 cols) + "東" (2 cols) + "56789" (5 cols) = 10 visual cols
	bg := "012東56789"
	fg := "XY"
	bgW := 10
	// xOffset=2: rightStart=4; "東" occupies cols 3-4, straddling the right boundary.
	xOffset := 2
	got := tui.OverlayLine(bg, fg, xOffset, bgW)
	if gotW := ansi.StringWidth(got); gotW != bgW {
		t.Fatalf("OverlayLine width = %d, want %d (wide char at right boundary); result: %q", gotW, bgW, got)
	}
	if !strings.Contains(got, fg) {
		t.Fatalf("OverlayLine result does not contain fg %q: %q", fg, got)
	}
}

func TestOverlayCenter_fallbackWhenTooSmall(t *testing.T) {
	bg := "tiny"
	modal := strings.Repeat("x", 100)
	got := tui.OverlayCenter(bg, modal, 4, 1)
	if got != bg {
		t.Fatalf("OverlayCenter should return bg unchanged when terminal too small")
	}
}

func TestOverlayCenter_centersModal(t *testing.T) {
	bgW, bgH := 40, 20
	bgLines := make([]string, bgH)
	for i := range bgLines {
		bgLines[i] = strings.Repeat(".", bgW)
	}
	bg := strings.Join(bgLines, "\n")

	modalContent := "hello"
	got := tui.OverlayCenter(bg, modalContent, bgW, bgH)
	if got == bg {
		t.Fatal("OverlayCenter returned bg unchanged for a terminal that should fit the modal")
	}
	if !strings.Contains(got, modalContent) {
		t.Fatalf("OverlayCenter result does not contain modal content %q", modalContent)
	}
}
