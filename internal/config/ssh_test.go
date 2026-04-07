package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveSSHHostAppliesSpecificAndWildcardConfig(t *testing.T) {
	homeDir := filepath.Join(string(filepath.Separator), "Users", "tester")
	resolved, err := resolveSSHHost("bastion", homeDir, "local-user", []byte("Host *\n  User shared-user\n  IdentityFile ~/.ssh/id_shared\n  UserKnownHostsFile ~/.ssh/known_hosts\nHost bastion\n  HostName bastion.internal\n  Port 2222\n  IdentityFile ~/.ssh/%r_%h\n"))
	if err != nil {
		t.Fatalf("resolveSSHHost() error = %v", err)
	}

	if got, want := resolved.Alias, "bastion"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
	if got, want := resolved.Host, "bastion.internal"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if got, want := resolved.Port, 2222; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}
	if got, want := resolved.User, "shared-user"; got != want {
		t.Fatalf("User = %q, want %q", got, want)
	}

	wantIdentityFiles := []string{
		filepath.Join(homeDir, ".ssh", "id_shared"),
		filepath.Join(homeDir, ".ssh", "shared-user_bastion.internal"),
	}
	if got := resolved.IdentityFiles; !reflect.DeepEqual(got, wantIdentityFiles) {
		t.Fatalf("IdentityFiles = %#v, want %#v", got, wantIdentityFiles)
	}

	wantKnownHosts := []string{filepath.Join(homeDir, ".ssh", "known_hosts")}
	if got := resolved.KnownHostsFiles; !reflect.DeepEqual(got, wantKnownHosts) {
		t.Fatalf("KnownHostsFiles = %#v, want %#v", got, wantKnownHosts)
	}
}

func TestResolveSSHHostDefaultsWithoutConfig(t *testing.T) {
	homeDir := filepath.Join(string(filepath.Separator), "Users", "tester")
	resolved, err := resolveSSHHost("jumpbox", homeDir, "app", nil)
	if err != nil {
		t.Fatalf("resolveSSHHost() error = %v", err)
	}

	if got, want := resolved.Host, "jumpbox"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if got, want := resolved.Port, 22; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}
	if got, want := resolved.User, "app"; got != want {
		t.Fatalf("User = %q, want %q", got, want)
	}
	wantKnownHosts := []string{filepath.Join(homeDir, ".ssh", "known_hosts")}
	if got := resolved.KnownHostsFiles; !reflect.DeepEqual(got, wantKnownHosts) {
		t.Fatalf("KnownHostsFiles = %#v, want %#v", got, wantKnownHosts)
	}
}

func TestResolveSSHHostSpecificConfigOverridesWildcard(t *testing.T) {
	resolved, err := resolveSSHHost("prod-db", filepath.Join(string(filepath.Separator), "Users", "tester"), "local-user", []byte("Host *\n  User wildcard\n  Port 22\nHost prod-*\n  User deploy\n  Port 2200\n"))
	if err != nil {
		t.Fatalf("resolveSSHHost() error = %v", err)
	}

	if got, want := resolved.User, "deploy"; got != want {
		t.Fatalf("User = %q, want %q", got, want)
	}
	if got, want := resolved.Port, 2200; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}
}

func TestResolveSSHHostRejectsInvalidPort(t *testing.T) {
	_, err := resolveSSHHost("bastion", "/tmp/home", "app", []byte("Host bastion\n  Port nope\n"))
	if err == nil {
		t.Fatal("resolveSSHHost() error = nil, want error")
	}

	if got, want := err.Error(), `invalid port "nope"`; !strings.Contains(got, want) {
		t.Fatalf("resolveSSHHost() error = %q, want to contain %q", got, want)
	}
}
