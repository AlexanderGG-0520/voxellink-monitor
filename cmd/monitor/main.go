package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	monitorDiscord "github.com/alexandergg-0520/voxellink-monitor/internal/discord"
	"github.com/alexandergg-0520/voxellink-monitor/internal/integration"
	"github.com/alexandergg-0520/voxellink-monitor/internal/integration/voxellink"
	"github.com/alexandergg-0520/voxellink-monitor/internal/migrate"
	"github.com/alexandergg-0520/voxellink-monitor/internal/monitor"
	"github.com/alexandergg-0520/voxellink-monitor/internal/store"
	"github.com/alexandergg-0520/voxellink-monitor/internal/transport"
	"github.com/alexandergg-0520/voxellink-monitor/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
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
	case "migrate":
		migrateDatabase()
	default:
		log.Fatalf("unknown role %q", role)
	}
}
func migrateDatabase() {
	pool, err := pgxpool.New(context.Background(), requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Apply(context.Background(), pool); err != nil {
		log.Fatal(err)
	}
	log.Print("database migrations applied")
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
	app, err := web.New(repository, importer, web.Config{ClientID: requiredEnv("DISCORD_CLIENT_ID"), ClientSecret: requiredEnv("DISCORD_CLIENT_SECRET"), PublicBaseURL: requiredEnv("PUBLIC_BASE_URL"), SessionSecret: requiredEnv("SESSION_SECRET"), IntegrationToken: requiredEnv("INTEGRATION_SYNC_TOKEN")})
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.ListenAndServe(env("HTTP_ADDR", ":8080"), app.Handler()))
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
	tunnel := transport.NewAccessTunnel(env("CLOUDFLARED_BIN", "/usr/local/bin/cloudflared"), durationEnv("CLOUDFLARED_STARTUP_TIMEOUT", 10*time.Second))
	w := monitor.NewWorker(repository, durationEnv("MONITOR_INTERVAL", time.Minute), durationEnv("STATUS_TIMEOUT", 5*time.Second), durationEnv("FAILURE_RETRY_INTERVAL", 10*time.Second), slog.Default(), notifier, tunnel)
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
