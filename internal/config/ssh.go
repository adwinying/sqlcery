package config

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type ResolvedSSHHost struct {
	Alias                 string
	Host                  string
	Port                  int
	User                  string
	IdentityFiles         []string
	KnownHostsFiles       []string
	StrictHostKeyChecking string
}

func ResolveSSHHost(host string) (ResolvedSSHHost, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ResolvedSSHHost{}, fmt.Errorf("resolve ssh home directory: %w", err)
	}

	username, err := currentUsername()
	if err != nil {
		return ResolvedSSHHost{}, fmt.Errorf("resolve ssh username: %w", err)
	}

	configPath := filepath.Join(homeDir, ".ssh", "config")
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return ResolvedSSHHost{}, fmt.Errorf("read ssh config %s: %w", configPath, err)
	}

	return resolveSSHHost(host, homeDir, username, data)
}

func resolveSSHHost(host string, homeDir string, username string, data []byte) (ResolvedSSHHost, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return ResolvedSSHHost{}, fmt.Errorf("ssh host is required")
	}

	resolved := ResolvedSSHHost{
		Alias: host,
		Host:  host,
	}

	if len(data) > 0 {
		if err := applySSHConfig(&resolved, data); err != nil {
			return ResolvedSSHHost{}, err
		}
	}

	if resolved.Host == "" {
		resolved.Host = host
	}

	if resolved.Port == 0 {
		resolved.Port = 22
	}
	if resolved.User == "" {
		resolved.User = username
	}

	resolved.IdentityFiles = expandSSHPaths(resolved.IdentityFiles, homeDir, resolved)
	resolved.KnownHostsFiles = expandSSHPaths(resolved.KnownHostsFiles, homeDir, resolved)
	if len(resolved.KnownHostsFiles) == 0 && homeDir != "" {
		resolved.KnownHostsFiles = []string{filepath.Join(homeDir, ".ssh", "known_hosts")}
	}

	return resolved, nil
}

func applySSHConfig(resolved *ResolvedSSHHost, data []byte) error {
	if resolved == nil {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	patterns := []string{"*"}
	matchPriority := 1
	hostPriority := 0
	userPriority := 0
	portPriority := 0
	strictPriority := 0
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		key, value, ok := parseSSHConfigLine(scanner.Text())
		if !ok {
			continue
		}

		if strings.EqualFold(key, "Host") {
			patterns = splitSSHPatternList(value)
			matchPriority = sshHostMatchPriority(resolved.Alias, patterns)
			continue
		}

		if matchPriority == 0 {
			continue
		}

		switch strings.ToLower(key) {
		case "hostname":
			if matchPriority > hostPriority {
				resolved.Host = strings.TrimSpace(value)
				hostPriority = matchPriority
			}
		case "user":
			if matchPriority > userPriority {
				resolved.User = strings.TrimSpace(value)
				userPriority = matchPriority
			}
		case "port":
			if matchPriority > portPriority {
				port, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					return fmt.Errorf("parse ssh config line %d: invalid port %q", lineNumber, value)
				}
				resolved.Port = port
				portPriority = matchPriority
			}
		case "identityfile":
			resolved.IdentityFiles = appendSSHConfigValues(resolved.IdentityFiles, splitSSHValueList(value)...)
		case "userknownhostsfile":
			resolved.KnownHostsFiles = appendSSHConfigValues(resolved.KnownHostsFiles, splitSSHValueList(value)...)
		case "stricthostkeychecking":
			if matchPriority > strictPriority {
				resolved.StrictHostKeyChecking = strings.ToLower(strings.TrimSpace(value))
				strictPriority = matchPriority
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan ssh config: %w", err)
	}

	return nil
}

func parseSSHConfigLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	separator := strings.IndexAny(line, " \t=")
	if separator < 0 {
		return strings.TrimSpace(line), "", true
	}

	key := strings.TrimSpace(line[:separator])
	value := strings.TrimSpace(line[separator+1:])
	value = strings.TrimPrefix(value, "=")
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if key == "" {
		return "", "", false
	}

	return key, value, true
}

func splitSSHPatternList(value string) []string {
	values := splitSSHValueList(value)
	if len(values) == 0 {
		return []string{"*"}
	}

	return values
}

func splitSSHValueList(value string) []string {
	fields := strings.Fields(value)
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, `"'`)
		if field == "" {
			continue
		}
		values = append(values, field)
	}

	return values
}

func sshHostMatchPriority(host string, patterns []string) int {
	host = strings.ToLower(strings.TrimSpace(host))
	matched := 0
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.ToLower(strings.TrimPrefix(pattern, "!"))
		ok, err := path.Match(pattern, host)
		if err != nil {
			ok = pattern == host
		}

		if negated && ok {
			return 0
		}

		if ok {
			if pattern == "*" {
				if matched < 1 {
					matched = 1
				}
				continue
			}

			matched = 2
		}
	}

	return matched
}

func appendSSHConfigValues(existing []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		alreadyAdded := false
		for _, current := range existing {
			if current == value {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			existing = append(existing, value)
		}
	}

	return existing
}

func expandSSHPaths(paths []string, homeDir string, resolved ResolvedSSHHost) []string {
	expanded := make([]string, 0, len(paths))
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		value = strings.ReplaceAll(value, "~", homeDir)
		value = strings.ReplaceAll(value, "%h", resolved.Host)
		value = strings.ReplaceAll(value, "%n", resolved.Alias)
		value = strings.ReplaceAll(value, "%p", strconv.Itoa(resolved.Port))
		value = strings.ReplaceAll(value, "%r", resolved.User)
		value = filepath.Clean(value)
		expanded = append(expanded, value)
	}

	return expanded
}

func currentUsername() (string, error) {
	if username := strings.TrimSpace(os.Getenv("USER")); username != "" {
		return username, nil
	}

	current, err := user.Current()
	if err != nil {
		return "", err
	}

	return current.Username, nil
}
