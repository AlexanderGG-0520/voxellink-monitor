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

type fakeImporter struct{}

func (fakeImporter) Import(context.Context, string) (domain.Server, error) {
	return domain.Server{}, nil
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
