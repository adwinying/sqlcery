package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

type ResolvedConnection struct {
	Name       string
	Raw        string
	Connection Connection
}

func ResolveCLIConnection(cwd string, args []string) (ResolvedConnection, error) {
	target, err := resolveConnectionTarget(cwd, args)
	if err != nil {
		return ResolvedConnection{}, err
	}

	if target == "" {
		return ResolvedConnection{}, nil
	}

	if resolved, ok, err := ParseConnectionString(target); ok {
		return resolved, err
	}

	connections, err := LoadConnections[Connections](cwd)
	if err != nil {
		return ResolvedConnection{}, err
	}

	return ResolveConnectionReference(connections.Value, target)
}

func ResolveConnectionReference(connections Connections, raw string) (ResolvedConnection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ResolvedConnection{}, fmt.Errorf("connection name or connection string is required")
	}

	if resolved, ok, err := ParseConnectionString(raw); ok {
		return resolved, err
	}

	connection, ok := connections.Connection[raw]
	if !ok {
		return ResolvedConnection{}, &UnknownConnectionError{Name: raw}
	}

	return ResolvedConnection{
		Name:       raw,
		Raw:        raw,
		Connection: connection,
	}, nil
}

func ParseConnectionString(raw string) (ResolvedConnection, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ResolvedConnection{}, false, nil
	}

	if !looksLikeConnectionString(raw) {
		return ResolvedConnection{}, false, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ResolvedConnection{}, true, fmt.Errorf("invalid connection string: %w", err)
	}

	connection, err := connectionFromURL(parsed)
	if err != nil {
		return ResolvedConnection{}, true, fmt.Errorf("invalid connection string: %w", err)
	}

	if err := connection.Validate(); err != nil {
		return ResolvedConnection{}, true, fmt.Errorf("invalid connection string: %w", err)
	}

	return ResolvedConnection{
		Raw:        raw,
		Connection: connection,
	}, true, nil
}

func resolveConnectionTarget(cwd string, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("expected at most 1 argument, got %d", len(args))
	}

	if len(args) == 1 {
		return strings.TrimSpace(args[0]), nil
	}

	// Zero args: no auto-connect target — the Model will show the Connection Picker.
	return "", nil
}

func looksLikeConnectionString(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(raw, "://") || strings.HasPrefix(raw, "sqlite:")
}

func connectionFromURL(parsed *url.URL) (Connection, error) {
	switch strings.ToLower(parsed.Scheme) {
	case "postgres", "postgresql":
		return networkConnectionFromURL("postgres", parsed, 5432)
	case "mysql":
		return networkConnectionFromURL("mysql", parsed, 3306)
	case "sqlite":
		database := sqliteDatabaseFromURL(parsed)
		return Connection{
			Type:     "sqlite",
			Database: database,
		}, nil
	default:
		return Connection{}, fmt.Errorf("unsupported connection string scheme %q", parsed.Scheme)
	}
}

func networkConnectionFromURL(kind string, parsed *url.URL, defaultPort int) (Connection, error) {
	port := defaultPort
	if rawPort := parsed.Port(); rawPort != "" {
		value, err := strconv.Atoi(rawPort)
		if err != nil {
			return Connection{}, fmt.Errorf("invalid connection string: parse port %q: %w", rawPort, err)
		}
		port = value
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	return Connection{
		Type:     kind,
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		Username: username,
		Password: password,
	}, nil
}

func sqliteDatabaseFromURL(parsed *url.URL) string {
	if parsed.Opaque != "" {
		return parsed.Opaque
	}

	database := parsed.Path
	if parsed.Host != "" {
		database = filepath.Join(parsed.Host, strings.TrimPrefix(parsed.Path, "/"))
	}

	if database == "/:memory:" {
		return ":memory:"
	}

	return database
}
