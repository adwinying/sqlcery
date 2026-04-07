package db

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/adwinying/sqlcery/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type sshTunnel struct {
	dialContext func(context.Context, string, string) (net.Conn, error)
	close       func() error
}

var openSSHTunnel = func(ctx context.Context, sshHost string) (*sshTunnel, error) {
	resolved, err := config.ResolveSSHHost(sshHost)
	if err != nil {
		return nil, err
	}

	authMethods, agentConn, err := sshAuthMethods(resolved.IdentityFiles)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := sshHostKeyCallback(resolved)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}

	clientConfig := &ssh.ClientConfig{
		User:            resolved.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}

	address := net.JoinHostPort(resolved.Host, fmt.Sprintf("%d", resolved.Port))
	client, err := sshDial(ctx, "tcp", address, clientConfig)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("open ssh tunnel via %q (%s): %w", sshHost, address, err)
	}

	closeOnce := sync.Once{}
	closeFn := func() error {
		var closeErr error
		closeOnce.Do(func() {
			if err := client.Close(); err != nil {
				closeErr = err
			}
			if agentConn != nil {
				if err := agentConn.Close(); err != nil && closeErr == nil {
					closeErr = err
				}
			}
		})
		return closeErr
	}

	return &sshTunnel{
		dialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			type result struct {
				conn net.Conn
				err  error
			}

			resultCh := make(chan result, 1)
			go func() {
				conn, err := client.Dial(network, address)
				resultCh <- result{conn: conn, err: err}
			}()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case res := <-resultCh:
				return res.conn, res.err
			}
		},
		close: closeFn,
	}, nil
}

var sshDial = func(ctx context.Context, network string, address string, clientConfig *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	clientConn, channels, requests, err := ssh.NewClientConn(conn, address, clientConfig)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	_ = conn.SetDeadline(timeZero())
	return ssh.NewClient(clientConn, channels, requests), nil
}

func sshAuthMethods(identityFiles []string) ([]ssh.AuthMethod, net.Conn, error) {
	authMethods := make([]ssh.AuthMethod, 0, len(identityFiles)+1)
	agentConn, agentAuth, err := sshAgentAuthMethod()
	if err != nil {
		return nil, nil, err
	}
	if agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
	}

	identityAuth, err := sshIdentityAuthMethods(identityFiles)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, err
	}
	authMethods = append(authMethods, identityAuth...)

	if len(authMethods) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, fmt.Errorf("no ssh authentication methods available")
	}

	return authMethods, agentConn, nil
}

func sshAgentAuthMethod() (net.Conn, ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil, nil
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, fmt.Errorf("connect ssh agent: %w", err)
	}

	return conn, ssh.PublicKeysCallback(agent.NewClient(conn).Signers), nil
}

func sshIdentityAuthMethods(identityFiles []string) ([]ssh.AuthMethod, error) {
	authMethods := make([]ssh.AuthMethod, 0, len(identityFiles))
	for _, path := range identityFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read ssh identity %s: %w", path, err)
		}

		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse ssh identity %s: %w", path, err)
		}

		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	return authMethods, nil
}

func sshHostKeyCallback(resolved config.ResolvedSSHHost) (ssh.HostKeyCallback, error) {
	switch resolved.StrictHostKeyChecking {
	case "no", "off", "false":
		return ssh.InsecureIgnoreHostKey(), nil
	}

	files := make([]string, 0, len(resolved.KnownHostsFiles))
	for _, path := range resolved.KnownHostsFiles {
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat known_hosts %s: %w", path, err)
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("known_hosts file is required for ssh host %q", resolved.Alias)
	}

	callback, err := knownhosts.New(files...)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts for ssh host %q: %w", resolved.Alias, err)
	}

	return callback, nil
}

func timeZero() time.Time {
	return time.Time{}
}
