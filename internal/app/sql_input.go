package app

func isCompleteSQLStatement(input string) bool {
	runes := []rune(input)
	hasStatementContent := false
	lastMeaningfulWasSemicolon := false

	for i := 0; i < len(runes); {
		switch {
		case isSQLSpaceRune(runes[i]):
			i++
		case hasSQLRunePrefix(runes, i, '-', '-'):
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case hasSQLRunePrefix(runes, i, '/', '*'):
			next, closed := consumeSQLBlockComment(runes, i)
			if !closed {
				return false
			}
			i = next
		case runes[i] == '\'' || runes[i] == '"' || runes[i] == '`':
			next, closed := consumeSQLQuotedRunes(runes, i, runes[i])
			if !closed {
				return false
			}
			hasStatementContent = true
			lastMeaningfulWasSemicolon = false
			i = next
		case runes[i] == '[':
			next, closed := consumeSQLBracketIdentifier(runes, i)
			if !closed {
				return false
			}
			hasStatementContent = true
			lastMeaningfulWasSemicolon = false
			i = next
		case runes[i] == ';':
			lastMeaningfulWasSemicolon = true
			i++
		default:
			hasStatementContent = true
			lastMeaningfulWasSemicolon = false
			i++
		}
	}

	return hasStatementContent && lastMeaningfulWasSemicolon
}

func consumeSQLBlockComment(runes []rune, start int) (int, bool) {
	for i := start + 2; i < len(runes); i++ {
		if hasSQLRunePrefix(runes, i, '*', '/') {
			return i + 2, true
		}
	}

	return len(runes), false
}

func consumeSQLQuotedRunes(runes []rune, start int, quote rune) (int, bool) {
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

func consumeSQLBracketIdentifier(runes []rune, start int) (int, bool) {
	for i := start + 1; i < len(runes); i++ {
		if runes[i] == ']' {
			return i + 1, true
		}
	}

	return len(runes), false
}
