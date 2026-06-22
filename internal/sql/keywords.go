// Package sql is the lightweight-tokenization SQL Analysis Strategy: a single
// SQL tokenizer plus the rune-classification and keyword primitives it is built
// from. It is a leaf package — it imports nothing from internal/app, internal/tui,
// or internal/db — so both the editor highlighter (internal/tui) and the
// cursor/scope/statement analyses (internal/app) tokenize SQL the same way.
package sql

import "strings"

// Keywords is the ordered set of SQL keywords the tokenizer recognises. The
// order is significant: internal/app offers them as autocomplete suggestions in
// this order.
var Keywords = []string{
	"ALL", "AND", "AS", "ASC", "BETWEEN", "BY", "CASE", "CREATE",
	"CROSS", "DELETE", "DESC", "DISTINCT", "DROP", "ELSE", "END",
	"EXISTS", "FALSE", "FROM", "FULL", "GROUP", "HAVING", "IN",
	"INNER", "INSERT", "INTO", "IS", "JOIN", "LEFT", "LIKE",
	"LIMIT", "NOT", "NULL", "OFFSET", "ON", "OR", "ORDER",
	"OUTER", "PRIMARY", "REPLACE", "RETURNING", "RIGHT", "SELECT",
	"SET", "TABLE", "THEN", "TRUE", "UNION", "UNIQUE", "UPDATE",
	"VALUES", "VIEW", "WHEN", "WHERE", "WITH",
}

var keywordSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(Keywords))
	for _, keyword := range Keywords {
		set[keyword] = struct{}{}
	}
	return set
}()

// IsKeyword reports whether word (case-insensitively) is a SQL keyword.
func IsKeyword(word string) bool {
	_, ok := keywordSet[strings.ToUpper(word)]
	return ok
}
