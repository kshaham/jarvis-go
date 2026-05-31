// Package quality runs per-project test-coverage and dependency
// vulnerability scans for the office "Tests" page. Each saved project
// (internal/projects) can be a Go, Node, or Python repo; the language is
// detected at runtime and the matching tool is invoked in the project
// directory. Coverage runs the project's own test suite (best-effort per
// language); vulnerabilities are scanned with Trivy (which reads the dep
// manifests/lockfiles and never executes project code). Results are cached
// in the shared settings store so the UI shows last-known values instantly.
package quality

// Language markers.
const (
	LangGo     = "go"
	LangNode   = "node"
	LangPython = "python"
)

// Job/result statuses surfaced to the UI per project.
const (
	StatusIdle        = "idle"        // never run
	StatusRunning     = "running"     // a job is in flight
	StatusOK          = "ok"          // completed with a value
	StatusFailed      = "failed"      // tool ran but errored / unparseable
	StatusUnsupported = "unsupported" // no recognized toolchain for this project
)

// CoverageResult is the per-project coverage outcome.
type CoverageResult struct {
	Language  string   `json:"language"`
	Pct       *float64 `json:"pct"` // nil unless Status == ok
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
	LastRunAt string   `json:"lastRunAt,omitempty"` // RFC3339
}

// VulnResult is the per-project vulnerability scan outcome.
type VulnResult struct {
	Critical   int    `json:"critical"`
	High       int    `json:"high"`
	Medium     int    `json:"medium"`
	Low        int    `json:"low"`
	Unknown    int    `json:"unknown"`
	Total      int    `json:"total"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	LastScanAt string `json:"lastScanAt,omitempty"` // RFC3339
}

// ReactResult is the per-project react-doctor outcome (React projects only).
type ReactResult struct {
	Score     *int   `json:"score"`           // 0..100, nil unless Status == ok
	Label     string `json:"label,omitempty"` // react-doctor's "Great" / "Needs work" / "Critical"
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	LastRunAt string `json:"lastRunAt,omitempty"` // RFC3339
}

// ProjectResults bundles the outcomes for one project (the cache shape).
type ProjectResults struct {
	Coverage CoverageResult `json:"coverage"`
	Vuln     VulnResult     `json:"vuln"`
	React    ReactResult    `json:"react"`
}
