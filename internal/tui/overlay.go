package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	ModalMinWidth      = 30
	ModalMaxWidth      = 64
	ModalFixedRows     = 16 // single-box inner height (no filter)
	ModalDialogRows    = 5  // compact dialog inner height (prompt + buttons)
	ModalFilterRows    = 1  // filter box inner height
	ModalSplitListRows = 13 // suggestions box inner height when filter is present (ModalFixedRows - ModalFilterRows - 2 border rows)
)

// RenderTitledBox wraps content in a rounded-border box. If title is non-empty
// it is embedded in the top border as ╭─Title────╮. If counter is non-empty
// it is embedded in the bottom border as ╰────1 of 6─╯. Content lines are
// padded or truncated to exactly rows inner rows.
func RenderTitledBox(title, content, counter string, maxOuterWidth, rows int) string {
	lines := strings.Split(content, "\n")

	innerWidth := maxOuterWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	for len(lines) < rows {
		lines = append(lines, "")
	}
	lines = lines[:rows]

	bs := lipgloss.NewStyle().Foreground(AppTheme.ModalBorder.GetForeground())

	var topLine string
	if title == "" {
		topLine = bs.Render("╭" + strings.Repeat("─", innerWidth) + "╮")
	} else {
		titleW := ansi.StringWidth(title)
		dashes := innerWidth - titleW - 1 // 1 dash before title
		if dashes < 0 {
			dashes = 0
		}
		topLine = bs.Render("╭─") + AppTheme.PanelTitle.Render(title) + bs.Render(strings.Repeat("─", dashes)+"╮")
	}
	var bottomLine string
	if counter == "" {
		bottomLine = bs.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	} else {
		counterW := ansi.StringWidth(counter)
		dashes := innerWidth - counterW - 1 // 1 dash after counter (before ╯)
		if dashes < 0 {
			dashes = 0
		}
		bottomLine = bs.Render("╰"+strings.Repeat("─", dashes)) + AppTheme.PanelMuted.Render(counter) + bs.Render("─╯")
	}

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

// RenderPane wraps content in a rounded border with an optional title and
// counter. active panes receive the accent border colour; inactive panes
// receive the muted colour. outerWidth is the full column width including the
// two border characters; innerHeight is the number of content rows between
// borders. If counter is non-empty it is embedded in the bottom border as
// ╰────counter─╯.
func RenderPane(content, title string, active bool, outerWidth, innerHeight int, counter string) string {
	borderColor := AppTheme.PaneBorderInactive.GetForeground()
	if active {
		borderColor = AppTheme.PaneBorderActive.GetForeground()
	}
	innerWidth := outerWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 0 {
		innerHeight = 0
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	var topLine string
	if title != "" {
		titleRendered := AppTheme.PanelTitle.Render(title)
		titleVisualWidth := ansi.StringWidth(title)
		dashesAfter := innerWidth - 1 - titleVisualWidth - 1
		if dashesAfter < 0 {
			dashesAfter = 0
		}
		topLine = borderStyle.Render("╭─") + titleRendered + borderStyle.Render(" "+strings.Repeat("─", dashesAfter)+"╮")
	} else {
		topLine = borderStyle.Render("╭" + strings.Repeat("─", innerWidth) + "╮")
	}
	var bottomLine string
	if counter == "" {
		bottomLine = borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	} else {
		counterW := ansi.StringWidth(counter)
		dashes := innerWidth - counterW - 1
		if dashes < 0 {
			dashes = 0
		}
		bottomLine = borderStyle.Render("╰"+strings.Repeat("─", dashes)) + AppTheme.PanelMuted.Render(counter) + borderStyle.Render("─╯")
	}

	contentLines := strings.Split(content, "\n")
	for len(contentLines) < innerHeight {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}

	lines := make([]string, 0, innerHeight+2)
	lines = append(lines, topLine)
	for _, cl := range contentLines {
		visibleWidth := ansi.StringWidth(cl)
		padding := ""
		if visibleWidth < innerWidth {
			padding = strings.Repeat(" ", innerWidth-visibleWidth)
		}
		if visibleWidth > innerWidth {
			cl = ansi.Truncate(cl, innerWidth, "")
			padding = ""
		}
		lines = append(lines, borderStyle.Render("│")+cl+padding+borderStyle.Render("│"))
	}
	lines = append(lines, bottomLine)

	return strings.Join(lines, "\n")
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
// Wide (e.g. CJK) characters that straddle a boundary are replaced with a space
// so that the total line width stays exactly bgW columns.
func OverlayLine(bg, fg string, xOffset, bgW int) string {
	fgW := ansi.StringWidth(fg)
	rightStart := xOffset + fgW

	left := ansi.Cut(bg, 0, xOffset)
	// ansi.Truncate clips a wide char that straddles xOffset without padding.
	// Pad to the expected column so the modal doesn't shift left on those rows.
	if leftW := ansi.StringWidth(left); leftW < xOffset {
		left += strings.Repeat(" ", xOffset-leftW)
	}

	right := ansi.Cut(bg, rightStart, bgW)
	rightWidth := max(0, bgW-rightStart)
	if ansi.StringWidth(right) > rightWidth {
		// ansi.TruncateLeft included a wide char that starts 1 column before
		// rightStart. Skip it by cutting from rightStart+1 and pad with a space
		// to represent the consumed right-half of that character.
		right = " " + ansi.Cut(bg, rightStart+1, bgW)
		right = ansi.Truncate(right, rightWidth, "") // safety: trim if still wide
	}

	return left + fg + right
}
