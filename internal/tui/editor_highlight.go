package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	rw "github.com/mattn/go-runewidth"

	"github.com/adwinying/sqlcery/internal/sql"
)

type sqlTokenKind int

const (
	sqlTokenPlain sqlTokenKind = iota
	sqlTokenKeyword
	sqlTokenString
	sqlTokenNumber
	sqlTokenComment
	sqlTokenQuotedIdentifier
	sqlTokenParameter
	sqlTokenOperator
)

type sqlStyledRune struct {
	rune rune
	kind sqlTokenKind
}

type sqlStyledLine []sqlStyledRune

type sqlLexerState struct {
	inBlockComment bool
}

type sqlSyntaxHighlighter struct {
	keywordStyle          lipgloss.Style
	stringStyle           lipgloss.Style
	numberStyle           lipgloss.Style
	commentStyle          lipgloss.Style
	quotedIdentifierStyle lipgloss.Style
	parameterStyle        lipgloss.Style
	operatorStyle         lipgloss.Style
	promptStyle           lipgloss.Style
	lineNumberStyle       lipgloss.Style
	cursorLineNumberStyle lipgloss.Style
	cursorLineStyle       lipgloss.Style
	cursorStyle           lipgloss.Style
	ghostTextStyle        lipgloss.Style
}

// editorRenderedLine is a single visual (possibly wrapped) line of the editor.
type editorRenderedLine struct {
	logicalLine int
	lineNumber  int
	runes       sqlStyledLine
	isCursor    bool
	cursorCol   int
}

func newSQLSyntaxHighlighter() sqlSyntaxHighlighter {
	return sqlSyntaxHighlighter{
		keywordStyle:          AppTheme.KeywordStyle,
		stringStyle:           AppTheme.StringStyle,
		numberStyle:           AppTheme.NumberStyle,
		commentStyle:          AppTheme.CommentStyle,
		quotedIdentifierStyle: AppTheme.QuotedIdentifierStyle,
		parameterStyle:        AppTheme.ParameterStyle,
		operatorStyle:         AppTheme.OperatorStyle,
		promptStyle:           AppTheme.PromptStyle,
		lineNumberStyle:       AppTheme.LineNumberStyle,
		cursorLineNumberStyle: AppTheme.CursorLineNumberStyle,
		cursorLineStyle:       AppTheme.CursorLineStyle,
		cursorStyle:           AppTheme.CursorStyle,
		ghostTextStyle:        AppTheme.GhostTextStyle,
	}
}

// SplitEditorLines splits a SQL editor value into logical lines.
func SplitEditorLines(value string) []string {
	if value == "" {
		return []string{""}
	}
	return strings.Split(value, "\n")
}

func editorWrapStyledLine(line sqlStyledLine, width int) []sqlStyledLine {
	if width <= 0 {
		width = 1
	}
	if len(line) == 0 {
		return []sqlStyledLine{{}}
	}

	wrapped := make([]sqlStyledLine, 0, 1)
	current := make(sqlStyledLine, 0, len(line))
	currentWidth := 0

	for _, sr := range line {
		runeWidth := max(1, rw.RuneWidth(sr.rune))
		if currentWidth > 0 && currentWidth+runeWidth > width {
			wrapped = append(wrapped, current)
			current = make(sqlStyledLine, 0, len(line))
			currentWidth = 0
		}
		current = append(current, sr)
		currentWidth += runeWidth
	}

	wrapped = append(wrapped, current)
	return wrapped
}

func (h sqlSyntaxHighlighter) renderLineContentWithGhost(line sqlStyledLine, cursorCol, width int, ghostText string) string {
	var builder strings.Builder
	currentWidth := 0
	cursorRendered := false

	for _, sr := range line {
		rendered := string(sr.rune)
		runeWidth := max(1, rw.RuneWidth(sr.rune))

		if cursorCol == currentWidth && !cursorRendered {
			builder.WriteString(h.cursorStyle.Render(rendered))
			cursorRendered = true
		} else {
			builder.WriteString(h.styleFor(sr.kind).Render(rendered))
		}
		currentWidth += runeWidth
	}

	if cursorCol == currentWidth && !cursorRendered {
		builder.WriteString(h.cursorStyle.Render(" "))
		cursorRendered = true
	}

	lineDisplayWidth := currentWidth

	if ghostText != "" && cursorCol >= 0 && cursorCol == lineDisplayWidth {
		builder.WriteString(h.ghostTextStyle.Render(ghostText))
		currentWidth += rw.StringWidth(ghostText)
	}

	paddingWidth := max(0, width-currentWidth)
	if cursorCol == currentWidth-rw.StringWidth(ghostText) && cursorRendered && paddingWidth > 0 && ghostText == "" {
		paddingWidth--
	} else if cursorCol == lineDisplayWidth && cursorRendered && ghostText == "" {
		paddingWidth = max(0, width-currentWidth-1)
	}

	if paddingWidth > 0 {
		builder.WriteString(strings.Repeat(" ", paddingWidth))
	}
	return builder.String()
}

func (h sqlSyntaxHighlighter) styleFor(kind sqlTokenKind) lipgloss.Style {
	switch kind {
	case sqlTokenKeyword:
		return h.keywordStyle
	case sqlTokenString:
		return h.stringStyle
	case sqlTokenNumber:
		return h.numberStyle
	case sqlTokenComment:
		return h.commentStyle
	case sqlTokenQuotedIdentifier:
		return h.quotedIdentifierStyle
	case sqlTokenParameter:
		return h.parameterStyle
	case sqlTokenOperator:
		return h.operatorStyle
	default:
		return lipgloss.NewStyle()
	}
}

func (h sqlSyntaxHighlighter) highlightLines(lines []string) []sqlStyledLine {
	state := sqlLexerState{}
	highlighted := make([]sqlStyledLine, len(lines))
	for i, line := range lines {
		highlighted[i], state = h.highlightLine(line, state)
	}
	return highlighted
}

func (h sqlSyntaxHighlighter) highlightLine(line string, state sqlLexerState) (sqlStyledLine, sqlLexerState) {
	runes := []rune(line)
	tokens, endInBlockComment := sql.Lex(line, state.inBlockComment)

	styled := make(sqlStyledLine, 0, len(runes))
	for _, token := range tokens {
		kind := highlightTokenKind(token.Kind)
		for j := token.Start; j < token.End; j++ {
			styled = append(styled, sqlStyledRune{rune: runes[j], kind: kind})
		}
	}

	state.inBlockComment = endInBlockComment
	return styled, state
}

// highlightTokenKind maps a sql.TokenKind to the editor's styling kind.
// Identifiers and whitespace render unstyled, as does any unclassified rune;
// operators and punctuation share the operator style.
func highlightTokenKind(kind sql.TokenKind) sqlTokenKind {
	switch kind {
	case sql.KindKeyword:
		return sqlTokenKeyword
	case sql.KindString:
		return sqlTokenString
	case sql.KindNumber:
		return sqlTokenNumber
	case sql.KindComment:
		return sqlTokenComment
	case sql.KindQuotedIdentifier:
		return sqlTokenQuotedIdentifier
	case sql.KindParameter:
		return sqlTokenParameter
	case sql.KindOperator, sql.KindPunctuation:
		return sqlTokenOperator
	default:
		return sqlTokenPlain
	}
}
