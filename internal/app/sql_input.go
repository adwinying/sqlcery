package app

import "github.com/adwinying/sqlcery/internal/sql"

// isCompleteSQLStatement reports whether input is a submittable SQL statement:
// it carries meaningful content and the last meaningful token is a semicolon,
// with no unterminated quote/identifier or block comment. Comments and
// whitespace are not meaningful content. (ADR-0001.)
func isCompleteSQLStatement(input string) bool {
	tokens, inBlockComment := sql.Lex(input, false)
	if inBlockComment {
		return false
	}

	hasContent := false
	lastWasSemicolon := false
	for _, token := range tokens {
		switch token.Kind {
		case sql.KindWhitespace, sql.KindComment:
			continue
		case sql.KindString, sql.KindQuotedIdentifier:
			if token.Unterminated {
				return false
			}
			hasContent = true
			lastWasSemicolon = false
		case sql.KindPunctuation:
			if token.Text == ";" {
				lastWasSemicolon = true
				continue
			}
			hasContent = true
			lastWasSemicolon = false
		default:
			hasContent = true
			lastWasSemicolon = false
		}
	}

	return hasContent && lastWasSemicolon
}
