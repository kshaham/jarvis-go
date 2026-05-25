#!/usr/bin/env bash
# verify-phase6.sh -- throwaway smoke test for the R3-R4 changes shipped
# in this session. Verifies build, vet, the new test suites, and the
# codegraph wiring without booting a full jarvis daemon. Delete after
# the changes ship to prod.
#
# Usage:
#   scripts/verify-phase6.sh           # run all checks
#   scripts/verify-phase6.sh --quick   # skip race detector (faster)
#
# Exit codes:
#   0 = all green
#   1 = a check failed (look at the most recent FAIL line)
#
# What this DOESN'T do: spin up the jarvis HTTP server, log in, and
# curl the new endpoints. That's covered by the httptest-based tests
# under internal/web/handler/ (office_snapshot_test.go,
# pr_lookup_test.go, claude_session_ops_test.go, spawn_remote_test.go).
# Running it as a real HTTP integration test would need a configured
# auth provider + a free port + a stub jarvis-mcp; out of scope.

set -uo pipefail
cd "$(dirname "$0")/.."

QUICK=0
for arg in "$@"; do
    if [ "$arg" = "--quick" ]; then
        QUICK=1
    fi
done

green() { printf '\033[32m✓\033[0m %s\n' "$1"; }
red()   { printf '\033[31m✗\033[0m %s\n' "$1"; }
note()  { printf '\033[90m·\033[0m %s\n' "$1"; }

failures=0
fail() { red "$1"; failures=$((failures + 1)); }

note "Phase 6 verification starting in $(pwd)"

# ---------- build ----------
note "go build ./..."
if go build ./... 2>&1 | tee /tmp/jarvis-phase6-build.log | grep -q .; then
    fail "go build emitted output (see /tmp/jarvis-phase6-build.log)"
else
    green "go build clean"
fi

# ---------- vet ----------
note "go vet ./..."
if go vet ./... 2>&1 | tee /tmp/jarvis-phase6-vet.log | grep -q .; then
    fail "go vet emitted output (see /tmp/jarvis-phase6-vet.log)"
else
    green "go vet clean"
fi

# ---------- tests ----------
TEST_ARGS=(-count=1 -timeout 180s)
if [ $QUICK -eq 0 ]; then
    TEST_ARGS+=(-race)
    note "go test (with -race; pass --quick to skip)"
else
    note "go test (quick mode, no -race)"
fi

PKGS=(
    ./internal/office/...
    ./internal/pty/...
    ./internal/web/handler/...
    ./internal/claudehooks/...
    ./internal/subagent/...
    ./internal/logscrub/...
    ./internal/testutil/...
)
if ! go test "${TEST_ARGS[@]}" "${PKGS[@]}" 2>&1 | tee /tmp/jarvis-phase6-test.log; then
    fail "go test failed (see /tmp/jarvis-phase6-test.log)"
else
    green "go test green across $(printf '%s ' "${PKGS[@]}")"
fi

# ---------- new tests are actually present ----------
note "verifying new test files exist + are non-empty"
NEW_TESTS=(
    internal/office/codegraph_test.go
    internal/office/manager_pr_test.go
    internal/office/ultrareview_test.go
    internal/pty/channels_test.go
    internal/web/handler/office_snapshot_test.go
    internal/web/handler/pr_lookup_test.go
    internal/web/handler/claude_session_ops_test.go
    internal/web/handler/office_task_complete_test.go
    internal/web/handler/spawn_remote_test.go
    internal/logscrub/scrub_test.go
    internal/office/ecc_test.go
    internal/office/ecc_checksum_test.go
    internal/web/handler/ecc_test.go
)
for f in "${NEW_TESTS[@]}"; do
    if [ ! -s "$f" ]; then
        fail "expected test file missing or empty: $f"
    fi
done
if [ $failures -eq 0 ]; then
    green "all ${#NEW_TESTS[@]} new test files present"
fi

# ---------- codegraph wiring ----------
note "checking codegraph wiring"
if ! command -v codegraph >/dev/null 2>&1; then
    note "codegraph CLI not on PATH; the runtime gate will skip MCP wiring (graceful)"
else
    green "codegraph CLI present at $(command -v codegraph)"
    if [ ! -d .codegraph ]; then
        note "jarvis-go itself is not yet indexed (operator can run: codegraph init -i)"
    else
        green ".codegraph/ index exists at $(pwd)/.codegraph"
    fi
fi

# Confirm the spawn-flow code references codegraph (regression guard
# against accidental deletion during a future merge conflict).
note "verifying spawn-flow references codegraph"
# Use extended-regex greps that tolerate formatting drift (whitespace
# around the bracket, optional package qualifier, etc.). P7 cleanup.
if grep -Eq 'mcpServers[[:space:]]*\[[[:space:]]*"codegraph"' internal/web/handler/office.go; then
    green 'mcpServers["codegraph"] entry present in office.go'
else
    fail 'mcpServers["codegraph"] entry missing from office.go'
fi
if grep -Eq '(office\.)?EnsureCodegraphIndex[[:space:]]*\(' internal/web/handler/office.go; then
    green "office.EnsureCodegraphIndex wired into spawnTeamFromRequest"
else
    fail "office.EnsureCodegraphIndex call missing from spawnTeamFromRequest"
fi

# ---------- summary ----------
echo
if [ $failures -eq 0 ]; then
    green "Phase 6 verification: ALL CHECKS PASSED"
    exit 0
fi
red "Phase 6 verification: $failures check(s) failed"
exit 1
