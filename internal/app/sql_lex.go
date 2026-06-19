package app

import (
	"strings"
	"unicode"

	"github.com/adwinying/sqlcery/internal/tui"
)

// sqlToken is a single lexical token from a SQL input string.
// SQL keywords are emitted with both Keyword and Ident set to true.
// Quoted identifiers ("foo", `foo`, [foo]) are unquoted before storage.
// String literals ('foo') are consumed but not emitted as tokens.
type sqlToken struct {
	Text    string
	Keyword bool
	Ident   bool
	Symbol  bool
}

// sqlLex tokenises a SQL string into a flat slice of sqlTokens.
//
// Behaviour:
//   - String literals ('...') are consumed and discarded.
//   - Quoted identifiers ("...", `...`, [...]) are unquoted and emitted as
//     Ident tokens.
//   - Line comments (-- ...) and block comments (/* ... */) are discarded.
//   - Semicolons and punctuation (.,()) are emitted as Symbol tokens.
//   - Returns early on an unclosed quoted literal or block comment.
func sqlLex(input string) []sqlToken {
	runes := []rune(input)
	tokens := make([]sqlToken, 0, len(runes)/4)

	for i := 0; i < len(runes); {
		switch {
		case unicode.IsSpace(runes[i]):
			i++
		case hasSQLRunePrefix(runes, i, '-', '-'):
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case hasSQLRunePrefix(runes, i, '/', '*'):
			next, closed := consumeSQLBlockComment(runes, i)
			if !closed {
				return tokens
			}
			i = next
		case runes[i] == '\'':
			next, closed := consumeSQLQuotedRunes(runes, i, '\'')
			if !closed {
				return tokens
			}
			i = next
		case runes[i] == '"' || runes[i] == '`':
			next, closed := consumeSQLQuotedRunes(runes, i, runes[i])
			if !closed {
				return tokens
			}
			tokens = append(tokens, sqlToken{Text: unquoteSQLIdentifier(string(runes[i:next])), Ident: true})
			i = next
		case runes[i] == '[':
			next, closed := consumeSQLBracketIdentifier(runes, i)
			if !closed {
				return tokens
			}
			tokens = append(tokens, sqlToken{Text: unquoteSQLIdentifier(string(runes[i:next])), Ident: true})
			i = next
		case tui.IsIdentifierStart(runes[i]):
			end := tui.ConsumeIdentifier(runes, i)
			text := string(runes[i:end])
			_, keyword := autocompleteSQLKeywords[strings.ToUpper(text)]
			tokens = append(tokens, sqlToken{Text: text, Keyword: keyword, Ident: true})
			i = end
		case strings.ContainsRune(".,();", runes[i]):
			tokens = append(tokens, sqlToken{Text: string(runes[i]), Symbol: true})
			i++
		default:
			i++
		}
	}

	return tokens
}

// unquoteSQLIdentifier strips surrounding quote characters from a quoted SQL
// identifier and unescapes any doubled quote characters within.
func unquoteSQLIdentifier(value string) string {
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
