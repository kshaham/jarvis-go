package pty

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// reapTargetComms are the agent CLI process names the office spawns. The
// graceful Stop path cancels the SDK/Codex context, which signals the direct
// child, but does not necessarily tear down that child's own descendants
// (claude spawns MCP-server children, etc.). Those can linger and pile up.
var reapTargetComms = map[string]bool{
	"claude": true,
	"codex":  true,
}

// isReapTarget reports whether a /proc entry with the given comm name and
// owning uid is an orphaned agent subprocess this process should SIGKILL.
// It matches only the agent CLIs the office spawns (claude/codex) AND only
// processes owned by our own euid, so the backstop can never reach beyond
// the jarvis user's own process tree. The caller is responsible for skipping
// our own PID.
func isReapTarget(comm string, uid, selfUID int) bool {
	if !reapTargetComms[comm] {
		return false
	}
	return uid == selfUID
}

// ReapAgentSubprocesses is a Linux-only, best-effort shutdown backstop that
// SIGKILLs any lingering agent subprocesses this process owns. It exists for
// the case where a graceful Stop did not fully tear down a claude/codex
// process tree -- e.g. claude's MCP-server children, which survive the
// context cancellation that kills only the direct child.
//
// It is GATED on running as PID 1: inside the jarvis container jarvis is the
// init process, so every agent process is its descendant and "claude/codex
// owned by our uid" is unambiguously our orphan. We deliberately do NOT reap
// when we are not PID 1 -- a developer running jarvis-go on a Linux host could
// have their own personal `claude` session under the same uid, and we must
// never kill that. So this only fires in the container (the case that actually
// orphans). It is also a no-op on non-Linux dev hosts (darwin). Returns the
// number of processes successfully killed.
func ReapAgentSubprocesses() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	// Container-only: PID 1 means we are the container init and every agent is
	// our descendant. Outside a container jarvis is not PID 1, so skip to avoid
	// reaching another claude session owned by the same developer.
	if os.Getpid() != 1 {
		return 0
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	self := os.Getpid()
	selfUID := os.Geteuid()
	killed := 0

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}

		comm, ok := readProcComm(pid)
		if !ok {
			continue
		}
		uid, ok := readProcUID(pid)
		if !ok {
			continue
		}
		if !isReapTarget(comm, uid, selfUID) {
			continue
		}

		// Tolerate races: the pid may have already exited between the reads
		// above and the kill below, so ignore the error.
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}

	if killed > 0 {
		slog.Info("reaped orphaned agent subprocesses", "count", killed)
	}
	return killed
}

// readProcComm reads /proc/<pid>/comm and returns the trimmed process name.
// Returns ok=false if the entry can't be read (the process vanished, a race
// we tolerate during shutdown).
func readProcComm(pid int) (string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(data), "\n"), true
}

// readProcUID parses the real UID (first field of the "Uid:" line) from
// /proc/<pid>/status. Returns ok=false if the file can't be read or the line
// is absent/malformed.
func readProcUID(pid int) (int, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "Uid:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		uid, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0, false
		}
		return uid, true
	}
	return 0, false
}
