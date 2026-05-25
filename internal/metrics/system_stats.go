package metrics

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SystemStats captures the daemon's live resource usage in the
// shape the dashboard's /api/system/stats endpoint serves. Held
// here (not in the handler) so other consumers (CLI status,
// mobile-app health pings) can reuse the same numbers without
// re-implementing the /proc parsing.

// SystemStatsResult is the JSON payload shape. Fields named to
// match the existing dashboard contract; renaming would force a
// frontend churn.
type SystemStatsResult struct {
	CPU        float64       `json:"cpu"`
	Memory     MemoryStats   `json:"memory"`
	Goroutines int           `json:"goroutines"`
	CPUs       int           `json:"cpus"`
	GoVersion  string        `json:"goVersion"`
	AllocMB    float64       `json:"allocMB"`
	SysMB      float64       `json:"sysMB"`
	Uptime     string        `json:"uptime"`
	Sessions   int           `json:"sessions"`
}

// MemoryStats is the nested memory object on the JSON payload.
type MemoryStats struct {
	UsedMB  float64 `json:"usedMB"`
	TotalMB float64 `json:"totalMB"`
	Percent float64 `json:"percent"`
}

// procStatPath is the file SystemStats reads to compute CPU%.
// Pinned here (not parameterised) so tests can swap it via the
// SetProcStatPathForTest seam when needed.
var procStatPath = "/proc/self/stat"

// SetProcStatPathForTest swaps the /proc path SystemStats reads
// for a test-controlled file. Returns the previous value so the
// test can restore via t.Cleanup.
func SetProcStatPathForTest(path string) (previous string) {
	previous = procStatPath
	procStatPath = path
	return previous
}

// SystemStats reads runtime + /proc state into a SystemStatsResult.
// Caller passes:
//   - startTime: the daemon's start instant (used for CPU% and
//     uptime).
//   - sessionCount: live session count from the session manager
//     (kept out of this helper because office can't see pty).
//
// CPU% calculation: reads jiffies from /proc/self/stat fields 14-15
// (utime + stime), divides by 100 to get seconds, then normalises
// against process uptime + NumCPU. Returns 0 when /proc isn't
// available (non-Linux hosts).
//
// Memory: RSS is approximated from runtime.MemStats.Sys (total
// bytes obtained from the OS), and total system memory is rough-
// estimated as 4× RSS with a 1 GB floor. Good enough for the
// dashboard's "where are we relative to baseline?" signal; not a
// substitute for cgroup-based monitoring in production.
func SystemStats(startTime time.Time, sessionCount int) SystemStatsResult {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	rssMB := float64(memStats.Sys) / 1024 / 1024

	cpuPercent := readCPUPercent(startTime)

	// Estimate total system memory: process uses ~25% (rough heuristic).
	totalMB := rssMB * 4
	if totalMB < 1024 {
		totalMB = 1024
	}
	memPercent := (rssMB / totalMB) * 100
	if memPercent > 100 {
		memPercent = 100
	}

	return SystemStatsResult{
		CPU: roundOne(cpuPercent),
		Memory: MemoryStats{
			UsedMB:  roundOne(rssMB),
			TotalMB: roundOne(totalMB),
			Percent: roundOne(memPercent),
		},
		Goroutines: runtime.NumGoroutine(),
		CPUs:       runtime.NumCPU(),
		GoVersion:  runtime.Version(),
		AllocMB:    float64(memStats.Alloc) / 1024 / 1024,
		SysMB:      rssMB,
		Uptime:     time.Since(startTime).String(),
		Sessions:   sessionCount,
	}
}

// readCPUPercent reads /proc/self/stat and computes the
// process-cumulative CPU% averaged over its lifetime. Returns 0
// when /proc is unavailable or the fields can't be parsed.
func readCPUPercent(startTime time.Time) float64 {
	data, err := os.ReadFile(procStatPath)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) <= 14 {
		return 0
	}
	utime, _ := strconv.ParseFloat(fields[13], 64)
	stime, _ := strconv.ParseFloat(fields[14], 64)
	// Convert jiffies → seconds at the standard 100 Hz clock.
	totalCPUSeconds := (utime + stime) / 100.0
	uptimeSeconds := time.Since(startTime).Seconds()
	if uptimeSeconds <= 0 {
		return 0
	}
	return (totalCPUSeconds / uptimeSeconds) * 100.0 / float64(runtime.NumCPU())
}

// roundOne rounds to one decimal place. Used so the dashboard
// doesn't render fifteen digits of float precision.
func roundOne(f float64) float64 {
	return math.Round(f*10) / 10
}
