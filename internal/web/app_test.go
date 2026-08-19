package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

type fakeRepository struct{ servers []domain.Server }

func (f fakeRepository) ServersForDiscordMember(context.Context, string) ([]domain.Server, error) {
	return f.servers, nil
}
func (fakeRepository) SetEnabledForDiscordMember(context.Context, string, string, bool) error {
	return nil
}
func (fakeRepository) SetNotificationChannelForDiscordMember(context.Context, string, string, string) error {
	return nil
}
func (fakeRepository) ScheduleMaintenanceForDiscordMember(context.Context, string, string, time.Time, time.Time) error {
	return nil
}
func (f fakeRepository) SnapshotByID(context.Context, string) (domain.ServerSnapshot, error) {
	return domain.ServerSnapshot{Server: f.servers[0]}, nil
}
func (f fakeRepository) PublicSnapshots(context.Context) ([]domain.ServerSnapshot, error) {
	return []domain.ServerSnapshot{{Server: f.servers[0]}}, nil
}
func (fakeRepository) Uptime24h(context.Context, string) (float64, error) { return 100, nil }
func (fakeRepository) RecentIncidents(context.Context, string, int) ([]domain.Incident, error) {
	return nil, nil
}
func (fakeRepository) RecordUserReport(context.Context, domain.UserReport) (domain.CrowdSignal, error) {
	return domain.CrowdSignal{}, nil
}
func (fakeRepository) CrowdSignal(context.Context, string) (domain.CrowdSignal, error) {
	return domain.CrowdSignal{}, nil
}

type fakeImporter struct{}

func (fakeImporter) Import(context.Context, string) (domain.Server, error) {
	return domain.Server{}, nil
}
func TestPublicReportIsAccepted(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/servers/server-id/reports", strings.NewReader("report_type=CONNECTION&detail=cannot+join"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", response.Code)
	}
}
func TestHomeRendersReportForm(t *testing.T) {
	app := testApp(t)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/reports") {
		t.Fatalf("public report form was not rendered: status=%d body=%q", response.Code, response.Body.String())
	}
}
func testApp(t *testing.T) *App {
	t.Helper()
	app, err := New(fakeRepository{servers: []domain.Server{{Name: "Example", Status: domain.Operational}}}, fakeImporter{}, Config{ClientID: "client", ClientSecret: "secret", PublicBaseURL: "http://localhost:8080", SessionSecret: "this-is-a-long-enough-test-session-secret", IntegrationToken: "this-is-a-long-enough-token"})
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func TestSignedSessionAuthenticatesConsole(t *testing.T) {
	app := testApp(t)
	cookie, err := app.signSession("discord-user", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/console", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Example") {
		t.Fatal("console did not render owned server")
	}
}

func TestMonitorCanBeMountedBelowAPublicPath(t *testing.T) {
	app, err := New(fakeRepository{servers: []domain.Server{{Name: "Example", Status: domain.Operational}}}, fakeImporter{}, Config{ClientID: "client", ClientSecret: "secret", PublicBaseURL: "https://voxellink.example.com/monitor", SessionSecret: "this-is-a-long-enough-test-session-secret", IntegrationToken: "this-is-a-long-enough-token"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/monitor/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `action="/monitor/servers/`) || !strings.Contains(response.Body.String(), `href="/monitor/login"`) {
		t.Fatalf("subpath page was not rendered correctly: status=%d body=%q", response.Code, response.Body.String())
	}
}
