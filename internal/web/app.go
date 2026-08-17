// Package web serves the public status surface and the Discord-authenticated owner console.
package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
)

const sessionCookie = "voxellink_monitor_session"

type Repository interface {
	ServersForDiscordMember(context.Context, string) ([]domain.Server, error)
	SetEnabledForDiscordMember(context.Context, string, string, bool) error
	SetNotificationChannelForDiscordMember(context.Context, string, string, string) error
}
type Importer interface {
	Import(context.Context, string) (domain.Server, error)
}
type App struct {
	repository Repository
	importer   Importer
	config     Config
	states     map[string]time.Time
	mu         sync.Mutex
}
type Config struct{ ClientID, ClientSecret, PublicBaseURL, SessionSecret, IntegrationToken string }

func New(repository Repository, importer Importer, config Config) (*App, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.PublicBaseURL == "" || len(config.SessionSecret) < 24 || len(config.IntegrationToken) < 16 {
		return nil, errors.New("Discord OAuth and session configuration must be set")
	}
	return &App{repository: repository, importer: importer, config: config, states: map[string]time.Time{}}, nil
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.health)
	mux.HandleFunc("/", a.home)
	mux.HandleFunc("/login", a.login)
	mux.HandleFunc("/oauth/discord/callback", a.callback)
	mux.HandleFunc("/console", a.console)
	mux.HandleFunc("/console/servers/", a.updateServer)
	mux.HandleFunc("/api/v1/integrations/voxellink/import", a.importServer)
	return mux
}
func (a *App) health(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	user, _ := a.user(r)
	render(w, homeTemplate, struct{ User string }{User: user})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "could not start login", 500)
		return
	}
	a.mu.Lock()
	a.states[state] = time.Now().Add(10 * time.Minute)
	a.mu.Unlock()
	redirect := a.config.PublicBaseURL + "/oauth/discord/callback"
	values := url.Values{"client_id": {a.config.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {"identify"}, "state": {state}}
	http.Redirect(w, r, "https://discord.com/oauth2/authorize?"+values.Encode(), http.StatusFound)
}
func (a *App) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if !a.consumeState(state) {
		http.Error(w, "login session expired; try again", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Discord did not return a code", http.StatusBadRequest)
		return
	}
	userID, err := a.discordUserID(r.Context(), code)
	if err != nil {
		http.Error(w, "Discord login failed", http.StatusBadGateway)
		return
	}
	session, err := a.signSession(userID, time.Now().Add(24*time.Hour))
	if err != nil {
		http.Error(w, "could not create session", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: session, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.config.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 86400})
	http.Redirect(w, r, "/console", http.StatusFound)
}
func (a *App) console(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.user(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	servers, err := a.repository.ServersForDiscordMember(r.Context(), userID)
	if err != nil {
		http.Error(w, "could not load servers", 500)
		return
	}
	render(w, consoleTemplate, struct{ Servers []domain.Server }{servers})
}
func (a *App) updateServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.user(r)
	if !ok {
		http.Error(w, "login required", 401)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/console/servers/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	var err error
	switch parts[1] {
	case "enabled":
		err = a.repository.SetEnabledForDiscordMember(r.Context(), parts[0], userID, r.Form.Get("enabled") == "true")
	case "channel":
		channel := strings.TrimSpace(r.Form.Get("channel_id"))
		if channel == "" {
			http.Error(w, "channel ID is required", 400)
			return
		}
		err = a.repository.SetNotificationChannelForDiscordMember(r.Context(), parts[0], userID, channel)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		http.Error(w, "could not save setting", 403)
		return
	}
	http.Redirect(w, r, "/console", http.StatusSeeOther)
}
func (a *App) importServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !bearerMatches(r, a.config.IntegrationToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	defer r.Body.Close()
	var body struct {
		ExternalServerID string `json:"external_server_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	server, err := a.importer.Import(r.Context(), body.ExternalServerID)
	if err != nil {
		http.Error(w, "VoxelLink import failed", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(server)
}
func (a *App) discordUserID(ctx context.Context, code string) (string, error) {
	form := url.Values{"client_id": {a.config.ClientID}, "client_secret": {a.config.ClientSecret}, "grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {a.config.PublicBaseURL + "/oauth/discord/callback"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://discord.com/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("token exchange: %s", response.Status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", err
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("Discord identity: %s", response.Status)
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&user); err != nil {
		return "", err
	}
	if user.ID == "" {
		return "", errors.New("Discord identity missing ID")
	}
	return user.ID, nil
}
func (a *App) consumeState(state string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.states[state]
	delete(a.states, state)
	return ok && time.Now().Before(expires)
}
func (a *App) signSession(user string, expires time.Time) (string, error) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(user + "." + fmt.Sprint(expires.Unix())))
	mac := hmac.New(sha256.New, []byte(a.config.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (a *App) user(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(a.config.SessionSecret))
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	fields := strings.Split(string(payload), ".")
	if len(fields) != 2 {
		return "", false
	}
	var expiry int64
	if _, err := fmt.Sscan(fields[1], &expiry); err != nil || time.Now().After(time.Unix(expiry, 0)) {
		return "", false
	}
	return fields[0], true
}
func bearerMatches(r *http.Request, token string) bool {
	value := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(value) == len(token) && hmac.Equal([]byte(value), []byte(token))
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func render(w http.ResponseWriter, source string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := template.Must(template.New("page").Parse(source)).Execute(w, data); err != nil {
		http.Error(w, "render error", 500)
	}
}

const homeTemplate = `<!doctype html><title>VoxelLink Monitor</title><main><h1>VoxelLink Monitor</h1><p>Minecraft server availability, independently observed.</p><a href="/login">Discordで管理画面へログイン</a></main>`
const consoleTemplate = `<!doctype html><title>VoxelLink Monitor Console</title><main><h1>あなたの監視サーバー</h1>{{if .Servers}}{{range .Servers}}<section><h2>{{.Name}}</h2><p>{{.Status}} · {{.Hostname}}:{{.Port}} · {{.Transport}}</p><form method="post" action="/console/servers/{{.ID}}/enabled"><input type="hidden" name="enabled" value="{{if .Enabled}}false{{else}}true{{end}}"><button>{{if .Enabled}}監視を無効化{{else}}監視を有効化{{end}}</button></form><form method="post" action="/console/servers/{{.ID}}/channel"><label>Discord ステータスチャンネルID <input name="channel_id" required></label><button>通知先を保存</button></form></section>{{end}}{{else}}<p>管理できるVoxelLink掲載サーバーはまだありません。</p>{{end}}</main>`
