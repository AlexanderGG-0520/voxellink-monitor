// Package monitor schedules external player-perspective checks.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"github.com/alexandergg-0520/voxellink-monitor/internal/minecraft"
	"github.com/alexandergg-0520/voxellink-monitor/internal/transport"
)

type Repository interface {
	EnabledServers(context.Context) ([]domain.Server, error)
	RecordCheck(context.Context, domain.Server, domain.CheckResult) (bool, domain.PublicStatus, error)
}
type Notifier interface {
	StateChanged(context.Context, domain.Server, domain.PublicStatus, domain.CheckResult) error
}
type TunnelProber interface {
	Ping(context.Context, string, int, time.Duration) domain.CheckResult
}
type Synchronizer interface{ Sync(context.Context) error }
type NoopNotifier struct{}

func (NoopNotifier) StateChanged(context.Context, domain.Server, domain.PublicStatus, domain.CheckResult) error {
	return nil
}

type Worker struct {
	repository        Repository
	interval, timeout time.Duration
	retryInterval     time.Duration
	logger            *slog.Logger
	notifier          Notifier
	tunnel            TunnelProber
	synchronizer      Synchronizer
}

func NewWorker(repository Repository, interval, timeout, retryInterval time.Duration, logger *slog.Logger, notifier Notifier, tunnel TunnelProber, synchronizer Synchronizer) *Worker {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if tunnel == nil {
		tunnel = transport.NewAccessTunnel("", timeout)
	}
	return &Worker{repository: repository, interval: interval, timeout: timeout, retryInterval: retryInterval, logger: logger, notifier: notifier, tunnel: tunnel, synchronizer: synchronizer}
}
func (w *Worker) Run(ctx context.Context) error {
	w.runRetention(ctx)
	w.syncVoxelLink(ctx)
	if err := w.RunOnce(ctx); err != nil {
		w.logger.Error("initial monitor pass failed", "error", err)
	}
	ticker := time.NewTicker(w.interval)
	retentionTicker := time.NewTicker(6 * time.Hour)
	syncTicker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	defer retentionTicker.Stop()
	defer syncTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.logger.Error("monitor pass failed", "error", err)
			}
		case <-retentionTicker.C:
			w.runRetention(ctx)
		case <-syncTicker.C:
			w.syncVoxelLink(ctx)
		}
	}
}
func (w *Worker) syncVoxelLink(ctx context.Context) {
	if w.synchronizer == nil {
		return
	}
	if err := w.synchronizer.Sync(ctx); err != nil {
		w.logger.Warn("VoxelLink sync failed; existing monitoring continues", "error", err)
		return
	}
	w.logger.Info("VoxelLink metadata sync complete")
}
func (w *Worker) runRetention(ctx context.Context) {
	if repository, ok := w.repository.(interface {
		RunRetention(context.Context) (domain.RetentionStats, error)
	}); ok {
		stats, err := repository.RunRetention(ctx)
		if err != nil {
			w.logger.Error("retention failed", "error", err)
			return
		}
		w.logger.Info("retention complete", "raw_deleted", stats.RawDeleted, "15m_deleted", stats.FifteenMinuteDeleted, "hourly_deleted", stats.HourlyDeleted)
	}
}
func (w *Worker) RunOnce(ctx context.Context) error {
	servers, err := w.repository.EnabledServers(ctx)
	if err != nil {
		return err
	}
	for _, server := range servers {
		w.check(ctx, server)
	}
	return nil
}
func (w *Worker) check(ctx context.Context, server domain.Server) {
	// A normal pass makes one observation. A service-facing failure gets two
	// confirmation checks; an outage recovering gets its second confirmation.
	for attempt := 0; attempt < 3; attempt++ {
		result := w.probe(ctx, server)
		changed, state, err := w.repository.RecordCheck(ctx, server, result)
		if err != nil {
			w.logger.Error("record check", "server_id", server.ID, "error", err)
			return
		}
		w.logger.Info("server checked", "server_id", server.ID, "outcome", result.Outcome, "changed", changed, "state", state, "attempt", attempt+1)
		if changed {
			if err := w.notifier.StateChanged(ctx, server, state, result); err != nil {
				w.logger.Error("send status notification", "server_id", server.ID, "error", err)
			}
		}
		if state == domain.Maintenance {
			return
		}
		previousState := server.Status
		server.Status = state
		if result.Outcome == domain.ProbeError || result.Outcome == domain.Success && previousState != domain.Outage {
			return
		}
		if result.Outcome == domain.Success && previousState == domain.Outage && attempt == 1 {
			return
		}
		if attempt == 2 {
			return
		}
		if !sleep(ctx, w.retryInterval) {
			return
		}
	}
}

func (w *Worker) probe(ctx context.Context, server domain.Server) domain.CheckResult {
	switch server.Transport {
	case "DIRECT", "CLOUDFLARE_SPECTRUM":
		return minecraft.PingJava(server.Hostname, server.Port, w.timeout)
	case "CLOUDFLARE_TUNNEL":
		return w.tunnel.Ping(ctx, server.Hostname, server.Port, w.timeout)
	default:
		return domain.CheckResult{Outcome: domain.ProbeError, Detail: fmt.Sprintf("unsupported transport %q", server.Transport), At: time.Now()}
	}
}
func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
