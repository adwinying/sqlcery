package app

import "github.com/adwinying/sqlcery/internal/sql"

// sqlToken is a single lexical token consumed by the SQL analysis layer
// (scope detection and table references). SQL keywords carry both Keyword and
// Ident set; quoted identifiers are unquoted before storage.
type sqlToken struct {
	Text    string
	Keyword bool
	Ident   bool
	Symbol  bool
}

// sqlLex tokenises a SQL string for analysis by filtering the lightweight
// tokenizer (internal/sql) down to the tokens the analyses care about:
// keywords, identifiers (quoted ones unquoted), and the punctuation . , ( ) ;.
// Strings, comments, numbers, parameters, operators, and whitespace are
// discarded — exactly the tokens analyzeAutocompleteScope and referencedTables
// ignore.
func sqlLex(input string) []sqlToken {
	tokens, _ := sql.Lex(input, false)
	out := make([]sqlToken, 0, len(tokens))
	for _, token := range tokens {
		switch token.Kind {
		case sql.KindKeyword:
			out = append(out, sqlToken{Text: token.Text, Keyword: true, Ident: true})
		case sql.KindIdentifier:
			out = append(out, sqlToken{Text: token.Text, Ident: true})
		case sql.KindQuotedIdentifier:
			out = append(out, sqlToken{Text: sql.Unquote(token.Text), Ident: true})
		case sql.KindPunctuation:
			out = append(out, sqlToken{Text: token.Text, Symbol: true})
		}
	}
	return out
}

// scanDottedIdentifier reads a run of identifier tokens separated by "."
// punctuation starting at start, returning the identifier parts and the index
// of the first token after the run. It is the shared mechanic behind both
// table-reference analyses (autocomplete's referencedTables and the Statement
// Expander's source-table inference); each applies its own arity policy to the
// parts.
func scanDottedIdentifier(tokens []sqlToken, start int) (parts []string, next int) {
	i := start
	for i < len(tokens) {
		if !tokens[i].Ident {
			break
		}
		parts = append(parts, tokens[i].Text)
		i++
		if i >= len(tokens) || !tokens[i].Symbol || tokens[i].Text != "." {
			break
		}
		i++
	}
	return parts, i
}
