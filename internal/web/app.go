// Package web serves the public status surface and the Discord-authenticated owner console.
package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
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
	SnapshotByID(context.Context, string) (domain.ServerSnapshot, error)
	PublicSnapshots(context.Context) ([]domain.ServerSnapshot, error)
	Uptime24h(context.Context, string) (float64, error)
	RecentIncidents(context.Context, string, int) ([]domain.Incident, error)
	RecordUserReport(context.Context, domain.UserReport) (domain.CrowdSignal, error)
	CrowdSignal(context.Context, string) (domain.CrowdSignal, error)
	SetEnabledForDiscordMember(context.Context, string, string, bool) error
	SetNotificationChannelForDiscordMember(context.Context, string, string, string) error
	ScheduleMaintenanceForDiscordMember(context.Context, string, string, time.Time, time.Time) error
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
type ConsoleServer struct {
	Snapshot  domain.ServerSnapshot
	Uptime24h float64
	Incidents []domain.Incident
}

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
	mux.HandleFunc("/servers/", a.report)
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
	servers, err := a.repository.PublicSnapshots(r.Context())
	if err != nil {
		http.Error(w, "could not load status", 500)
		return
	}
	type publicServer struct {
		domain.ServerSnapshot
		Crowd domain.CrowdSignal
	}
	view := make([]publicServer, 0, len(servers))
	for _, server := range servers {
		signal, signalErr := a.repository.CrowdSignal(r.Context(), server.ID)
		if signalErr != nil {
			http.Error(w, "could not load crowd signal", 500)
			return
		}
		view = append(view, publicServer{server, signal})
	}
	render(w, homeTemplate, struct{ Servers []publicServer }{view})
}
func (a *App) report(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/servers/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "reports" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	kind := domain.UserReportType(r.Form.Get("report_type"))
	switch kind {
	case domain.ReportConnection, domain.ReportLogin, domain.ReportTimeout, domain.ReportLag, domain.ReportOther:
	default:
		http.Error(w, "invalid report type", 400)
		return
	}
	detail := strings.TrimSpace(r.Form.Get("detail"))
	if len(detail) > 280 {
		http.Error(w, "report is too long", 400)
		return
	}
	_, err := a.repository.RecordUserReport(r.Context(), domain.UserReport{ServerID: parts[0], Type: kind, Detail: detail, ReporterHash: a.reporterHash(r, parts[0]), At: time.Now().UTC()})
	if err != nil {
		http.Error(w, "could not save report", 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) reporterHash(r *http.Request, serverID string) string {
	// The rotating hash is only used for daily de-duplication; raw client data
	// is never persisted. SessionSecret keeps it non-reversible to the public.
	day := time.Now().UTC().Format("2006-01-02")
	input := strings.Join([]string{a.config.SessionSecret, clientAddress(r), r.UserAgent(), serverID, day}, "|")
	digest := sha256.Sum256([]byte(input))
	return hex.EncodeToString(digest[:])
}
func clientAddress(r *http.Request) string {
	// cloudflared/Cloudflare supplies CF-Connecting-IP. X-Forwarded-For keeps
	// the form usable behind a conventional reverse proxy; deployments should
	// strip this client-supplied header at their trusted proxy boundary.
	if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); value != "" {
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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
	view := make([]ConsoleServer, 0, len(servers))
	for _, server := range servers {
		snapshot, err := a.repository.SnapshotByID(r.Context(), server.ID)
		if err != nil {
			http.Error(w, "could not load server details", 500)
			return
		}
		uptime, err := a.repository.Uptime24h(r.Context(), server.ID)
		if err != nil {
			http.Error(w, "could not load uptime", 500)
			return
		}
		incidents, err := a.repository.RecentIncidents(r.Context(), server.ID, 3)
		if err != nil {
			http.Error(w, "could not load incidents", 500)
			return
		}
		view = append(view, ConsoleServer{Snapshot: snapshot, Uptime24h: uptime, Incidents: incidents})
	}
	render(w, consoleTemplate, struct{ Servers []ConsoleServer }{view})
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
	case "maintenance":
		start, startErr := time.ParseInLocation("2006-01-02T15:04", r.Form.Get("starts_at"), time.FixedZone("JST", 9*60*60))
		end, endErr := time.ParseInLocation("2006-01-02T15:04", r.Form.Get("ends_at"), time.FixedZone("JST", 9*60*60))
		if startErr != nil || endErr != nil || !end.After(start) {
			http.Error(w, "有効な開始・終了時刻をJSTで指定してください", http.StatusBadRequest)
			return
		}
		err = a.repository.ScheduleMaintenanceForDiscordMember(r.Context(), parts[0], userID, start.UTC(), end.UTC())
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

const homeTemplate = `<!doctype html><meta charset="utf-8"><title>VoxelLink Monitor</title><style>body{font:16px system-ui;background:#10131a;color:#eef2ff;margin:auto;max-width:760px;padding:32px}a,button,input,select{color:#9ed0ff}section{background:#1b2230;padding:16px;margin:12px 0;border-radius:10px}.status{font-weight:bold}input,select,button{padding:6px;margin:3px;background:#10131a;border:1px solid #526078;border-radius:4px}</style><main><h1>VoxelLink Monitor</h1><p>Minecraft server availability — active probe + player reports.</p>{{if .Servers}}{{range .Servers}}<section><strong>{{.Name}}</strong><p class="status">{{.Status}}{{if .LastOutcome}} · 最終確認 {{.LastCheckedAt}}{{end}}</p>{{if .Crowd.Anomalous}}<p>プレイヤー報告が通常より増えています（直近15分: {{.Crowd.Reports}}件 / 判定基準: {{.Crowd.Threshold}}件）。</p>{{end}}<form method="post" action="/servers/{{.ID}}/reports"><label>接続トラブルを報告 <select name="report_type"><option value="CONNECTION">接続できない</option><option value="LOGIN">ログインできない</option><option value="TIMEOUT">タイムアウト</option><option value="LAG">ラグ・遅延</option><option value="OTHER">その他</option></select></label><input name="detail" maxlength="280" placeholder="任意（280文字まで）"><button>報告する</button></form><small>同じサーバーへの報告は1日1件まで集計されます。IPアドレス等は保存しません。</small></section>{{end}}{{else}}<p>現在、公開中の監視対象はありません。</p>{{end}}<p><a href="/login">Discordで管理画面へログイン</a></p></main>`
const consoleTemplate = `<!doctype html><meta charset="utf-8"><title>VoxelLink Monitor Console</title><style>body{font:16px system-ui;background:#10131a;color:#eef2ff;margin:auto;max-width:900px;padding:32px}section{background:#1b2230;padding:18px;margin:16px 0;border-radius:10px}form{margin:10px 0}input,button{padding:7px;margin:3px}.grid{display:flex;gap:20px;flex-wrap:wrap}.status{font-weight:bold}</style><main><h1>あなたの監視サーバー</h1>{{if .Servers}}{{range .Servers}}<section><h2>{{.Snapshot.Name}}</h2><p class="status">{{.Snapshot.Status}} · {{.Snapshot.Hostname}}:{{.Snapshot.Port}} · {{.Snapshot.Transport}}</p><div class="grid"><span>直近24時間: <strong>{{printf "%.2f" .Uptime24h}}%</strong></span>{{if .Snapshot.LastOutcome}}<span>最終確認: {{.Snapshot.LastCheckedAt}}{{if .Snapshot.Latency}} / {{.Snapshot.Latency.Milliseconds}}ms{{end}}</span>{{end}}</div><h3>直近Incident</h3>{{if .Incidents}}<ul>{{range .Incidents}}<li>{{.StartedAt}} — {{.State}}（{{.Reason}}）</li>{{end}}</ul>{{else}}<p>Incidentはありません。</p>{{end}}<form method="post" action="/console/servers/{{.Snapshot.ID}}/enabled"><input type="hidden" name="enabled" value="{{if .Snapshot.Enabled}}false{{else}}true{{end}}"><button>{{if .Snapshot.Enabled}}監視を無効化{{else}}監視を有効化{{end}}</button></form><form method="post" action="/console/servers/{{.Snapshot.ID}}/channel"><label>Discord ステータスチャンネルID <input name="channel_id" required></label><button>通知先を保存</button></form><form method="post" action="/console/servers/{{.Snapshot.ID}}/maintenance"><label>メンテ開始（JST）<input type="datetime-local" name="starts_at" required></label><label>終了（JST）<input type="datetime-local" name="ends_at" required></label><button>メンテナンスを予定</button></form></section>{{end}}{{else}}<p>管理できるVoxelLink掲載サーバーはまだありません。</p>{{end}}</main>`
