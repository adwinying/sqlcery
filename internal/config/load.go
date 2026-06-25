package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

const (
	DirName             = "sqlcery"
	FileName            = "sqlcery.toml"
	ConnectionsFileName = "connections.toml"
)

type Paths struct {
	Global string
	Local  string
}

type Result[T any] struct {
	Value  T
	Paths  Paths
	Loaded []string
}

type validator interface {
	Validate() error
}

func Load[T any](cwd string) (Result[T], error) {
	return load[T](cwd, FileName)
}

func DiscoverConnectionPaths(cwd string) (Paths, error) {
	return discoverPaths(os.UserConfigDir, os.Getwd, cwd, ConnectionsFileName)
}

func LoadConnections[T any](cwd string) (Result[T], error) {
	return load[T](cwd, ConnectionsFileName)
}

func load[T any](cwd string, fileName string) (Result[T], error) {
	paths, err := discoverPaths(os.UserConfigDir, os.Getwd, cwd, fileName)
	if err != nil {
		return Result[T]{}, err
	}

	result := Result[T]{Paths: paths}
	for _, path := range []string{paths.Global, paths.Local} {
		exists, err := fileExists(path)
		if err != nil {
			return Result[T]{}, fmt.Errorf("stat %s: %w", path, err)
		}
		if !exists {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return Result[T]{}, fmt.Errorf("read %s: %w", path, err)
		}

		if _, err := toml.Decode(string(data), &result.Value); err != nil {
			return Result[T]{}, &InvalidConfigError{Op: "decode", Path: path, Err: err}
		}

		result.Loaded = append(result.Loaded, path)
	}

	if err := validateValue(result.Value); err != nil {
		return result, &InvalidConfigError{Op: "validate", Path: validationSource(result.Loaded, fileName), Err: err}
	}

	result.Value = normalizeValue(result.Value)

	return result, nil
}

func validateValue[T any](value T) error {
	if validated, ok := any(value).(validator); ok {
		return validated.Validate()
	}

	if validated, ok := any(&value).(validator); ok {
		return validated.Validate()
	}

	return nil
}

func normalizeValue[T any](value T) T {
	if normalized, ok := any(value).(interface{ Normalized() T }); ok {
		return normalized.Normalized()
	}

	return value
}

func validationSource(loaded []string, fileName string) string {
	if len(loaded) == 0 {
		return fileName
	}

	return loaded[len(loaded)-1]
}

func discoverPaths(userConfigDir func() (string, error), getwd func() (string, error), cwd string, fileName string) (Paths, error) {
	configDir, err := resolveConfigHome(userConfigDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config dir: %w", err)
	}

	if cwd == "" {
		cwd, err = getwd()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}

	return Paths{
		Global: filepath.Join(configDir, DirName, fileName),
		Local:  filepath.Join(filepath.Clean(cwd), fileName),
	}, nil
}

func resolveConfigHome(userConfigDir func() (string, error)) (string, error) {
	if dir, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
		}
		return dir, nil
	}

	if runtime.GOOS == "darwin" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".config"), nil
	}

	return userConfigDir()
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
