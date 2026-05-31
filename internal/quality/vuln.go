package quality

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

// trivyReport is the subset of `trivy fs --format json` we care about.
type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

// RunVulnScan scans dir's dependency manifests/lockfiles with Trivy and
// returns severity-bucketed counts. Trivy reads files only (it never builds
// or runs project code), so this is safe and language-agnostic. Callers
// should bound it with a context timeout (the first run also downloads the
// vuln DB).
func RunVulnScan(ctx context.Context, dir string) VulnResult {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return VulnResult{Status: StatusFailed, Error: "project path not found: " + dir}
	}
	cmd := exec.CommandContext(ctx, "trivy", "fs",
		"--quiet", "--scanners", "vuln", "--format", "json", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return VulnResult{Status: StatusFailed, Error: "trivy: " + tail(out)}
	}
	var report trivyReport
	if err := json.Unmarshal(out, &report); err != nil {
		return VulnResult{Status: StatusFailed, Error: "parse trivy json: " + err.Error()}
	}
	res := VulnResult{Status: StatusOK}
	for _, r := range report.Results {
		for _, v := range r.Vulnerabilities {
			switch strings.ToUpper(v.Severity) {
			case "CRITICAL":
				res.Critical++
			case "HIGH":
				res.High++
			case "MEDIUM":
				res.Medium++
			case "LOW":
				res.Low++
			default:
				res.Unknown++
			}
			res.Total++
		}
	}
	return res
}
