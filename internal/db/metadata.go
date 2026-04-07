package db

import "strings"

func cloneTypes(types []TypeInfo) []TypeInfo {
	return append([]TypeInfo(nil), types...)
}

func normalizeTableType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "BASE TABLE":
		return "table"
	case "VIEW":
		return "view"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
