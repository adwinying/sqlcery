package db

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type sshTunnel struct {
	localPort int
	close     func() error
}

var openSSHTunnel = func(ctx context.Context, sshHost string, dbHost string, dbPort int) (*sshTunnel, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("pick local port for ssh tunnel: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	forward := fmt.Sprintf("%d:%s:%d", localPort, dbHost, dbPort)
	cmd := exec.Command("ssh",
		"-N",
		"-L", forward,
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		sshHost,
	)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh tunnel to %q: %w", sshHost, err)
	}

	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
	}()

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeoutCh := time.After(time.Until(deadline))

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-exitCh
			return nil, ctx.Err()

		case <-exitCh:
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				return nil, fmt.Errorf("ssh tunnel to %q: %s", sshHost, stderr)
			}
			return nil, fmt.Errorf("ssh tunnel to %q exited unexpectedly", sshHost)

		case <-timeoutCh:
			_ = cmd.Process.Kill()
			<-exitCh
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				return nil, fmt.Errorf("ssh tunnel to %q timed out: %s", sshHost, stderr)
			}
			return nil, fmt.Errorf("ssh tunnel to %q timed out waiting for port %d", sshHost, localPort)

		case <-ticker.C:
			conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 50*time.Millisecond)
			if dialErr != nil {
				continue
			}
			_ = conn.Close()

			closeOnce := sync.Once{}
			return &sshTunnel{
				localPort: localPort,
				close: func() error {
					closeOnce.Do(func() {
						_ = cmd.Process.Kill()
						<-exitCh
					})
					return nil
				},
			}, nil
		}
	}
}
