package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	monitorDiscord "github.com/alexandergg-0520/voxellink-monitor/internal/discord"
	"github.com/alexandergg-0520/voxellink-monitor/internal/integration"
	"github.com/alexandergg-0520/voxellink-monitor/internal/integration/voxellink"
	"github.com/alexandergg-0520/voxellink-monitor/internal/monitor"
	"github.com/alexandergg-0520/voxellink-monitor/internal/store"
)

func main() {
	role := "api"
	if len(os.Args) > 1 {
		role = os.Args[1]
	}
	switch role {
	case "api":
		api()
	case "worker":
		worker()
	case "bot":
		bot()
	default:
		log.Fatalf("unknown role %q", role)
	}
}
func api() {
	ctx := context.Background()
	repository, err := store.Connect(ctx, requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	client, err := voxellink.New(requiredEnv("VOXELLINK_API_BASE_URL"), requiredEnv("VOXELLINK_API_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	importer := integration.NewImporter(client, repository)
	syncToken := requiredEnv("INTEGRATION_SYNC_TOKEN")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/api/v1/integrations/voxellink/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r, syncToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		defer r.Body.Close()
		var request struct {
			ExternalServerID string `json:"external_server_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		server, err := importer.Import(r.Context(), request.ExternalServerID)
		if err != nil {
			slog.Default().Warn("VoxelLink import failed", "error", err)
			http.Error(w, "VoxelLink import failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(server)
	})
	log.Fatal(http.ListenAndServe(env("HTTP_ADDR", ":8080"), mux))
}
func authorized(request *http.Request, token string) bool {
	value := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	return len(value) == len(token) && subtle.ConstantTimeCompare([]byte(value), []byte(token)) == 1
}
func worker() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	repository, err := store.Connect(ctx, requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	var notifier monitor.Notifier
	if token := os.Getenv("DISCORD_BOT_TOKEN"); token != "" {
		notifier, err = monitorDiscord.NewNotifier(token, repository)
		if err != nil {
			log.Fatal(err)
		}
	}
	w := monitor.NewWorker(repository, durationEnv("MONITOR_INTERVAL", time.Minute), durationEnv("STATUS_TIMEOUT", 5*time.Second), durationEnv("FAILURE_RETRY_INTERVAL", 10*time.Second), slog.Default(), notifier)
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
func bot() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	repository, err := store.Connect(ctx, requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	instance, err := monitorDiscord.NewBot(requiredEnv("DISCORD_BOT_TOKEN"), repository)
	if err != nil {
		log.Fatal(err)
	}
	if err := instance.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func requiredEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("%s must be set", key)
	}
	return value
}
func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("invalid %s: %v", key, err)
	}
	return parsed
}
