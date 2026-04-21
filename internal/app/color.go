package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// namedColors maps common color names to ANSI 256-color codes.
// Standard colors use the base 16 ANSI palette values.
var namedColors = map[string]string{
	// Standard colors
	"black":   "0",
	"red":     "1",
	"green":   "2",
	"yellow":  "3",
	"blue":    "4",
	"magenta": "5",
	"cyan":    "6",
	"white":   "7",

	// Bright variants
	"bright-black":   "8",
	"bright-red":     "9",
	"bright-green":   "10",
	"bright-yellow":  "11",
	"bright-blue":    "12",
	"bright-magenta": "13",
	"bright-cyan":    "14",
	"bright-white":   "15",

	// Aliases
	"gray":        "8",
	"grey":        "8",
	"bright-gray": "15",
	"bright-grey": "15",
	"orange":      "214",
	"pink":        "213",
	"purple":      "129",
	"violet":      "99",
	"lime":        "118",
	"teal":        "37",
	"brown":       "130",
}

// resolveColor converts a color string to a lipgloss.Color.
// It accepts:
//   - Named colors: "red", "bright-blue", etc.
//   - ANSI color numbers: "1", "196", etc.
//   - Hex color codes: "#FF0000", etc.
func resolveColor(color string) lipgloss.Color {
	normalized := strings.ToLower(strings.TrimSpace(color))
	if ansi, ok := namedColors[normalized]; ok {
		return lipgloss.Color(ansi)
	}
	return lipgloss.Color(color)
}
