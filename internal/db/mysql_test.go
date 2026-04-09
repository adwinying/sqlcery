package db

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestMySQLConnConfig(t *testing.T) {
	connConfig := mysqlConnConfig(config.Connection{
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	})

	if got, want := connConfig.Net, "tcp"; got != want {
		t.Fatalf("connConfig.Net = %q, want %q", got, want)
	}

	if got, want := connConfig.Addr, "db.example.com:3306"; got != want {
		t.Fatalf("connConfig.Addr = %q, want %q", got, want)
	}

	if got, want := connConfig.DBName, "warehouse"; got != want {
		t.Fatalf("connConfig.DBName = %q, want %q", got, want)
	}

	if got, want := connConfig.User, "app"; got != want {
		t.Fatalf("connConfig.User = %q, want %q", got, want)
	}

	if got, want := connConfig.Passwd, "secret"; got != want {
		t.Fatalf("connConfig.Passwd = %q, want %q", got, want)
	}
}

func TestMySQLConnConfigWithLifecycle(t *testing.T) {
	connConfig := mysqlConnConfigWithLifecycle(config.Connection{
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	}, config.ConnectionLifecycleOptions{
		ConnectTimeout: config.Duration(11 * time.Second),
	})

	if got, want := connConfig.Timeout, 11*time.Second; got != want {
		t.Fatalf("connConfig.Timeout = %s, want %s", got, want)
	}
}

func TestMySQLDSN(t *testing.T) {
	got := mysqlDSN(config.Connection{
		Host:     "db.example.com",
		Port:     3307,
		Database: "warehouse/reports",
		Username: "app",
		Password: "secret",
	})

	if want := "app:secret@tcp(db.example.com:3307)/warehouse%2Freports"; got != want {
		t.Fatalf("mysqlDSN() = %q, want %q", got, want)
	}
}

