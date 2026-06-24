package app

// mouseRegion identifies which logical pane a mouse coordinate lands in.
type mouseRegion int

const (
	mouseRegionNone    mouseRegion = iota
	mouseRegionResults             // interior of the Results pane
	mouseRegionCommand             // interior of the Command pane
)

// hitTestPane maps a terminal coordinate (x, y) to the mouseRegion it falls
// in, using the same row arithmetic as (*Model).syncPaneSizes and
// (Model).readyStateView.
//
// Parameters:
//   - layout     — current AppLayout
//   - width      — terminal width (columns)
//   - height     — terminal height (rows)
//   - splitRatio — fraction of contentHeight given to the Results pane in LayoutSplit
//   - x, y       — 0-based click coordinate
func hitTestPane(layout AppLayout, width, height int, splitRatio float64, x, y int) mouseRegion {
	// Status bar occupies the last row.
	if y < 0 || y >= height || x < 0 || x >= width {
		return mouseRegionNone
	}
	if y == height-1 {
		return mouseRegionNone
	}

	// Left and right border columns.
	if x == 0 || x == width-1 {
		return mouseRegionNone
	}

	contentHeight := height - 1
	if contentHeight < 2 {
		contentHeight = 2
	}

	switch layout {
	case LayoutResultsOnly:
		// Single pane fills contentHeight; rows 0 and contentHeight-1 are borders.
		if y == 0 || y == contentHeight-1 {
			return mouseRegionNone
		}
		return mouseRegionResults

	case LayoutCommandOnly:
		// Single pane fills contentHeight; rows 0 and contentHeight-1 are borders.
		if y == 0 || y == contentHeight-1 {
			return mouseRegionNone
		}
		return mouseRegionCommand

	default: // LayoutSplit
		resultsPaneOuterH := max(3, int(float64(contentHeight)*splitRatio))
		commandOuterH := max(3, contentHeight-resultsPaneOuterH)
		if resultsPaneOuterH+commandOuterH > contentHeight {
			resultsPaneOuterH = contentHeight - commandOuterH
		}

		// Results pane occupies rows [0, resultsPaneOuterH).
		// Command pane occupies rows [resultsPaneOuterH, contentHeight).
		if y < resultsPaneOuterH {
			// Inside results block.
			topBorder := 0
			bottomBorder := resultsPaneOuterH - 1
			if y == topBorder || y == bottomBorder {
				return mouseRegionNone
			}
			return mouseRegionResults
		}
		// Inside command block.
		relY := y - resultsPaneOuterH
		topBorder := 0
		bottomBorder := commandOuterH - 1
		if relY == topBorder || relY == bottomBorder {
			return mouseRegionNone
		}
		return mouseRegionCommand
	}
}
