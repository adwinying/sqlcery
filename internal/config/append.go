package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isBareKey reports whether name is safe to use as a bare TOML table key.
// A bare key may only contain ASCII letters, digits, underscores, and hyphens.
func isBareKey(name string) bool {
	if len(name) == 0 {
		return false
	}
	for _, r := range name {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// tomlEscapeValue escapes s for use inside a TOML double-quoted string.
// Escaped characters: \ → \\, " → \", newline → \n, tab → \t,
// carriage return → \r, and other control characters < 0x20 as \uXXXX.
func tomlEscapeValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// tomlTableKey returns the TOML table key fragment for name.
// Bare-safe names are returned unquoted; others are double-quoted and escaped.
func tomlTableKey(name string) string {
	if isBareKey(name) {
		return name
	}
	return `"` + tomlEscapeValue(name) + `"`
}

// AppendConnection appends a [connection.<name>] TOML block to the file at path.
// The parent directory is created with mode 0755 if absent.
// Existing file content is preserved byte-for-byte; the new block lands at the end.
// String values are TOML-escaped; the port field is rendered bare and omitted when zero.
// Lifecycle and Color fields are not emitted.
func AppendConnection(path string, name string, conn Connection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	existing := ""
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err == nil {
		existing = string(data)
	}

	var sb strings.Builder
	sb.WriteString(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		sb.WriteByte('\n')
	}
	// Separate the new entry from existing records with a blank line.
	if strings.TrimSpace(existing) != "" {
		sb.WriteByte('\n')
	}

	fmt.Fprintf(&sb, "[connection.%s]\n", tomlTableKey(name))
	fmt.Fprintf(&sb, "type = \"%s\"\n", tomlEscapeValue(conn.Type))
	if conn.SSHHost != "" {
		fmt.Fprintf(&sb, "ssh_host = \"%s\"\n", tomlEscapeValue(conn.SSHHost))
	}
	if conn.Host != "" {
		fmt.Fprintf(&sb, "host = \"%s\"\n", tomlEscapeValue(conn.Host))
	}
	if conn.Port > 0 {
		fmt.Fprintf(&sb, "port = %d\n", conn.Port)
	}
	if conn.Database != "" {
		fmt.Fprintf(&sb, "database = \"%s\"\n", tomlEscapeValue(conn.Database))
	}
	if conn.Username != "" {
		fmt.Fprintf(&sb, "username = \"%s\"\n", tomlEscapeValue(conn.Username))
	}
	if conn.Password != "" {
		fmt.Fprintf(&sb, "password = \"%s\"\n", tomlEscapeValue(conn.Password))
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
