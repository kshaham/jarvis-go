package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// goTotalRe matches the trailing "total:" line of `go tool cover -func`,
// e.g. "total:\t(statements)\t87.2%".
var goTotalRe = regexp.MustCompile(`(?m)^total:\s+\(statements\)\s+([0-9.]+)%`)

// RunCoverage detects the language and runs the matching test+coverage
// command in dir, returning the overall percentage. It executes the
// project's own test code, so callers MUST bound it with a context timeout.
func RunCoverage(ctx context.Context, dir string) CoverageResult {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return CoverageResult{Status: StatusFailed, Error: "project path not found: " + dir}
	}
	switch DetectLanguage(dir) {
	case LangGo:
		return runGoCoverage(ctx, dir)
	case LangNode:
		return runNodeCoverage(ctx, dir)
	case LangPython:
		return runPythonCoverage(ctx, dir)
	default:
		return CoverageResult{
			Status: StatusUnsupported,
			Error:  "no recognized project markers (go.mod / package.json / pyproject.toml)",
		}
	}
}

func runGoCoverage(ctx context.Context, dir string) CoverageResult {
	res := CoverageResult{Language: LangGo}
	prof, err := os.CreateTemp("", "jarvis-cover-*.out")
	if err != nil {
		return failf(res, "create coverage temp: %v", err)
	}
	profPath := prof.Name()
	_ = prof.Close()
	defer func() { _ = os.Remove(profPath) }()

	run := exec.CommandContext(ctx, "go", "test", "-covermode=set", "-coverprofile="+profPath, "./...")
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		return failf(res, "go test failed: %v\n%s", err, tail(out))
	}
	sum := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+profPath)
	sum.Dir = dir
	out, err := sum.CombinedOutput()
	if err != nil {
		return failf(res, "go tool cover failed: %v\n%s", err, tail(out))
	}
	m := goTotalRe.FindSubmatch(out)
	if m == nil {
		return failf(res, "could not parse coverage total from go tool cover output")
	}
	pct, _ := strconv.ParseFloat(string(m[1]), 64)
	res.Pct = &pct
	res.Status = StatusOK
	return res
}

func runNodeCoverage(ctx context.Context, dir string) CoverageResult {
	res := CoverageResult{Language: LangNode}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		return failf(res, "dependencies not installed (run npm install first)")
	}
	pkg := readPackageJSON(dir)
	if pkg == nil {
		return failf(res, "could not read package.json")
	}
	var cmd *exec.Cmd
	switch {
	case pkg.hasDep("vitest"):
		cmd = exec.CommandContext(ctx, "npx", "--no-install", "vitest", "run",
			"--coverage", "--coverage.reporter=json-summary", "--coverage.reportsDirectory=coverage")
	case pkg.hasDep("jest"):
		cmd = exec.CommandContext(ctx, "npx", "--no-install", "jest",
			"--coverage", "--coverageReporters=json-summary")
	default:
		return CoverageResult{Language: LangNode, Status: StatusUnsupported,
			Error: "no jest or vitest found in package.json"}
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=true")
	// Test failures exit non-zero but the coverage summary is still written,
	// so parse the artifact regardless and only fail if it's absent.
	out, runErr := cmd.CombinedOutput()
	pct, err := parseJSONSummary(filepath.Join(dir, "coverage", "coverage-summary.json"))
	if err != nil {
		return failf(res, "coverage summary not produced: %v\n%s", err, tail(out))
	}
	_ = runErr
	res.Pct = &pct
	res.Status = StatusOK
	return res
}

func runPythonCoverage(ctx context.Context, dir string) CoverageResult {
	res := CoverageResult{Language: LangPython}
	tmp, err := os.CreateTemp("", "jarvis-pycov-*.json")
	if err != nil {
		return failf(res, "create coverage temp: %v", err)
	}
	covPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(covPath) }()

	cmd := exec.CommandContext(ctx, "pytest", "--cov=.", "--cov-report=json:"+covPath, "-q")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput() // pytest exits non-zero on test failures; report is still written
	pct, err := parsePyCovJSON(covPath)
	if err != nil {
		return failf(res, "pytest --cov produced no report: %v\n%s", err, tail(out))
	}
	res.Pct = &pct
	res.Status = StatusOK
	return res
}

// ── parsing helpers ──────────────────────────────────────────────────────

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (p *packageJSON) hasDep(name string) bool {
	if _, ok := p.Dependencies[name]; ok {
		return true
	}
	_, ok := p.DevDependencies[name]
	return ok
}

func readPackageJSON(dir string) *packageJSON {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var p packageJSON
	if err := json.Unmarshal(b, &p); err != nil {
		return nil
	}
	return &p
}

// parseJSONSummary reads Istanbul's coverage-summary.json (jest/vitest) and
// returns total.lines.pct.
func parseJSONSummary(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Total struct {
			Lines struct {
				Pct float64 `json:"pct"`
			} `json:"lines"`
		} `json:"total"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0, err
	}
	return doc.Total.Lines.Pct, nil
}

// parsePyCovJSON reads coverage.py's JSON report and returns
// totals.percent_covered.
func parsePyCovJSON(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Totals struct {
			PercentCovered float64 `json:"percent_covered"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0, err
	}
	return doc.Totals.PercentCovered, nil
}

func failf(res CoverageResult, format string, args ...interface{}) CoverageResult {
	res.Status = StatusFailed
	res.Error = fmt.Sprintf(format, args...)
	return res
}

// tail returns the last ~600 bytes of command output for an error message,
// trimmed to whole lines so the UI shows the relevant failure tail.
func tail(out []byte) string {
	const max = 600
	s := strings.TrimSpace(string(out))
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
