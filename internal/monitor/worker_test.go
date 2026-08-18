package monitor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

type notificationRepository struct {
	pending   []domain.PendingStateNotification
	delivered []int64
}

func (r *notificationRepository) EnabledServers(context.Context) ([]domain.Server, error) {
	return nil, nil
}
func (r *notificationRepository) RecordCheck(context.Context, domain.Server, domain.CheckResult) (bool, domain.PublicStatus, error) {
	return false, domain.Operational, nil
}
func (r *notificationRepository) PendingStateNotifications(context.Context, int) ([]domain.PendingStateNotification, error) {
	return r.pending, nil
}
func (r *notificationRepository) MarkStateNotificationDelivered(_ context.Context, id int64) error {
	r.delivered = append(r.delivered, id)
	return nil
}

type recordingNotifier struct{ states []domain.PublicStatus }

func (n *recordingNotifier) StateChanged(_ context.Context, _ domain.Server, state domain.PublicStatus, _ domain.CheckResult) error {
	n.states = append(n.states, state)
	return nil
}

func TestDispatchPendingNotificationsDeliversAndAcknowledges(t *testing.T) {
	repository := &notificationRepository{pending: []domain.PendingStateNotification{{ID: 7, Server: domain.Server{ID: "server", Name: "Example"}, State: domain.Degraded, Result: domain.CheckResult{Outcome: domain.Success, At: time.Now()}}}}
	notifier := &recordingNotifier{}
	worker := NewWorker(repository, time.Minute, time.Second, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)), notifier, nil, nil)
	worker.dispatchPendingNotifications(context.Background())
	if len(notifier.states) != 1 || notifier.states[0] != domain.Degraded {
		t.Fatalf("notifications = %#v", notifier.states)
	}
	if len(repository.delivered) != 1 || repository.delivered[0] != 7 {
		t.Fatalf("delivered = %#v", repository.delivered)
	}
}
