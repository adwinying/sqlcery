package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	ModalMinWidth  = 30
	ModalMaxWidth  = 64
	ModalFixedRows = 16 // fixed inner height: content is padded or clipped to this many rows
)

// RenderModal wraps content in a rounded-border modal box with a fixed inner
// height of modalFixedRows rows and a fixed inner width derived from maxOuterWidth.
// Content lines are padded or truncated to fit the inner width.
// If there are fewer content lines than modalFixedRows, blank lines are added.
// If there are more, excess lines are silently dropped (the modal acts like a
// fixed-size viewport — callers are responsible for pre-scrolling the slice).
func RenderModal(content string, maxOuterWidth int) string {
	lines := strings.Split(content, "\n")

	// Inner width accounts for left and right border chars (│ = 1 char each).
	innerWidth := maxOuterWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Pad or clip to exactly ModalFixedRows rows.
	for len(lines) < ModalFixedRows {
		lines = append(lines, "")
	}
	lines = lines[:ModalFixedRows]

	bs := lipgloss.NewStyle().Foreground(AppTheme.ModalBorder.GetForeground())
	topLine := bs.Render("╭" + strings.Repeat("─", innerWidth) + "╮")
	bottomLine := bs.Render("╰" + strings.Repeat("─", innerWidth) + "╯")

	result := make([]string, 0, len(lines)+2)
	result = append(result, topLine)
	for _, line := range lines {
		w := ansi.StringWidth(line)
		padding := ""
		if w < innerWidth {
			padding = strings.Repeat(" ", innerWidth-w)
		}
		if w > innerWidth {
			line = ansi.Truncate(line, innerWidth, "…")
			padding = ""
		}
		result = append(result, bs.Render("│")+line+padding+bs.Render("│"))
	}
	result = append(result, bottomLine)

	return strings.Join(result, "\n")
}

// ClampHScrollOffset returns the largest offset that is still meaningful for a
// view of viewW columns displaying content of totalW visible columns. At any
// non-zero offset a '<' edge marker occupies one column, so the ceiling is
// max(0, totalW-viewW+1). Callers should store the result back onto their
// scroll-offset field so that pressing left immediately moves the view.
func ClampHScrollOffset(totalW, offset, viewW int) int {
	return min(offset, max(0, totalW-viewW+1))
}

// ApplyHScroll returns a width-column slice of plain-text s starting at offset,
// inserting a '<' left-edge marker when offset > 0 and a '>' right-edge marker
// when content extends past the right edge. Each marker occupies one column.
// s must be plain text with no ANSI escape sequences.
func ApplyHScroll(s string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	totalW := ansi.StringWidth(s)

	offset = ClampHScrollOffset(totalW, offset, width)

	hasLeft := offset > 0

	contentW := width
	if hasLeft {
		contentW--
	}
	hasRight := totalW > offset+contentW
	if hasRight {
		contentW--
	}
	if contentW < 0 {
		contentW = 0
	}

	var cut string
	if contentW > 0 {
		cut = ansi.Cut(s, offset, offset+contentW)
	}
	if w := ansi.StringWidth(cut); w < contentW {
		cut += strings.Repeat(" ", contentW-w)
	}

	left, right := "", ""
	if hasLeft {
		left = "<"
	}
	if hasRight {
		right = ">"
	}
	return left + cut + right
}

// OverlayCenter composites modal centered over bg.
// bgW and bgH are the visual dimensions of bg (width in columns, height in rows).
// If the terminal is too small to fit the modal with at least one column of
// margin on each side, bg is returned unchanged as a fallback.
func OverlayCenter(bg, modal string, bgW, bgH int) string {
	modalLines := strings.Split(modal, "\n")
	modalH := len(modalLines)

	modalW := 0
	for _, line := range modalLines {
		if w := ansi.StringWidth(line); w > modalW {
			modalW = w
		}
	}

	// Require at least one column of margin on each side and one row above/below.
	if bgW < modalW+2 || bgH < modalH+2 {
		return bg
	}

	startX := (bgW - modalW) / 2
	startY := (bgH - modalH) / 2

	bgLines := strings.Split(bg, "\n")
	// Ensure we have enough rows to place the modal.
	for len(bgLines) < bgH {
		bgLines = append(bgLines, "")
	}

	result := make([]string, len(bgLines))
	copy(result, bgLines)

	for i, modalLine := range modalLines {
		targetRow := startY + i
		if targetRow < 0 || targetRow >= len(result) {
			continue
		}
		result[targetRow] = OverlayLine(result[targetRow], modalLine, startX, bgW)
	}

	return strings.Join(result, "\n")
}

// OverlayLine composites fg onto bg at visual column xOffset within bgW columns.
// The left background content (columns 0..xOffset-1) and right background content
// (columns xOffset+fgWidth..bgW-1) are preserved from bg; fg replaces the middle.
func OverlayLine(bg, fg string, xOffset, bgW int) string {
	fgW := ansi.StringWidth(fg)
	left := ansi.Cut(bg, 0, xOffset)
	right := ansi.Cut(bg, xOffset+fgW, bgW)
	return left + fg + right
}
