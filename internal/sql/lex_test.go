package sql

import "testing"

func kinds(tokens []Token) []TokenKind {
	out := make([]TokenKind, len(tokens))
	for i, token := range tokens {
		out[i] = token.Kind
	}
	return out
}

func TestLexClassifiesTokens(t *testing.T) {
	tokens, inComment := Lex(`SELECT "id", 42, 'x' FROM t WHERE a = :p;`, false)
	if inComment {
		t.Fatal("inComment = true, want false")
	}

	// Spot-check the non-whitespace tokens in order.
	want := []struct {
		text string
		kind TokenKind
	}{
		{"SELECT", KindKeyword},
		{`"id"`, KindQuotedIdentifier},
		{",", KindPunctuation},
		{"42", KindNumber},
		{",", KindPunctuation},
		{"'x'", KindString},
		{"FROM", KindKeyword},
		{"t", KindIdentifier},
		{"WHERE", KindKeyword},
		{"a", KindIdentifier},
		{"=", KindOperator},
		{":p", KindParameter},
		{";", KindPunctuation},
	}

	got := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != KindWhitespace {
			got = append(got, token)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d non-whitespace tokens, want %d: %#v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Text != w.text || got[i].Kind != w.kind {
			t.Fatalf("token %d = (%q, %d), want (%q, %d)", i, got[i].Text, got[i].Kind, w.text, w.kind)
		}
	}
}

func TestLexTotalCoverage(t *testing.T) {
	input := "a , b"
	tokens, _ := Lex(input, false)
	end := 0
	for _, token := range tokens {
		if token.Start != end {
			t.Fatalf("token %q starts at %d, want %d (gap or overlap)", token.Text, token.Start, end)
		}
		end = token.End
	}
	if end != len([]rune(input)) {
		t.Fatalf("tokens cover %d runes, want %d", end, len([]rune(input)))
	}
}

// TestLexLineCommentStopsAtNewline is a regression guard: a line comment must
// end at the newline, not swallow the rest of a multi-line statement.
func TestLexLineCommentStopsAtNewline(t *testing.T) {
	tokens, _ := Lex("-- c\nSELECT 1;", false)
	sawSelect := false
	for _, token := range tokens {
		if token.Kind == KindKeyword && token.Text == "SELECT" {
			sawSelect = true
		}
	}
	if !sawSelect {
		t.Fatalf("SELECT after the line comment was not tokenised: %#v", tokens)
	}
}

func TestLexBlockCommentStateCarriesAcrossLines(t *testing.T) {
	line1, inComment := Lex("SELECT /* open", false)
	if !inComment {
		t.Fatal("after line 1, inComment = false, want true")
	}
	if last := line1[len(line1)-1]; last.Kind != KindComment || !last.Unterminated {
		t.Fatalf("line 1 last token = (%d, unterminated=%v), want comment+unterminated", last.Kind, last.Unterminated)
	}

	line2, stillOpen := Lex("still */ 1", inComment)
	if stillOpen {
		t.Fatal("after line 2, inComment = true, want false")
	}
	if line2[0].Kind != KindComment {
		t.Fatalf("line 2 first token kind = %d, want comment", line2[0].Kind)
	}
}

func TestLexFlagsUnterminated(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"string", "'open"},
		{"bracket", "[open"},
		{"block comment", "/* open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _ := Lex(tt.input, false)
			if !tokens[len(tokens)-1].Unterminated {
				t.Fatalf("last token not flagged unterminated: %#v", tokens)
			}
		})
	}
}

func TestUnquote(t *testing.T) {
	tests := map[string]string{
		`"a""b"`: `a"b`,
		"`a`":    "a",
		"[a]":    "a",
		"bare":   "bare",
	}
	for in, want := range tests {
		if got := Unquote(in); got != want {
			t.Fatalf("Unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsKeyword(t *testing.T) {
	if !IsKeyword("select") || !IsKeyword("SELECT") {
		t.Fatal("SELECT should be a keyword (case-insensitive)")
	}
	if IsKeyword("widgets") {
		t.Fatal("widgets should not be a keyword")
	}
}
