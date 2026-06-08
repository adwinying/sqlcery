package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	modalMinWidth  = 30
	modalMaxWidth  = 64
	modalFixedRows = 16 // fixed inner height: content is padded or clipped to this many rows
)

// renderModal wraps content in a rounded-border modal box with a fixed inner
// height of modalFixedRows rows and a fixed inner width derived from maxOuterWidth.
// Content lines are padded or truncated to fit the inner width.
// If there are fewer content lines than modalFixedRows, blank lines are added.
// If there are more, excess lines are silently dropped (the modal acts like a
// fixed-size viewport — callers are responsible for pre-scrolling the slice).
func renderModal(content string, maxOuterWidth int) string {
	lines := strings.Split(content, "\n")

	// Inner width accounts for left and right border chars (│ = 1 char each).
	innerWidth := maxOuterWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Pad or clip to exactly modalFixedRows rows.
	for len(lines) < modalFixedRows {
		lines = append(lines, "")
	}
	lines = lines[:modalFixedRows]

	bs := lipgloss.NewStyle().Foreground(appTheme.modalBorder.GetForeground())
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
			line = ansi.Truncate(line, innerWidth, "")
			padding = ""
		}
		result = append(result, bs.Render("│")+line+padding+bs.Render("│"))
	}
	result = append(result, bottomLine)

	return strings.Join(result, "\n")
}

// overlayCenter composites popup centered over bg.
// bgW and bgH are the visual dimensions of bg (width in columns, height in rows).
// If the terminal is too small to fit the popup with at least one column of
// margin on each side, bg is returned unchanged as a fallback.
func overlayCenter(bg, popup string, bgW, bgH int) string {
	popupLines := strings.Split(popup, "\n")
	popupH := len(popupLines)

	popupW := 0
	for _, line := range popupLines {
		if w := ansi.StringWidth(line); w > popupW {
			popupW = w
		}
	}

	// Require at least one column of margin on each side and one row above/below.
	if bgW < popupW+2 || bgH < popupH+2 {
		return bg
	}

	startX := (bgW - popupW) / 2
	startY := (bgH - popupH) / 2

	bgLines := strings.Split(bg, "\n")
	// Ensure we have enough rows to place the popup.
	for len(bgLines) < bgH {
		bgLines = append(bgLines, "")
	}

	result := make([]string, len(bgLines))
	copy(result, bgLines)

	for i, popupLine := range popupLines {
		targetRow := startY + i
		if targetRow < 0 || targetRow >= len(result) {
			continue
		}
		result[targetRow] = overlayLine(result[targetRow], popupLine, startX, bgW)
	}

	return strings.Join(result, "\n")
}

// overlayLine composites fg onto bg at visual column xOffset within bgW columns.
// The left background content (columns 0..xOffset-1) and right background content
// (columns xOffset+fgWidth..bgW-1) are preserved from bg; fg replaces the middle.
func overlayLine(bg, fg string, xOffset, bgW int) string {
	fgW := ansi.StringWidth(fg)
	left := ansi.Cut(bg, 0, xOffset)
	right := ansi.Cut(bg, xOffset+fgW, bgW)
	return left + fg + right
}
