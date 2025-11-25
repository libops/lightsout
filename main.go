package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type Config struct {
	Port              string
	InactivityTimeout time.Duration
	LibOpsKeepOnline  string
	LogLevel          string
	GoogleProjectID   string
	GCEZone           string
	GCEInstance       string
}

type ActivityTracker struct {
	mu           sync.RWMutex
	requestCount int64
	lastPing     time.Time
}

var (
	config         *Config
	tracker        *ActivityTracker
	shutdownTimer  *time.Timer
	shutdownMutex  sync.Mutex
	serverShutdown = make(chan struct{})
	// Dependency injection for testing - initialize later to avoid cycle
	suspendFunc func() error
)

func init() {
	config = loadConfig()
	tracker = &ActivityTracker{
		lastPing: time.Now(),
	}
	setupLogging()
	// Initialize suspendFunc to avoid initialization cycle
	suspendFunc = suspendInstance
}

func loadConfig() *Config {
	return &Config{
		Port:              getEnv("PORT", "8808"),
		InactivityTimeout: getDurationEnv("INACTIVITY_TIMEOUT", 90) * time.Second,
		LogLevel:          getEnv("LOG_LEVEL", "INFO"),
		GoogleProjectID:   getEnv("GCP_PROJECT", ""),
		GCEZone:           getEnv("GCP_ZONE", ""),
		GCEInstance:       getEnv("GCP_INSTANCE_NAME", ""),
		LibOpsKeepOnline:  getEnv("LIBOPS_KEEP_ONLINE", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getDurationEnv(key string, defaultSeconds int) time.Duration {
	if value := getEnv(key, ""); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			return time.Duration(seconds)
		}
	}
	return time.Duration(defaultSeconds)
}

func setupLogging() {
	var level slog.Level
	switch strings.ToUpper(config.LogLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	handler := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(handler)
}

func resetShutdownTimer() {
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()

	if shutdownTimer != nil {
		shutdownTimer.Stop()
	}

	shutdownTimer = time.AfterFunc(config.InactivityTimeout, func() {
		slog.Info("Inactivity timeout reached, initiating shutdown",
			"timeout_seconds", int(config.InactivityTimeout.Seconds()))
		initiateShutdown()
	})

	slog.Debug("Shutdown timer reset", "timeout_seconds", int(config.InactivityTimeout.Seconds()))
}

func stopShutdownTimer() {
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()

	if shutdownTimer != nil {
		shutdownTimer.Stop()
		shutdownTimer = nil
		slog.Debug("Shutdown timer stopped")
	}
}

func getLastGitHubActionsActivity() (time.Time, error) {
	cmd := exec.Command("docker", "logs", "--tail", "1", "github-actions-runner")
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("no github-actions-runner logs: %v", err)
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return time.Time{}, fmt.Errorf("empty github-actions-runner logs")
	}

	// Parse timestamp from the beginning of the log line
	parts := strings.Split(line, ":")
	if len(parts) >= 3 {
		timeStr := parts[0] + ":" + parts[1] + ":" + parts[2]
		if t, err := time.Parse("15:04:05", timeStr); err == nil {
			// Add today's date
			now := time.Now()
			return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse github-actions timestamp")
}

func createComputeService(ctx context.Context) (*compute.Service, error) {
	// Use Application Default Credentials (ADC)
	// This will automatically use:
	// 1. GOOGLE_APPLICATION_CREDENTIALS environment variable
	// 2. GCE metadata server (when running on GCE)
	// 3. gcloud CLI credentials
	creds, err := google.FindDefaultCredentials(ctx, compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	service, err := compute.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create compute service: %w", err)
	}

	return service, nil
}

func suspendMachine() (*compute.Instance, error) {
	ctx := context.Background()

	slog.Info("Checking if machine is suspended",
		"project", config.GoogleProjectID,
		"zone", config.GCEZone,
		"instance", config.GCEInstance)

	// Create compute service with default credentials
	service, err := createComputeService(ctx)
	if err != nil {
		return nil, fmt.Errorf("createComputeService: %v", err)
	}

	// Get instance details
	instance, err := service.Instances.Get(config.GoogleProjectID, config.GCEZone, config.GCEInstance).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %v", err)
	}

	// If the machine is running, suspend it
	if instance.Status == "RUNNING" {
		slog.Info("Instance is RUNNING, suspending instance")
		_, err := service.Instances.Suspend(config.GoogleProjectID, config.GCEZone, config.GCEInstance).Context(ctx).Do()
		if err != nil {
			return instance, fmt.Errorf("failed to suspend instance: %v", err)
		}
	} else {
		slog.Info("Instance is not RUNNING, skipping suspension", "status", instance.Status)
	}

	return instance, nil
}

func suspendInstance() error {
	slog.Info("Attempting to suspend instance directly via GCP API")

	// Reset the timer before suspension to prevent immediate shutdown after wake-up
	resetShutdownTimer()

	_, err := suspendMachine()
	if err != nil {
		return fmt.Errorf("failed to suspend machine: %v", err)
	}

	slog.Info("Suspend request completed successfully")
	return nil
}

func initiateShutdown() {
	tracker.mu.RLock()
	lastPing := tracker.lastPing
	tracker.mu.RUnlock()

	now := time.Now()
	duration := now.Sub(lastPing)

	// Check GitHub Actions as fallback
	if lastGHA, err := getLastGitHubActionsActivity(); err == nil {
		ghaDuration := now.Sub(lastGHA)
		if ghaDuration < config.InactivityTimeout {
			slog.Info("Staying online for GitHub Actions",
				"gha_duration_seconds", int(ghaDuration.Seconds()))
			// Reset timer for another round
			resetShutdownTimer()
			return
		}
	}

	slog.Info("Proceeding with shutdown",
		"ping_duration_seconds", int(duration.Seconds()))

	// Check if we have the required GCP configuration
	if config.GoogleProjectID == "" || config.GCEZone == "" || config.GCEInstance == "" {
		slog.Warn("Missing GCP configuration, cannot suspend",
			"project", config.GoogleProjectID,
			"zone", config.GCEZone,
			"instance", config.GCEInstance)
	} else {
		if err := suspendFunc(); err != nil {
			slog.Error("Failed to suspend instance", "error", err)
		} else {
			slog.Info("Suspend request sent successfully")
		}
	}

	// Signal server shutdown (protected by mutex to prevent race condition)
	shutdownMutex.Lock()
	defer shutdownMutex.Unlock()

	select {
	case <-serverShutdown:
		// Channel already closed, nothing to do
	default:
		close(serverShutdown)
	}
}

func pingHandler(w http.ResponseWriter, r *http.Request) {
	tracker.mu.Lock()
	tracker.lastPing = time.Now()
	tracker.requestCount++
	tracker.mu.Unlock()

	// Reset the shutdown timer
	resetShutdownTimer()

	slog.Info("Ping request received",
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
		"timer_reset", true)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("pong")); err != nil {
		slog.Error("Failed to write ping response", "error", err)
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
}

func main() {
	slog.Info("Lightswitch starting",
		"port", config.Port,
		"inactivity_timeout", config.InactivityTimeout,
		"keep_online", config.LibOpsKeepOnline == "yes")

	// Check if this is a paid site that should stay online
	if config.LibOpsKeepOnline != "yes" {
		slog.Info("Starting inactivity timer", "timeout_seconds", int(config.InactivityTimeout.Seconds()))
		resetShutdownTimer()
	}

	// Setup HTTP handlers
	http.HandleFunc("/ping", pingHandler)
	http.HandleFunc("/healthcheck", healthHandler)

	// Setup HTTP server
	server := &http.Server{
		Addr:         ":" + config.Port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("HTTP server starting", "port", config.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Wait for shutdown signal or internal shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	select {
	case <-sigChan:
		slog.Info("Shutdown signal received")
	case <-serverShutdown:
		slog.Info("Internal shutdown triggered")
	}

	slog.Info("Gracefully shutting down...")

	// Stop the shutdown timer
	stopShutdownTimer()

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Lightswitch shutdown complete")
}
