package discord

import (
	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestStatusMessageIsPlayerFacing(t *testing.T) {
	message := statusMessage(domain.Server{Name: "Example"}, domain.Outage, domain.CheckResult{})
	if !strings.Contains(message, "Example") || !strings.Contains(message, "接続できません") {
		t.Fatalf("unexpected message: %s", message)
	}
}
func TestSnapshotMessageOmitsInternalError(t *testing.T) {
	message := snapshotMessage(domain.ServerSnapshot{Server: domain.Server{Name: "Example", Status: domain.Operational}, LastOutcome: domain.Success, LastCheckedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Latency: 23 * time.Millisecond})
	if strings.Contains(message, "STATUS_TIMEOUT") {
		t.Fatal("internal detail leaked")
	}
}
