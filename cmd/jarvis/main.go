package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/claudeman/jarvis-go/internal/pty"
	"github.com/claudeman/jarvis-go/internal/web"
)

var version = "dev"

// configureLogging sets the default slog handler from environment variables.
// JARVIS_LOG_LEVEL: debug|info|warn|error (default info)
// JARVIS_LOG_FORMAT: text|json (default text)
func configureLogging() {
	var level slog.Level
	switch strings.ToLower(os.Getenv("JARVIS_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(os.Getenv("JARVIS_LOG_FORMAT")) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func main() {
	configureLogging()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "web":
		runWeb()
	case "version":
		fmt.Printf("jarvis %s (go)\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("Jarvis %s (Go)\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  jarvis web [--port PORT]   Start the web server (default: 3000)")
	fmt.Println("  jarvis version             Print version")
}

func runWeb() {
	port := 3000
	https := false

	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			fmt.Sscanf(os.Args[i+1], "%d", &port)
		}
		if arg == "--https" {
			https = true
		}
	}

	slog.Info("Starting Jarvis Go", "version", version, "port", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("Shutdown signal received", "signal", sig)
		cancel()
	}()

	// Initialize core layers
	sessionManager := pty.NewSessionManager(pty.MaxConcurrentSessions)
	server := web.NewServer(ctx, port, https, sessionManager)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		slog.Info("Shutting down gracefully...")
	case err := <-errCh:
		if err != nil {
			slog.Error("Server error", "error", err)
		}
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*1e9)
	defer shutdownCancel()

	_ = server.Stop(shutdownCtx)
	_ = sessionManager.StopAllSessions(shutdownCtx)

	slog.Info("Jarvis Go shutdown complete")
}
