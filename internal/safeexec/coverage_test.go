package safeexec

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolve_AllowedButNotOnPath covers the exec.LookPath failure branch: a
// command whose basename is allowlisted but which does not exist on PATH.
func TestResolve_AllowedButNotOnPath(t *testing.T) {
	saved := AllowedCommands
	AllowedCommands = append([]string{"definitely-not-a-real-binary-xyz"}, saved...)
	defer func() { AllowedCommands = saved }()

	if _, _, ok := Resolve("definitely-not-a-real-binary-xyz arg"); ok {
		t.Fatal("expected resolve to fail for a missing binary")
	}
}

// TestResolve_RejectsBinaryOutsideAllowedPrefix covers the prefix guard: a real,
// found, allowlisted binary that lives outside AllowedBinaryPrefixes must be
// rejected (the PATH-shim threat model).
func TestResolve_RejectsBinaryOutsideAllowedPrefix(t *testing.T) {
	dir := t.TempDir() // not under any AllowedBinaryPrefixes entry
	bin := filepath.Join(dir, "myfakebin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir) // LookPath will find it here, outside the prefixes

	saved := AllowedCommands
	AllowedCommands = append([]string{"myfakebin"}, saved...)
	defer func() { AllowedCommands = saved }()

	if _, _, ok := Resolve("myfakebin"); ok {
		t.Fatal("expected resolve to reject a binary outside allowed prefixes")
	}
}
