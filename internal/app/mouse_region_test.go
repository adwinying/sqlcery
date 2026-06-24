package app

import "testing"

func TestHitTestPane(t *testing.T) {
	// Shared geometry for split-layout tests:
	//   height=20, splitRatio=0.65
	//   contentHeight = 19
	//   resultsPaneOuterH = max(3, int(19*0.65)) = max(3,12) = 12
	//   commandOuterH     = max(3, 19-12)        = max(3, 7) = 7
	//   sum = 19 == contentHeight, no correction needed
	//
	// Results pane rows [0,11]:  top border=0, bottom border=11, interior=[1,10]
	// Command pane rows [12,18]: top border=12, bottom border=18, interior=[13,17]
	// Status bar row: 19

	const (
		w     = 40
		h     = 20
		ratio = 0.65
	)

	tests := []struct {
		name   string
		layout AppLayout
		x, y   int
		want   mouseRegion
	}{
		// ── LayoutSplit ──────────────────────────────────────────────
		{
			name:   "split: status bar row",
			layout: LayoutSplit,
			x:      5, y: h - 1, // y=19
			want: mouseRegionNone,
		},
		{
			name:   "split: results top border",
			layout: LayoutSplit,
			x:      5, y: 0,
			want: mouseRegionNone,
		},
		{
			name:   "split: results bottom border",
			layout: LayoutSplit,
			x:      5, y: 11,
			want: mouseRegionNone,
		},
		{
			name:   "split: results interior first row",
			layout: LayoutSplit,
			x:      5, y: 1,
			want: mouseRegionResults,
		},
		{
			name:   "split: results interior last row",
			layout: LayoutSplit,
			x:      5, y: 10,
			want: mouseRegionResults,
		},
		{
			name:   "split: command top border (boundary line)",
			layout: LayoutSplit,
			x:      5, y: 12,
			want: mouseRegionNone,
		},
		{
			name:   "split: command interior first row",
			layout: LayoutSplit,
			x:      5, y: 13,
			want: mouseRegionCommand,
		},
		{
			name:   "split: command interior last row",
			layout: LayoutSplit,
			x:      5, y: 17,
			want: mouseRegionCommand,
		},
		{
			name:   "split: command bottom border",
			layout: LayoutSplit,
			x:      5, y: 18,
			want: mouseRegionNone,
		},
		{
			name:   "split: left border column",
			layout: LayoutSplit,
			x:      0, y: 5,
			want: mouseRegionNone,
		},
		{
			name:   "split: right border column",
			layout: LayoutSplit,
			x:      w - 1, y: 5,
			want: mouseRegionNone,
		},

		// ── LayoutResultsOnly ────────────────────────────────────────
		// contentHeight=19; pane rows [0,18]; interior=[1,17]; status bar=19
		{
			name:   "results-only: top border",
			layout: LayoutResultsOnly,
			x:      5, y: 0,
			want: mouseRegionNone,
		},
		{
			name:   "results-only: bottom border",
			layout: LayoutResultsOnly,
			x:      5, y: 18,
			want: mouseRegionNone,
		},
		{
			name:   "results-only: interior",
			layout: LayoutResultsOnly,
			x:      5, y: 9,
			want: mouseRegionResults,
		},
		{
			name:   "results-only: status bar",
			layout: LayoutResultsOnly,
			x:      5, y: h - 1,
			want: mouseRegionNone,
		},
		{
			name:   "results-only: left border",
			layout: LayoutResultsOnly,
			x:      0, y: 9,
			want: mouseRegionNone,
		},
		{
			name:   "results-only: right border",
			layout: LayoutResultsOnly,
			x:      w - 1, y: 9,
			want: mouseRegionNone,
		},

		// ── LayoutCommandOnly ────────────────────────────────────────
		// Same geometry: contentHeight=19; pane rows [0,18]; interior=[1,17]; status bar=19
		{
			name:   "command-only: top border",
			layout: LayoutCommandOnly,
			x:      5, y: 0,
			want: mouseRegionNone,
		},
		{
			name:   "command-only: bottom border",
			layout: LayoutCommandOnly,
			x:      5, y: 18,
			want: mouseRegionNone,
		},
		{
			name:   "command-only: interior",
			layout: LayoutCommandOnly,
			x:      5, y: 9,
			want: mouseRegionCommand,
		},
		{
			name:   "command-only: status bar",
			layout: LayoutCommandOnly,
			x:      5, y: h - 1,
			want: mouseRegionNone,
		},
		{
			name:   "command-only: left border",
			layout: LayoutCommandOnly,
			x:      0, y: 9,
			want: mouseRegionNone,
		},
		{
			name:   "command-only: right border",
			layout: LayoutCommandOnly,
			x:      w - 1, y: 9,
			want: mouseRegionNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hitTestPane(tc.layout, w, h, ratio, tc.x, tc.y)
			if got != tc.want {
				t.Errorf("hitTestPane(%s, w=%d, h=%d, ratio=%v, x=%d, y=%d) = %v, want %v",
					tc.layout, w, h, ratio, tc.x, tc.y, got, tc.want)
			}
		})
	}
}
