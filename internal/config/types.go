package config

import (
	"encoding"
	"fmt"
	"sort"
	"strings"
	"time"
)

var _ encoding.TextUnmarshaler = (*Duration)(nil)

type Config struct {
	Connection string `toml:"connection"`
}

func (c Config) Validate() error {
	if c.Connection == "" {
		return nil
	}

	if strings.TrimSpace(c.Connection) == "" {
		return fmt.Errorf("connection must not be blank")
	}

	return nil
}

type Connections struct {
	Connection map[string]Connection `toml:"connection"`
}

func (c Connections) Validate() error {
	if len(c.Connection) == 0 {
		return nil
	}

	names := make([]string, 0, len(c.Connection))
	for name := range c.Connection {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("connection name must not be blank")
		}

		if err := c.Connection[name].Validate(); err != nil {
			return fmt.Errorf("connection %q: %w", name, err)
		}
	}

	return nil
}

type Connection struct {
	Type      string                     `toml:"type"`
	SQLite    SQLiteConnectionOptions    `toml:"sqlite"`
	Postgres  PostgresConnectionOptions  `toml:"postgres"`
	MySQL     MySQLConnectionOptions     `toml:"mysql"`
	Lifecycle ConnectionLifecycleOptions `toml:"lifecycle"`
	SSHHost   string                     `toml:"ssh_host"`
}

func (c Connection) Validate() error {
	if c.SSHHost != "" && strings.TrimSpace(c.SSHHost) == "" {
		return fmt.Errorf("ssh_host must not be blank")
	}

	if err := c.Lifecycle.validateFor(c.Type); err != nil {
		return fmt.Errorf("lifecycle: %w", err)
	}

	switch c.Type {
	case "sqlite":
		if strings.TrimSpace(c.SSHHost) != "" {
			return fmt.Errorf("ssh_host is only supported for postgres and mysql")
		}

		if err := c.SQLite.Validate(); err != nil {
			return fmt.Errorf("sqlite: %w", err)
		}
	case "postgres", "mysql":
		var err error
		if c.Type == "postgres" {
			err = c.Postgres.Validate()
		} else {
			err = c.MySQL.Validate()
		}
		if err != nil {
			return fmt.Errorf("%s: %w", c.Type, err)
		}
	case "":
		return fmt.Errorf("type is required")
	default:
		return fmt.Errorf("type must be one of sqlite, postgres, mysql")
	}

	return nil
}

type ConnectionLifecycleOptions struct {
	ConnectTimeout     Duration `toml:"connect_timeout"`
	HealthCheckTimeout Duration `toml:"health_check_timeout"`
	MaxOpenConns       int      `toml:"max_open_conns"`
	MaxIdleConns       int      `toml:"max_idle_conns"`
	ConnMaxLifetime    Duration `toml:"conn_max_lifetime"`
	ConnMaxIdleTime    Duration `toml:"conn_max_idle_time"`
}

func (o ConnectionLifecycleOptions) validateFor(connectionType string) error {
	if err := o.Validate(); err != nil {
		return err
	}

	if connectionType == "sqlite" {
		if o.MaxOpenConns > 1 {
			return fmt.Errorf("max_open_conns must be 1 or lower for sqlite")
		}

		if o.MaxIdleConns > 1 {
			return fmt.Errorf("max_idle_conns must be 1 or lower for sqlite")
		}
	}

	return nil
}

type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return d.Duration().String()
}

func (d *Duration) UnmarshalText(text []byte) error {
	value, err := time.ParseDuration(strings.TrimSpace(string(text)))
	if err != nil {
		return err
	}

	*d = Duration(value)
	return nil
}

func (o ConnectionLifecycleOptions) Validate() error {
	if o.ConnectTimeout < 0 {
		return fmt.Errorf("connect_timeout must not be negative")
	}

	if o.HealthCheckTimeout < 0 {
		return fmt.Errorf("health_check_timeout must not be negative")
	}

	if o.MaxOpenConns < 0 {
		return fmt.Errorf("max_open_conns must not be negative")
	}

	if o.MaxIdleConns < 0 {
		return fmt.Errorf("max_idle_conns must not be negative")
	}

	if o.MaxOpenConns > 0 && o.MaxIdleConns > o.MaxOpenConns {
		return fmt.Errorf("max_idle_conns must be less than or equal to max_open_conns")
	}

	if o.ConnMaxLifetime < 0 {
		return fmt.Errorf("conn_max_lifetime must not be negative")
	}

	if o.ConnMaxIdleTime < 0 {
		return fmt.Errorf("conn_max_idle_time must not be negative")
	}

	return nil
}

type SQLiteConnectionOptions struct {
	Database string `toml:"database"`
}

func (o SQLiteConnectionOptions) Validate() error {
	if strings.TrimSpace(o.Database) == "" {
		return fmt.Errorf("database is required")
	}

	return nil
}

type PostgresConnectionOptions struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Database string `toml:"database"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

func (o PostgresConnectionOptions) Validate() error {
	return validateNetworkConnectionOptions(o.Host, o.Port, o.Database, o.Username)
}

type MySQLConnectionOptions struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Database string `toml:"database"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

func (o MySQLConnectionOptions) Validate() error {
	return validateNetworkConnectionOptions(o.Host, o.Port, o.Database, o.Username)
}

func validateNetworkConnectionOptions(host string, port int, database string, username string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host is required")
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	if strings.TrimSpace(database) == "" {
		return fmt.Errorf("database is required")
	}

	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("username is required")
	}

	return nil
}
