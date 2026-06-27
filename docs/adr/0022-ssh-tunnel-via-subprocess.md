# ADR 0022: SSH tunnel via system `ssh` subprocess

## Status

Accepted

## Context

sqlcery supports SSH tunnelling for Postgres and MySQL connections via an
`ssh_host` field in `connections.toml`. The original implementation used
`golang.org/x/crypto/ssh` to open a native Go SSH connection, then injected a
custom `DialFunc` into the DB driver config so all TCP dials went through the
tunnel.

This worked for simple host aliases, but two common `~/.ssh/config` directives
are not handled:

- **`ProxyJump`** — chain through one or more jump hosts
- **`ProxyCommand`** — delegate the transport to an arbitrary shell command
  (the dominant real-world pattern being `ssh -W %h:%p <jump-host>`)

Three approaches were considered to close this gap:

1. **Extend the native Go implementation** — parse `ProxyJump` and
   `ProxyCommand` in the Go SSH config reader, implement multi-hop chaining via
   `golang.org/x/crypto/ssh`, and handle `ProxyCommand` by spawning a
   subprocess shim that wraps stdin/stdout as a `net.Conn`. Covers all cases
   but requires ~200 extra lines for the `net.Conn` shim alone, with deadline
   handling, subprocess lifecycle, and stderr capture adding further complexity.

2. **Normalize `ProxyCommand` to `ProxyJump`** — detect the
   `ssh -W %h:%p <host>` pattern (the old spelling of `ProxyJump`) and reduce
   it to a hop internally. Covers the common case, but fails silently on any
   `ProxyCommand` that doesn't match the pattern.

3. **Spawn the system `ssh` binary with `-L`** — exec
   `ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -L <local-port>:<db-host>:<db-port> <ssh-host>`
   and connect the DB driver to `127.0.0.1:<local-port>`. All SSH config
   processing — `ProxyJump`, `ProxyCommand`, `ForwardAgent`,
   `HostKeyAlgorithms`, `Include`, etc. — is delegated to the system binary
   that already understands it.

## Decision

Use **approach 3**: open SSH tunnels by spawning the system `ssh` binary.

The local port is selected by binding `127.0.0.1:0` and letting the OS assign
a port from the ephemeral range (49152–65535); the listener is closed before
`ssh -L` is started. Tunnel readiness is detected by polling
`127.0.0.1:<local-port>` at 50 ms intervals until a TCP connection succeeds or
the connect timeout elapses. If the timeout expires, stderr captured from the
`ssh` process is included in the error message.

DB driver callers (`openPostgres`, `openMySQL`) redirect their connection
config's host and port to `127.0.0.1:<local-port>` instead of installing a
custom `DialFunc`.

## Consequences

- `internal/config/ssh.go` and its tests are deleted. The Go SSH config parser,
  `ResolvedSSHHost`, and all associated helpers are no longer needed; the system
  `ssh` binary handles config resolution.
- `internal/db/ssh_tunnel.go` is rewritten. `sshDial`, `sshAuthMethods`,
  `sshAgentAuthMethod`, `sshIdentityAuthMethods`, `sshHostKeyCallback`, and the
  `golang.org/x/crypto/ssh` import are removed.
- `openSSHTunnel` gains `dbHost string` and `dbPort int` parameters.
  `sshTunnel` exposes `localPort int` instead of `dialContext`.
- sqlcery gains a runtime dependency on `ssh` being present on `PATH`. This is
  acceptable: `ssh` ships with macOS, all major Linux distributions, and
  Windows 10+ (OpenSSH optional feature).
