package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/minecraft"
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
		log.Print("Discord bot foundation ready; configure DISCORD_BOT_TOKEN to enable gateway adapter")
		select {}
	default:
		log.Fatalf("unknown role %q", role)
	}
}
func api() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/api/v1/probe", func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		if host == "" {
			http.Error(w, "host is required", http.StatusBadRequest)
			return
		}
		result := minecraft.PingJava(host, 25565, 5*time.Second)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	log.Fatal(http.ListenAndServe(env("HTTP_ADDR", ":8080"), mux))
}
func worker() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	repository, err := store.Connect(ctx, requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	w := monitor.NewWorker(repository, durationEnv("MONITOR_INTERVAL", time.Minute), durationEnv("STATUS_TIMEOUT", 5*time.Second), durationEnv("FAILURE_RETRY_INTERVAL", 10*time.Second), slog.Default())
	if err := w.Run(ctx); err != nil && err != context.Canceled {
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
