package sql

import (
	"strings"
	"unicode"
)

// TokenKind classifies a Token. Callers map kinds to what they need: the editor
// highlighter maps each kind to a style; the analysis layer keeps only the
// keyword/identifier/punctuation kinds and discards the rest.
type TokenKind int

const (
	KindWhitespace TokenKind = iota
	KindKeyword
	KindIdentifier
	KindQuotedIdentifier // "foo", `foo`, [foo]
	KindString           // 'foo'
	KindNumber
	KindComment     // -- line  and  /* block */
	KindParameter   // :name, @name, $name, $1, ?
	KindOperator    // * + - / % < > = ! |
	KindPunctuation // . , ; ( )
	KindPlain       // any rune matching no other rule
)

// Token is a single lexical token. Start and End are rune offsets into the
// lexed input; Text is that slice. Unterminated is set on a quote, bracket, or
// block comment that reached end-of-input without closing.
type Token struct {
	Kind         TokenKind
	Text         string
	Start        int
	End          int
	Unterminated bool
}

// Lex tokenises input into a contiguous, total slice of Tokens (every rune is
// covered by exactly one Token). It is stateless except for inBlockComment:
// pass the previous line's endInBlockComment to continue a block comment across
// lines. Whole-statement callers pass false and ignore the returned state.
func Lex(input string, inBlockComment bool) (tokens []Token, endInBlockComment bool) {
	runes := []rune(input)
	tokens = make([]Token, 0, len(runes)/2+1)

	i := 0
	for i < len(runes) {
		if inBlockComment {
			end, closed := indexOfBlockCommentEnd(runes, i)
			if !closed {
				tokens = append(tokens, tokenAt(runes, KindComment, i, len(runes), true))
				return tokens, true
			}
			tokens = append(tokens, tokenAt(runes, KindComment, i, end, false))
			inBlockComment = false
			i = end
			continue
		}

		switch {
		case isSpaceRune(runes[i]):
			start := i
			for i < len(runes) && isSpaceRune(runes[i]) {
				i++
			}
			tokens = append(tokens, tokenAt(runes, KindWhitespace, start, i, false))
		case startsWith(runes, i, '-', '-'):
			end := i + 2
			for end < len(runes) && runes[end] != '\n' {
				end++
			}
			tokens = append(tokens, tokenAt(runes, KindComment, i, end, false))
			i = end
		case startsWith(runes, i, '/', '*'):
			end, closed := indexOfBlockCommentEnd(runes, i)
			if !closed {
				tokens = append(tokens, tokenAt(runes, KindComment, i, len(runes), true))
				return tokens, true
			}
			tokens = append(tokens, tokenAt(runes, KindComment, i, end, false))
			i = end
		case runes[i] == '\'':
			end, closed := consumeQuoted(runes, i, '\'')
			tokens = append(tokens, tokenAt(runes, KindString, i, end, !closed))
			i = end
		case runes[i] == '"' || runes[i] == '`':
			end, closed := consumeQuoted(runes, i, runes[i])
			tokens = append(tokens, tokenAt(runes, KindQuotedIdentifier, i, end, !closed))
			i = end
		case runes[i] == '[':
			end, closed := consumeBracket(runes, i)
			tokens = append(tokens, tokenAt(runes, KindQuotedIdentifier, i, end, !closed))
			i = end
		case isNamedParameterStart(runes, i):
			end := consumeNamedParameter(runes, i)
			tokens = append(tokens, tokenAt(runes, KindParameter, i, end, false))
			i = end
		case runes[i] == '?':
			tokens = append(tokens, tokenAt(runes, KindParameter, i, i+1, false))
			i++
		case unicode.IsDigit(runes[i]):
			end := consumeNumber(runes, i)
			tokens = append(tokens, tokenAt(runes, KindNumber, i, end, false))
			i = end
		case IsIdentifierStart(runes[i]):
			end := ConsumeIdentifier(runes, i)
			kind := KindIdentifier
			if IsKeyword(string(runes[i:end])) {
				kind = KindKeyword
			}
			tokens = append(tokens, tokenAt(runes, kind, i, end, false))
			i = end
		case isPunctuationRune(runes[i]):
			tokens = append(tokens, tokenAt(runes, KindPunctuation, i, i+1, false))
			i++
		case isOperatorRune(runes[i]):
			tokens = append(tokens, tokenAt(runes, KindOperator, i, i+1, false))
			i++
		default:
			tokens = append(tokens, tokenAt(runes, KindPlain, i, i+1, false))
			i++
		}
	}

	return tokens, inBlockComment
}

func tokenAt(runes []rune, kind TokenKind, start, end int, unterminated bool) Token {
	return Token{Kind: kind, Text: string(runes[start:end]), Start: start, End: end, Unterminated: unterminated}
}

// IsIdentifierStart reports whether r can start a SQL identifier.
func IsIdentifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

// IsIdentifierPart reports whether r can appear inside a SQL identifier.
func IsIdentifierPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

// ConsumeIdentifier returns the end index after consuming a SQL identifier
// starting at start.
func ConsumeIdentifier(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && IsIdentifierPart(runes[i]) {
		i++
	}
	return i
}

// Unquote strips surrounding quote characters from a quoted SQL identifier
// ("foo", `foo`, [foo]) and unescapes doubled quotes within. Unquoted input is
// returned unchanged.
func Unquote(value string) string {
	if len(value) >= 2 {
		switch {
		case value[0] == '"' && value[len(value)-1] == '"':
			return strings.ReplaceAll(value[1:len(value)-1], `""`, `"`)
		case value[0] == '`' && value[len(value)-1] == '`':
			return strings.ReplaceAll(value[1:len(value)-1], "``", "`")
		case value[0] == '[' && value[len(value)-1] == ']':
			return value[1 : len(value)-1]
		}
	}
	return value
}

func indexOfBlockCommentEnd(runes []rune, start int) (int, bool) {
	for i := start + 2; i < len(runes); i++ {
		if runes[i-1] == '*' && runes[i] == '/' {
			return i + 1, true
		}
	}
	return len(runes), false
}

func consumeQuoted(runes []rune, start int, quote rune) (int, bool) {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] != quote {
			continue
		}
		if i+1 < len(runes) && runes[i+1] == quote {
			i++
			continue
		}
		return i + 1, true
	}
	return len(runes), false
}

func consumeBracket(runes []rune, start int) (int, bool) {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == ']' {
			return i + 1, true
		}
	}
	return len(runes), false
}

func isNamedParameterStart(runes []rune, index int) bool {
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

func consumeNamedParameter(runes []rune, start int) int {
	i := start + 1
	for i < len(runes) && (IsIdentifierPart(runes[i]) || unicode.IsDigit(runes[i])) {
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

func isOperatorRune(r rune) bool {
	return strings.ContainsRune("*+-/%<>=!|", r)
}

func isPunctuationRune(r rune) bool {
	return strings.ContainsRune(".,;()", r)
}

func isSpaceRune(r rune) bool {
	return unicode.IsSpace(r)
}

func startsWith(runes []rune, index int, a, b rune) bool {
	return index+1 < len(runes) && runes[index] == a && runes[index+1] == b
}
