package tui_test

import (
	"strings"
	"testing"

	"github.com/adwinying/sqlcery/internal/db"
	"github.com/adwinying/sqlcery/internal/tui"
	"github.com/charmbracelet/x/ansi"
)

func TestPrepareResultsPanePageComputesColumnWidths(t *testing.T) {
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(1)},
				{Kind: db.ValueKindString, Value: "Ada"},
			}},
		},
	}

	prepared := tui.PrepareResultsPanePage(result, 0)

	if prepared == nil {
		t.Fatal("PrepareResultsPanePage() = nil, want non-nil")
	}
	if got, want := prepared.Headers[0], "id"; got != want {
		t.Fatalf("Headers[0] = %q, want %q", got, want)
	}
	if got, want := prepared.Headers[1], "name"; got != want {
		t.Fatalf("Headers[1] = %q, want %q", got, want)
	}
	// "name" header width is 4; cell "Ada" width is 3 → column width = 4
	if got, want := prepared.Widths[1], 4; got != want {
		t.Fatalf("Widths[1] = %d, want %d (max of header 'name'=4 and cell 'Ada'=3)", got, want)
	}
}

func TestRenderPreparedResultsPanePageRendersTable(t *testing.T) {
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(1)},
				{Kind: db.ValueKindString, Value: "Ada"},
			}},
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(2)},
				{Kind: db.ValueKindString, Value: "Grace"},
			}},
		},
	}

	prepared := tui.PrepareResultsPanePage(result, 0)
	output := ansi.Strip(tui.RenderPreparedResultsPanePage(prepared, 80, 10, tui.ResultsPaneRenderState{}))

	for _, want := range []string{"id", "name", "1", "Ada", "2", "Grace"} {
		if !strings.Contains(output, want) {
			t.Fatalf("RenderPreparedResultsPanePage() = %q, want to contain %q", output, want)
		}
	}
	// Header separator must be present
	if !strings.Contains(output, "---") {
		t.Fatalf("RenderPreparedResultsPanePage() = %q, want separator line", output)
	}
}

func TestRenderPreparedResultsPanePageHighlightsActiveRow(t *testing.T) {
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(7)},
				{Kind: db.ValueKindString, Value: "Ada"},
			}},
		},
	}

	prepared := tui.PrepareResultsPanePage(result, 0)
	output := tui.RenderPreparedResultsPanePage(prepared, 80, 10, tui.ResultsPaneRenderState{
		Active: tui.ResultsPaneSelection{Row: 0, Column: 1, Active: true},
	})

	plain := ansi.Strip(output)
	if !strings.Contains(plain, "7") || !strings.Contains(plain, "Ada") {
		t.Fatalf("RenderPreparedResultsPanePage() = %q, want row values preserved when active", plain)
	}
}

func TestRenderPreparedResultsPanePageActiveRowSpansFullWidth(t *testing.T) {
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "id"}, {Name: "name"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindInteger, Value: int64(1)},
				{Kind: db.ValueKindString, Value: "Ada"},
			}},
		},
	}

	const paneWidth = 80
	prepared := tui.PrepareResultsPanePage(result, 0)
	output := tui.RenderPreparedResultsPanePage(prepared, paneWidth, 10, tui.ResultsPaneRenderState{
		Active: tui.ResultsPaneSelection{Row: 0, Active: true},
	})

	lines := strings.Split(ansi.Strip(output), "\n")
	var activeRowLine string
	for _, l := range lines {
		if strings.Contains(l, "Ada") {
			activeRowLine = l
			break
		}
	}
	if activeRowLine == "" {
		t.Fatalf("RenderPreparedResultsPanePage() = %q, could not find active row line", output)
	}
	if got := tui.RuneWidth(activeRowLine); got != paneWidth {
		t.Fatalf("active row display width = %d, want %d (must span full pane width)", got, paneWidth)
	}
}

func TestPrepareResultsPanePageCJKColumnWidths(t *testing.T) {
	// "名前" = 2 CJK chars → display width 4
	// "テスト長い値" = 6 CJK chars → display width 12
	result := &db.ResultSet{
		Columns: []db.ResultColumn{{Name: "名前"}, {Name: "id"}},
		Rows: []db.ResultRow{
			{Values: []db.ResultValue{
				{Kind: db.ValueKindString, Value: "中文"},
				{Kind: db.ValueKindInteger, Value: int64(1)},
			}},
			{Values: []db.ResultValue{
				{Kind: db.ValueKindString, Value: "テスト長い値"},
				{Kind: db.ValueKindInteger, Value: int64(2)},
			}},
		},
	}

	prepared := tui.PrepareResultsPanePage(result, 0)

	// "名前" header width 4; widest cell "テスト長い値" width 12 → column 0 width = 12
	if got, want := prepared.Widths[0], 12; got != want {
		t.Fatalf("Widths[0] = %d, want %d (CJK display widths must be counted as 2 each)", got, want)
	}
	// "id" header width 2; cells "1","2" width 1 → column 1 width = 2
	if got, want := prepared.Widths[1], 2; got != want {
		t.Fatalf("Widths[1] = %d, want %d", got, want)
	}
}
