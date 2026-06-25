package tui

import (
	"strings"
	"testing"
)

// styledSegment is a run of runes sharing the same token kind.
type styledSegment struct {
	text string
	kind sqlTokenKind
}

func compactStyledSegments(line sqlStyledLine) []styledSegment {
	if len(line) == 0 {
		return nil
	}
	segments := make([]styledSegment, 0, len(line))
	current := styledSegment{kind: line[0].kind}
	for _, sr := range line {
		if sr.kind != current.kind && current.text != "" {
			segments = append(segments, current)
			current = styledSegment{kind: sr.kind}
		}
		current.text += string(sr.rune)
	}
	if current.text != "" {
		segments = append(segments, current)
	}
	return segments
}

func assertStyledSegmentKind(t *testing.T, segments []styledSegment, text string, want sqlTokenKind) {
	t.Helper()
	for _, segment := range segments {
		if segment.text == text {
			if segment.kind != want {
				t.Fatalf("segment %q kind = %v, want %v", text, segment.kind, want)
			}
			return
		}
	}
	t.Fatalf("segment %q not found in %#v", text, segments)
}

func assertStyledSegmentContainsKind(t *testing.T, segments []styledSegment, text string, want sqlTokenKind) {
	t.Helper()
	for _, segment := range segments {
		if strings.Contains(segment.text, text) {
			if segment.kind != want {
				t.Fatalf("segment containing %q kind = %v, want %v", text, segment.kind, want)
			}
			return
		}
	}
	t.Fatalf("segment containing %q not found in %#v", text, segments)
}

func TestSQLSyntaxHighlighterHighlightsCommonTokens(t *testing.T) {
	highlighter := newSQLSyntaxHighlighter()
	line, _ := highlighter.highlightLine(`SELECT "users".name, 42, 'Ada', @id -- comment`, sqlLexerState{})
	segments := compactStyledSegments(line)

	assertStyledSegmentKind(t, segments, "SELECT", sqlTokenKeyword)
	assertStyledSegmentKind(t, segments, `"users"`, sqlTokenQuotedIdentifier)
	assertStyledSegmentKind(t, segments, "42", sqlTokenNumber)
	assertStyledSegmentKind(t, segments, "'Ada'", sqlTokenString)
	assertStyledSegmentKind(t, segments, "@id", sqlTokenParameter)
	assertStyledSegmentKind(t, segments, "-- comment", sqlTokenComment)
	assertStyledSegmentKind(t, segments, "name", sqlTokenPlain)
	assertStyledSegmentKind(t, segments, ".", sqlTokenOperator)
}

func TestSQLSyntaxHighlighterTracksBlockCommentsAcrossLines(t *testing.T) {
	highlighter := newSQLSyntaxHighlighter()
	lines := highlighter.highlightLines([]string{
		"SELECT 1 /* open comment",
		"still comment */ FROM widgets",
	})

	firstSegments := compactStyledSegments(lines[0])
	secondSegments := compactStyledSegments(lines[1])

	assertStyledSegmentKind(t, firstSegments, "SELECT", sqlTokenKeyword)
	assertStyledSegmentKind(t, firstSegments, "/* open comment", sqlTokenComment)
	assertStyledSegmentKind(t, secondSegments, "still comment */", sqlTokenComment)
	assertStyledSegmentKind(t, secondSegments, "FROM", sqlTokenKeyword)
	assertStyledSegmentContainsKind(t, secondSegments, "widgets", sqlTokenPlain)
}

func TestRenderLineContentWithGhostCJKCursorPosition(t *testing.T) {
	h := newSQLSyntaxHighlighter()

	// "世A": 世 is full-width (display width 2), A is ASCII (display width 1)
	line, _ := h.highlightLine("世A", sqlLexerState{})

	// cursorCol=2 puts the cursor ON 'A'
	rendered := h.renderLineContentWithGhost(line, 2, 10, "")
	if !strings.Contains(rendered, "A") {
		t.Fatalf("renderLineContentWithGhost with CJK: expected 'A' in rendered output, got %q", rendered)
	}

	// cursorCol=3 (end of "世A") → ghost text should appear
	ghost := "GHOST"
	renderedGhost := h.renderLineContentWithGhost(line, 3, 20, ghost)
	if !strings.Contains(renderedGhost, ghost) {
		t.Fatalf("renderLineContentWithGhost with CJK and ghost: expected ghost %q at end-of-line, got %q", ghost, renderedGhost)
	}

	// cursorCol=2 (ON 'A') → ghost text must NOT appear
	renderedNoGhost := h.renderLineContentWithGhost(line, 2, 20, ghost)
	if strings.Contains(renderedNoGhost, ghost) {
		t.Fatalf("renderLineContentWithGhost with CJK: ghost must not appear when cursor is not at end, got %q", renderedNoGhost)
	}
}
