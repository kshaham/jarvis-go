package quality

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
)

// IsReactProject reports whether dir is a React project (package.json declares a
// "react" dependency). react-doctor only applies to these.
func IsReactProject(dir string) bool {
	pkg := readPackageJSON(dir)
	return pkg != nil && pkg.hasDep("react")
}

// rdReport is the subset of `react-doctor --json` output we read. The 0..100
// health score + its label live under "summary" (react.doctor's score API
// fills them in; with --no-score they are null).
type rdReport struct {
	OK      bool `json:"ok"`
	Summary struct {
		Score      *int   `json:"score"`
		ScoreLabel string `json:"scoreLabel"`
	} `json:"summary"`
}

// RunReactDoctor runs `react-doctor --json` in dir and returns the health score.
// Non-React projects are Unsupported. Callers must bound it with a context
// timeout (a scan is usually fast once react-doctor is installed, but the score
// step calls react.doctor's API).
func RunReactDoctor(ctx context.Context, dir string) ReactResult {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ReactResult{Status: StatusFailed, Error: "project path not found: " + dir}
	}
	if !IsReactProject(dir) {
		return ReactResult{Status: StatusUnsupported, Error: "not a React project"}
	}
	cmd := exec.CommandContext(ctx, "react-doctor", "--yes", "--json", ".")
	cmd.Dir = dir
	out, err := cmd.Output() // --json writes the report to stdout; warnings go to stderr
	if err != nil && len(out) == 0 {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = tail(ee.Stderr)
		}
		return ReactResult{Status: StatusFailed, Error: "react-doctor: " + msg}
	}
	var rep rdReport
	if e := json.Unmarshal(out, &rep); e != nil {
		return ReactResult{Status: StatusFailed, Error: "parse react-doctor json: " + e.Error()}
	}
	if rep.Summary.Score == nil {
		return ReactResult{Status: StatusFailed, Error: "react-doctor returned no score (score service unavailable?)"}
	}
	return ReactResult{Score: rep.Summary.Score, Label: rep.Summary.ScoreLabel, Status: StatusOK}
}
