package app

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
}

type renderedEditorLine struct {
	logicalLine int
	lineNumber  int
	runes       sqlStyledLine
	isCursor    bool
	cursorCol   int
}

func newSQLSyntaxHighlighter() sqlSyntaxHighlighter {
	return sqlSyntaxHighlighter{
		keywordStyle:          appTheme.keywordStyle,
		stringStyle:           appTheme.stringStyle,
		numberStyle:           appTheme.numberStyle,
		commentStyle:          appTheme.commentStyle,
		quotedIdentifierStyle: appTheme.quotedIdentifierStyle,
		parameterStyle:        appTheme.parameterStyle,
		operatorStyle:         appTheme.operatorStyle,
		promptStyle:           appTheme.promptStyle,
		lineNumberStyle:       appTheme.lineNumberStyle,
		cursorLineNumberStyle: appTheme.cursorLineNumberStyle,
		placeholderStyle:      appTheme.placeholderStyle,
		cursorLineStyle:       appTheme.cursorLineStyle,
		cursorStyle:           appTheme.cursorStyle,
	}
}

func splitEditorLines(value string) []string {
	if value == "" {
		return []string{""}
	}

	return strings.Split(value, "\n")
}

func wrapStyledLine(line sqlStyledLine, width int) []sqlStyledLine {
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
			end := indexOfBlockCommentEnd(runes, i)
			if end < 0 {
				styled = appendStyledRunes(styled, runes[i:], sqlTokenComment)
				return styled, state
			}

			styled = appendStyledRunes(styled, runes[i:end], sqlTokenComment)
			state.inBlockComment = false
			i = end
			continue
		}

		switch {
		case startsWithRunes(runes, i, '-', '-'):
			styled = appendStyledRunes(styled, runes[i:], sqlTokenComment)
			return styled, state
		case startsWithRunes(runes, i, '/', '*'):
			end := indexOfBlockCommentEnd(runes, i)
			if end < 0 {
				styled = appendStyledRunes(styled, runes[i:], sqlTokenComment)
				state.inBlockComment = true
				return styled, state
			}

			styled = appendStyledRunes(styled, runes[i:end], sqlTokenComment)
			i = end
		case runes[i] == '\'':
			end := consumeQuotedLiteral(runes, i, '\'')
			styled = appendStyledRunes(styled, runes[i:end], sqlTokenString)
			i = end
		case runes[i] == '"' || runes[i] == '`':
			end := consumeQuotedLiteral(runes, i, runes[i])
			styled = appendStyledRunes(styled, runes[i:end], sqlTokenQuotedIdentifier)
			i = end
		case runes[i] == '[':
			end := consumeBracketIdentifier(runes, i)
			styled = appendStyledRunes(styled, runes[i:end], sqlTokenQuotedIdentifier)
			i = end
		case isNamedParameterStart(runes, i):
			end := consumeNamedParameter(runes, i)
			styled = appendStyledRunes(styled, runes[i:end], sqlTokenParameter)
			i = end
		case runes[i] == '?':
			styled = appendStyledRunes(styled, runes[i:i+1], sqlTokenParameter)
			i++
		case unicode.IsDigit(runes[i]):
			end := consumeNumber(runes, i)
			styled = appendStyledRunes(styled, runes[i:end], sqlTokenNumber)
			i = end
		case isIdentifierStart(runes[i]):
			end := consumeIdentifier(runes, i)
			kind := sqlTokenPlain
			if _, ok := sqlKeywords[strings.ToUpper(string(runes[i:end]))]; ok {
				kind = sqlTokenKeyword
			}
			styled = appendStyledRunes(styled, runes[i:end], kind)
			i = end
		case isOperatorRune(runes[i]):
			styled = appendStyledRunes(styled, runes[i:i+1], sqlTokenOperator)
			i++
		default:
			styled = appendStyledRunes(styled, runes[i:i+1], sqlTokenPlain)
			i++
		}
	}

	return styled, state
}

func appendStyledRunes(line sqlStyledLine, runes []rune, kind sqlTokenKind) sqlStyledLine {
	for _, r := range runes {
		line = append(line, sqlStyledRune{rune: r, kind: kind})
	}

	return line
}

func startsWithRunes(runes []rune, index int, prefix ...rune) bool {
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

func indexOfBlockCommentEnd(runes []rune, start int) int {
	for i := start + 2; i < len(runes); i++ {
		if runes[i-1] == '*' && runes[i] == '/' {
			return i + 1
		}
	}

	return -1
}

func consumeQuotedLiteral(runes []rune, start int, quote rune) int {
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

func consumeBracketIdentifier(runes []rune, start int) int {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == ']' {
			return i + 1
		}
	}

	return len(runes)
}

func isNamedParameterStart(runes []rune, index int) bool {
	if index >= len(runes) {
		return false
	}

	switch runes[index] {
	case ':', '@':
		return index+1 < len(runes) && isIdentifierPart(runes[index+1])
	case '$':
		return index+1 < len(runes) && (isIdentifierPart(runes[index+1]) || unicode.IsDigit(runes[index+1]))
	default:
		return false
	}
}

func consumeNamedParameter(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && (isIdentifierPart(runes[i]) || unicode.IsDigit(runes[i])) {
		i++
	}

	return i
}

func consumeNumber(runes []rune, start int) int {
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

func isIdentifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentifierPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

func consumeIdentifier(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && isIdentifierPart(runes[i]) {
		i++
	}

	return i
}

func isOperatorRune(r rune) bool {
	return strings.ContainsRune("*+-/%<>=!|.,;()", r)
}

var sqlKeywords = map[string]struct{}{
	"ALL": {}, "AND": {}, "AS": {}, "ASC": {}, "BETWEEN": {}, "BY": {}, "CASE": {}, "CREATE": {},
	"CROSS": {}, "DELETE": {}, "DESC": {}, "DISTINCT": {}, "DROP": {}, "ELSE": {}, "END": {},
	"EXISTS": {}, "FALSE": {}, "FROM": {}, "FULL": {}, "GROUP": {}, "HAVING": {}, "IN": {},
	"INNER": {}, "INSERT": {}, "INTO": {}, "IS": {}, "JOIN": {}, "LEFT": {}, "LIKE": {},
	"LIMIT": {}, "NOT": {}, "NULL": {}, "OFFSET": {}, "ON": {}, "OR": {}, "ORDER": {},
	"OUTER": {}, "PRIMARY": {}, "REPLACE": {}, "RETURNING": {}, "RIGHT": {}, "SELECT": {},
	"SET": {}, "TABLE": {}, "THEN": {}, "TRUE": {}, "UNION": {}, "UNIQUE": {}, "UPDATE": {},
	"VALUES": {}, "VIEW": {}, "WHEN": {}, "WHERE": {}, "WITH": {},
}