func TestOpenMySQLAdapterUsesDriverConnector(t *testing.T) {
	driverName := registerStubPingDriver(t, false)
	originalOpenMySQLDB := openMySQLDB
	originalOpenSSHTunnel := openSSHTunnel
	t.Cleanup(func() {
		openMySQLDB = originalOpenMySQLDB
		openSSHTunnel = originalOpenSSHTunnel
	})

	openSSHTunnel = func(context.Context, string) (*sshTunnel, error) {
		return nil, errors.New("ssh tunnel should not be opened")
	}

	openMySQLDB = func(connConfig *mysqldriver.Config) (*sql.DB, error) {
		if got, want := connConfig.Net, "tcp"; got != want {
			t.Fatalf("connConfig.Net = %q, want %q", got, want)
		}

		if got, want := connConfig.Addr, "db.example.com:3306"; got != want {
			t.Fatalf("connConfig.Addr = %q, want %q", got, want)
		}

		if got, want := connConfig.DBName, "warehouse"; got != want {
			t.Fatalf("connConfig.DBName = %q, want %q", got, want)
		}

		if got, want := connConfig.Timeout, 11*time.Second; got != want {
			t.Fatalf("connConfig.Timeout = %s, want %s", got, want)
		}

		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db, nil
	}

	adapter, err := Open(context.Background(), config.Connection{
		Type:     "mysql",
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
		Lifecycle: config.ConnectionLifecycleOptions{
			ConnectTimeout:     config.Duration(11 * time.Second),
			HealthCheckTimeout: config.Duration(time.Second),
			MaxOpenConns:       8,
			MaxIdleConns:       3,
			ConnMaxLifetime:    config.Duration(20 * time.Minute),
			ConnMaxIdleTime:    config.Duration(90 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer adapter.Close()

	if got, want := adapter.Dialect().Name(), "mysql"; got != want {
		t.Fatalf("adapter.Dialect().Name() = %q, want %q", got, want)
	}

	types, err := adapter.Types(context.Background())
	if err != nil {
		t.Fatalf("Types() error = %v", err)
	}

	if !containsType(types, TypeInfo{Name: "varchar"}) {
		t.Fatalf("Types() = %#v, want varchar entry", types)
	}
}

func TestOpenMySQLAdapterUsesSSHTunnelWhenConfigured(t *testing.T) {
	driverName := registerStubPingDriver(t, false)
	originalOpenMySQLDB := openMySQLDB
	originalOpenSSHTunnel := openSSHTunnel
	t.Cleanup(func() {
		openMySQLDB = originalOpenMySQLDB
		openSSHTunnel = originalOpenSSHTunnel
	})

	tunnelDialCalled := false
	tunnelClosed := false
	openSSHTunnel = func(_ context.Context, sshHost string) (*sshTunnel, error) {
		if got, want := sshHost, "bastion"; got != want {
			t.Fatalf("sshHost = %q, want %q", got, want)
		}

		return &sshTunnel{
			dialContext: func(context.Context, string, string) (net.Conn, error) {
				tunnelDialCalled = true
				return nil, nil
			},
			close: func() error {
				tunnelClosed = true
				return nil
			},
		}, nil
	}

	openMySQLDB = func(connConfig *mysqldriver.Config) (*sql.DB, error) {
		if connConfig.DialFunc == nil {
			t.Fatal("connConfig.DialFunc = nil, want tunnel dialer")
		}

		if _, err := connConfig.DialFunc(context.Background(), "tcp", "db.internal:3306"); err != nil {
			t.Fatalf("connConfig.DialFunc() error = %v", err)
		}

		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db, nil
	}

	adapter, err := Open(context.Background(), config.Connection{
		Type:     "mysql",
		SSHHost:  "bastion",
		Host:     "db.internal",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if !tunnelDialCalled {
		t.Fatal("tunnel dialer was not used")
	}

	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !tunnelClosed {
		t.Fatal("tunnel was not closed")
	}
}

func TestOpenMySQLReturnsPingError(t *testing.T) {
	driverName := registerStubPingDriver(t, true)
	originalOpenMySQLDB := openMySQLDB
	t.Cleanup(func() {
		openMySQLDB = originalOpenMySQLDB
	})

	openMySQLDB = func(*mysqldriver.Config) (*sql.DB, error) {
		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db, nil
	}

	_, err := Open(context.Background(), config.Connection{
		Type:     "mysql",
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if got, want := err.Error(), "ping mysql database \"warehouse\" on db.example.com:3306: ping failed"; got != want {
		t.Fatalf("Open() error = %q, want %q", got, want)
	}
}

func TestOpenMySQLReturnsAuthenticationError(t *testing.T) {
	driverName := registerStubPingDriverWithError(t, &mysqldriver.MySQLError{
		Number:  1045,
		Message: "Access denied for user 'app'@'localhost' (using password: YES)",
	})
	originalOpenMySQLDB := openMySQLDB
	t.Cleanup(func() {
		openMySQLDB = originalOpenMySQLDB
	})

	openMySQLDB = func(*mysqldriver.Config) (*sql.DB, error) {
		db, err := sql.Open(driverName, "")
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}

		return db, nil
	}

	_, err := Open(context.Background(), config.Connection{
		Type:     "mysql",
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
		Password: "secret",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Open() error = %v, want errors.Is(..., ErrAuthentication)", err)
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("Open() error = %v, want AuthenticationError", err)
	}

	if got, want := authErr.Dialect, "mysql"; got != want {
		t.Fatalf("authErr.Dialect = %q, want %q", got, want)
	}

	if got, want := err.Error(), "ping mysql database \"warehouse\" on db.example.com:3306"; !strings.Contains(got, want) {
		t.Fatalf("Open() error = %q, want to contain %q", got, want)
	}

	if got, want := err.Error(), "Access denied for user"; !strings.Contains(got, want) {
		t.Fatalf("Open() error = %q, want to contain %q", got, want)
	}
}

func TestOpenMySQLReturnsOpenError(t *testing.T) {
	originalOpenMySQLDB := openMySQLDB
	t.Cleanup(func() {
		openMySQLDB = originalOpenMySQLDB
	})

	openMySQLDB = func(*mysqldriver.Config) (*sql.DB, error) {
		return nil, errors.New("open failed")
	}

	_, err := Open(context.Background(), config.Connection{
		Type:     "mysql",
		Host:     "db.example.com",
		Port:     3306,
		Database: "warehouse",
		Username: "app",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if got, want := err.Error(), "open mysql database \"warehouse\" on db.example.com:3306: open failed"; got != want {
		t.Fatalf("Open() error = %q, want %q", got, want)
	}
}
