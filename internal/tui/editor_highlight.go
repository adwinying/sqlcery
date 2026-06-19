package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	rw "github.com/mattn/go-runewidth"
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
	placeholderStyle      lipgloss.Style
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
		placeholderStyle:      AppTheme.PlaceholderStyle,
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

func (h sqlSyntaxHighlighter) renderLineContent(line sqlStyledLine, cursorCol, width int, placeholder bool) string {
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
			builder.WriteString(h.styleFor(sr.kind, placeholder).Render(rendered))
		}
		currentWidth += runeWidth
	}

	if cursorCol == currentWidth && !cursorRendered {
		builder.WriteString(h.cursorStyle.Render(" "))
		cursorRendered = true
	}

	paddingWidth := max(0, width-currentWidth)
	if cursorCol == currentWidth && cursorRendered && paddingWidth > 0 {
		paddingWidth--
	}
	if paddingWidth > 0 {
		builder.WriteString(strings.Repeat(" ", paddingWidth))
	}
	return builder.String()
}

func (h sqlSyntaxHighlighter) renderLineContentWithGhost(line sqlStyledLine, cursorCol, width int, placeholder bool, ghostText string) string {
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
			builder.WriteString(h.styleFor(sr.kind, placeholder).Render(rendered))
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

func (h sqlSyntaxHighlighter) styleFor(kind sqlTokenKind, placeholder bool) lipgloss.Style {
	if placeholder {
		return h.placeholderStyle
	}
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
	styled := make(sqlStyledLine, 0, len(runes))

	for i := 0; i < len(runes); {
		if state.inBlockComment {
			end := editorIndexOfBlockCommentEnd(runes, i)
			if end < 0 {
				styled = editorAppendStyledRunes(styled, runes[i:], sqlTokenComment)
				return styled, state
			}
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenComment)
			state.inBlockComment = false
			i = end
			continue
		}

		switch {
		case editorStartsWithRunes(runes, i, '-', '-'):
			styled = editorAppendStyledRunes(styled, runes[i:], sqlTokenComment)
			return styled, state
		case editorStartsWithRunes(runes, i, '/', '*'):
			end := editorIndexOfBlockCommentEnd(runes, i)
			if end < 0 {
				styled = editorAppendStyledRunes(styled, runes[i:], sqlTokenComment)
				state.inBlockComment = true
				return styled, state
			}
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenComment)
			i = end
		case runes[i] == '\'':
			end := editorConsumeQuotedLiteral(runes, i, '\'')
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenString)
			i = end
		case runes[i] == '"' || runes[i] == '`':
			end := editorConsumeQuotedLiteral(runes, i, runes[i])
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenQuotedIdentifier)
			i = end
		case runes[i] == '[':
			end := editorConsumeBracketIdentifier(runes, i)
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenQuotedIdentifier)
			i = end
		case editorIsNamedParameterStart(runes, i):
			end := editorConsumeNamedParameter(runes, i)
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenParameter)
			i = end
		case runes[i] == '?':
			styled = editorAppendStyledRunes(styled, runes[i:i+1], sqlTokenParameter)
			i++
		case unicode.IsDigit(runes[i]):
			end := editorConsumeNumber(runes, i)
			styled = editorAppendStyledRunes(styled, runes[i:end], sqlTokenNumber)
			i = end
		case IsIdentifierStart(runes[i]):
			end := ConsumeIdentifier(runes, i)
			kind := sqlTokenPlain
			if _, ok := editorSQLKeywords[strings.ToUpper(string(runes[i:end]))]; ok {
				kind = sqlTokenKeyword
			}
			styled = editorAppendStyledRunes(styled, runes[i:end], kind)
			i = end
		case editorIsOperatorRune(runes[i]):
			styled = editorAppendStyledRunes(styled, runes[i:i+1], sqlTokenOperator)
			i++
		default:
			styled = editorAppendStyledRunes(styled, runes[i:i+1], sqlTokenPlain)
			i++
		}
	}

	return styled, state
}

