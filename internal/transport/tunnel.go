// Package transport contains non-direct probe transports.
package transport

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"github.com/alexandergg-0520/voxellink-monitor/internal/minecraft"
)

// AccessTunnel runs cloudflared as a short-lived local TCP listener. Its
// authentication material stays in cloudflared's mounted configuration, never
// in Monitor PostgreSQL or API responses.
type AccessTunnel struct {
	binary         string
	startupTimeout time.Duration
}

func NewAccessTunnel(binary string, startupTimeout time.Duration) *AccessTunnel {
	if binary == "" {
		binary = "cloudflared"
	}
	return &AccessTunnel{binary: binary, startupTimeout: startupTimeout}
}
func (t *AccessTunnel) Ping(ctx context.Context, hostname string, port int, timeout time.Duration) domain.CheckResult {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return probeError(err)
	}
	localAddress := listener.Addr().String()
	_ = listener.Close()
	command := exec.CommandContext(ctx, t.binary, "access", "tcp", "--hostname", hostname, "--url", localAddress)
	if err := command.Start(); err != nil {
		return probeError(fmt.Errorf("start cloudflared: %w", err))
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if err := waitForListener(ctx, localAddress, t.startupTimeout); err != nil {
		return probeError(fmt.Errorf("cloudflared unavailable: %w", err))
	}
	_, portText, _ := net.SplitHostPort(localAddress)
	var localPort int
	if _, err := fmt.Sscan(portText, &localPort); err != nil {
		return probeError(err)
	}
	return minecraft.PingJavaEndpoint("127.0.0.1", localPort, hostname, port, timeout)
}
func waitForListener(ctx context.Context, address string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("listener did not become ready")
		case <-ticker.C:
		}
	}
}
func probeError(err error) domain.CheckResult {
	return domain.CheckResult{Outcome: domain.ProbeError, Detail: err.Error(), At: time.Now()}
}
