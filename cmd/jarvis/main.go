package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kshaham/jarvis/internal/pty"
	"github.com/kshaham/jarvis/internal/skillsbundle"
	"github.com/kshaham/jarvis/internal/state"
	"github.com/kshaham/jarvis/internal/web"
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

// scrubInheritedAuthEnv removes ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN
// from the Jarvis process's environment at startup so they can't leak
// into child PTY / SDK sessions via os.Environ() and silently override
// subscription-auth credentials. When the operator picks
// `claude-subscription` provider, the child must see no auth env vars
// so the Claude CLI reads ~/.claude/.credentials.json itself.
// Jarvis never reads these vars for its own purposes (verified by grep
// of the tree), so unsetting is safe. API-key and Z.AI modes still
// work because those set their own env via ProviderEnv on the child,
// independent of the parent's environment.
//
// When JARVIS_SCRUB_CREDENTIALS=1 is set, also unset any inherited
// env var whose name ends in a sensitive suffix (_SECRET, _TOKEN,
// _PASSWORD, _KEY, _CREDENTIAL). Default off because operators
// commonly export GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY, etc. for
// downstream tools (`gh`, `aws`); aggressively unsetting would
// silently break those flows. Opt-in is safer.
func scrubInheritedAuthEnv() {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if os.Getenv(k) != "" {
			slog.Warn("unsetting inherited auth env var to prevent subscription-auth shadowing", "key", k)
			_ = os.Unsetenv(k)
		}
	}
	if os.Getenv("JARVIS_SCRUB_CREDENTIALS") == "1" {
		scrubCredentialEnvByPattern()
	}
}

// scrubCredentialEnvByPattern unsets any env var matching the
// credential suffix pattern. Called only when the operator opts in
// via JARVIS_SCRUB_CREDENTIALS=1. Logs each removal so operators see
// exactly what was scrubbed (debugging "why does my GITHUB_TOKEN
// vanish" is otherwise a head-scratcher).
func scrubCredentialEnvByPattern() {
	suffixes := []string{"_SECRET", "_TOKEN", "_PASSWORD", "_KEY", "_CREDENTIAL"}
	for _, raw := range os.Environ() {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			continue
		}
		name := raw[:eq]
		upper := strings.ToUpper(name)
		for _, suffix := range suffixes {
			if strings.HasSuffix(upper, suffix) {
				slog.Warn("unsetting inherited credential env var (JARVIS_SCRUB_CREDENTIALS=1)", "key", name)
				_ = os.Unsetenv(name)
				break
			}
		}
	}
}

func main() {
	configureLogging()
	scrubInheritedAuthEnv()

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

	// Materialize the embedded skill bundle into ~/.claude/skills once at
	// startup so the team gets its skills with no external skills/ directory.
	// Non-fatal: a failure only degrades skill availability, not the daemon.
	if home, err := os.UserHomeDir(); err == nil {
		if n, err := skillsbundle.Materialize(filepath.Join(home, ".claude", "skills")); err != nil {
			slog.Warn("skills bundle materialize failed", "err", err)
		} else {
			slog.Info("materialized bundled skills", "count", n)
		}
	}

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
	// Wire Codex turn-cost computation to the shared pricing table (kept as a
	// package var on pty to avoid a pty -> state import dependency). Without
	// this, Codex EventTokens.Cost would always be 0.
	pty.CodexCostFunc = state.ComputeCost
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

	// Cancel the root context BEFORE Stop on EVERY path (idempotent on the
	// signal path). The errCh path -- server.Start() returning a runtime
	// listener error -- otherwise leaves rootCtx live, so in-flight background
	// jobs (e.g. standup audio) that derive their context from it wouldn't
	// abort, and Stop()'s drain could block well past the shutdown budget.
	cancel()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*1e9)
	defer shutdownCancel()

	_ = server.Stop(shutdownCtx)
	_ = sessionManager.StopAllSessions(shutdownCtx)

	slog.Info("Jarvis Go shutdown complete")
}