// IsIdentifierStart reports whether r can start a SQL identifier.
// Exported so internal/app's sql_lex.go can call it without a circular import.
func IsIdentifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

// IsIdentifierPart reports whether r can appear inside a SQL identifier.
func IsIdentifierPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

// ConsumeIdentifier returns the end index after consuming a SQL identifier
// starting at start. Exported for use by internal/app's sql_lex.go.
func ConsumeIdentifier(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && IsIdentifierPart(runes[i]) {
		i++
	}
	return i
}

func editorAppendStyledRunes(line sqlStyledLine, runes []rune, kind sqlTokenKind) sqlStyledLine {
	for _, r := range runes {
		line = append(line, sqlStyledRune{rune: r, kind: kind})
	}
	return line
}

func editorStartsWithRunes(runes []rune, index int, prefix ...rune) bool {
	if index+len(prefix) > len(runes) {
		return false
	}
	for i, r := range prefix {
		if runes[index+i] != r {
			return false
		}
	}
	return true
}

func editorIndexOfBlockCommentEnd(runes []rune, start int) int {
	for i := start + 2; i < len(runes); i++ {
		if runes[i-1] == '*' && runes[i] == '/' {
			return i + 1
		}
	}
	return -1
}

func editorConsumeQuotedLiteral(runes []rune, start int, quote rune) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] != quote {
			continue
		}
		if i+1 < len(runes) && runes[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return len(runes)
}

func editorConsumeBracketIdentifier(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == ']' {
			return i + 1
		}
	}
	return len(runes)
}

func editorIsNamedParameterStart(runes []rune, index int) bool {
	if index >= len(runes) {
		return false
	}
	switch runes[index] {
	case ':', '@':
		return index+1 < len(runes) && IsIdentifierPart(runes[index+1])
	case '$':
		return index+1 < len(runes) && (IsIdentifierPart(runes[index+1]) || unicode.IsDigit(runes[index+1]))
	default:
		return false
	}
}

func editorConsumeNamedParameter(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && (IsIdentifierPart(runes[i]) || unicode.IsDigit(runes[i])) {
		i++
	}
	return i
}

func editorConsumeNumber(runes []rune, start int) int {
	i := start
	hasDot := false
	for i < len(runes) {
		switch {
		case unicode.IsDigit(runes[i]):
			i++
		case runes[i] == '.' && !hasDot:
			hasDot = true
			i++
		case (runes[i] == 'e' || runes[i] == 'E') && i+1 < len(runes):
			i++
			if i < len(runes) && (runes[i] == '+' || runes[i] == '-') {
				i++
			}
		default:
			return i
		}
	}
	return i
}

func editorIsOperatorRune(r rune) bool {
	return strings.ContainsRune("*+-/%<>=!|.,;()", r)
}

var editorSQLKeywords = map[string]struct{}{
	"ALL": {}, "AND": {}, "AS": {}, "ASC": {}, "BETWEEN": {}, "BY": {}, "CASE": {}, "CREATE": {},
	"CROSS": {}, "DELETE": {}, "DESC": {}, "DISTINCT": {}, "DROP": {}, "ELSE": {}, "END": {},
	"EXISTS": {}, "FALSE": {}, "FROM": {}, "FULL": {}, "GROUP": {}, "HAVING": {}, "IN": {},
	"INNER": {}, "INSERT": {}, "INTO": {}, "IS": {}, "JOIN": {}, "LEFT": {}, "LIKE": {},
	"LIMIT": {}, "NOT": {}, "NULL": {}, "OFFSET": {}, "ON": {}, "OR": {}, "ORDER": {},
	"OUTER": {}, "PRIMARY": {}, "REPLACE": {}, "RETURNING": {}, "RIGHT": {}, "SELECT": {},
	"SET": {}, "TABLE": {}, "THEN": {}, "TRUE": {}, "UNION": {}, "UNIQUE": {}, "UPDATE": {},
	"VALUES": {}, "VIEW": {}, "WHEN": {}, "WHERE": {}, "WITH": {},
}
