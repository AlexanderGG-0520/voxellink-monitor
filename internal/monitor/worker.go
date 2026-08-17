// Package monitor schedules external player-perspective checks.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"github.com/alexandergg-0520/voxellink-monitor/internal/minecraft"
)

type Repository interface {
	EnabledServers(context.Context) ([]domain.Server, error)
	RecordCheck(context.Context, domain.Server, domain.CheckResult) (bool, domain.PublicStatus, error)
}
type Notifier interface {
	StateChanged(context.Context, domain.Server, domain.PublicStatus, domain.CheckResult) error
}
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
}

func NewWorker(repository Repository, interval, timeout, retryInterval time.Duration, logger *slog.Logger, notifier Notifier) *Worker {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &Worker{repository: repository, interval: interval, timeout: timeout, retryInterval: retryInterval, logger: logger, notifier: notifier}
}
func (w *Worker) Run(ctx context.Context) error {
	if err := w.RunOnce(ctx); err != nil {
		w.logger.Error("initial monitor pass failed", "error", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.logger.Error("monitor pass failed", "error", err)
			}
		}
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
		result := w.probe(server)
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

func (w *Worker) probe(server domain.Server) domain.CheckResult {
	switch server.Transport {
	case "DIRECT", "CLOUDFLARE_SPECTRUM":
		return minecraft.PingJava(server.Hostname, server.Port, w.timeout)
	case "CLOUDFLARE_TUNNEL":
		return domain.CheckResult{Outcome: domain.ProbeError, Detail: "cloudflared tunnel adapter is not configured", At: time.Now()}
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
