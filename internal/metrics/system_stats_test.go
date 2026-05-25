package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSystemStats_BasicShape(t *testing.T) {
	start := time.Now().Add(-time.Minute) // 60s uptime
	got := SystemStats(start, 42)

	if got.Sessions != 42 {
		t.Errorf("Sessions=%d want 42", got.Sessions)
	}
	if got.CPUs != runtime.NumCPU() {
		t.Errorf("CPUs=%d want %d", got.CPUs, runtime.NumCPU())
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion=%q want %q", got.GoVersion, runtime.Version())
	}
	if got.Memory.UsedMB <= 0 {
		t.Error("UsedMB should be positive")
	}
	if got.Memory.Percent < 0 || got.Memory.Percent > 100 {
		t.Errorf("Percent=%v out of [0,100]", got.Memory.Percent)
	}
	if got.Memory.TotalMB < 1024 {
		t.Errorf("TotalMB=%v below 1GB floor", got.Memory.TotalMB)
	}
	if !strings.Contains(got.Uptime, "s") && !strings.Contains(got.Uptime, "m") {
		t.Errorf("Uptime=%q should contain a time unit", got.Uptime)
	}
}

func TestSystemStats_CPUFromStubProcStat(t *testing.T) {
	// Build a synthetic /proc/self/stat with known utime+stime so we
	// can assert CPU% calculation deterministically.
	tmp := filepath.Join(t.TempDir(), "stat")
	// /proc/self/stat field layout: pid (comm) state ppid pgrp ...
	// utime is field 14, stime is field 15. We make all earlier
	// fields zeros (the values don't matter) so utime + stime land
	// at the right positions.
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[13] = "100" // utime = 1.00 CPU-second
	fields[14] = "200" // stime = 2.00 CPU-seconds
	body := strings.Join(fields, " ") + "\n"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := SetProcStatPathForTest(tmp)
	t.Cleanup(func() { SetProcStatPathForTest(prev) })

	// 100s uptime: utime+stime = 3s of CPU. Per-CPU%:
	//   (3 / 100) * 100 / numCPUs = 3 / numCPUs
	start := time.Now().Add(-100 * time.Second)
	got := SystemStats(start, 0)

	expected := 3.0 / float64(runtime.NumCPU())
	// Round to 1 decimal to match the helper's rounding.
	expectedRounded := roundOne(expected)
	if got.CPU != expectedRounded {
		t.Errorf("CPU=%v want %v", got.CPU, expectedRounded)
	}
}

func TestSystemStats_CPUZeroWhenProcUnavailable(t *testing.T) {
	prev := SetProcStatPathForTest("/nonexistent/proc/stat")
	t.Cleanup(func() { SetProcStatPathForTest(prev) })

	got := SystemStats(time.Now().Add(-time.Minute), 0)
	if got.CPU != 0 {
		t.Errorf("expected CPU=0 when /proc unavailable, got %v", got.CPU)
	}
	// Other fields should still populate.
	if got.Sessions != 0 {
		t.Errorf("Sessions=%d want 0", got.Sessions)
	}
}

func TestSystemStats_ZeroUptimeYieldsZeroCPU(t *testing.T) {
	// time.Since(startTime) ≤ 0 → divide-by-zero guard kicks in.
	got := SystemStats(time.Now().Add(time.Minute) /* future! */, 0)
	if got.CPU != 0 {
		t.Errorf("future startTime should yield CPU=0, got %v", got.CPU)
	}
}

func TestRoundOne(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{1.23456, 1.2},
		{1.25, 1.3}, // half-up
		{0, 0},
		{-1.45, -1.5},
		{99.99, 100},
	}
	for _, tc := range cases {
		if got := roundOne(tc.in); got != tc.want {
			t.Errorf("roundOne(%v)=%v want %v", tc.in, got, tc.want)
		}
	}
}
