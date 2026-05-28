package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kshaham/jarvis/internal/pty"
)

func main() {
	fmt.Println("Jarvis Go PTY Server - Test Binary")

	manager := pty.NewSessionManager(pty.MaxConcurrentSessions)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	config := pty.SessionConfig{
		WorkingDir: os.Getenv("HOME"),
		Mode:       pty.ModeShell,
		Name:       "test-session",
	}

	session, err := manager.CreateSession(ctx, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Session created: %s (PID: %d)\n", session.ID, session.PID())

	for event := range session.Events() {
		switch e := event.(type) {
		case pty.EventTerminal:
			fmt.Print(e.Data)
		case pty.EventExit:
			fmt.Printf("\n[Session exited with code %d]\n", e.ExitCode)
			return
		}
	}
}
