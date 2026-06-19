package tui_test

import (
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/tui"
)

func TestRenderModal_fixedHeight(t *testing.T) {
	got := tui.RenderModal("single line", 40)
	lines := strings.Split(got, "\n")
	// top border + 16 content rows + bottom border = 18
	if len(lines) != 18 {
		t.Fatalf("RenderModal produced %d lines, want 18 (16 content + 2 border)", len(lines))
	}
}

func TestRenderModal_topAndBottomBorders(t *testing.T) {
	got := tui.RenderModal("", 40)
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

func TestRenderModal_contentLinesHaveSideBorders(t *testing.T) {
	got := tui.RenderModal("hello", 40)
	lines := strings.Split(got, "\n")
	for i, line := range lines[1 : len(lines)-1] {
		if !strings.Contains(line, "│") {
			t.Fatalf("content line %d missing side border: %q", i+1, line)
		}
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
