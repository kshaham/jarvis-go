package quality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name    string
		markers []string
		want    string
	}{
		{"go", []string{"go.mod"}, LangGo},
		{"node", []string{"package.json"}, LangNode},
		{"python pyproject", []string{"pyproject.toml"}, LangPython},
		{"python requirements", []string{"requirements.txt"}, LangPython},
		{"python setup.py", []string{"setup.py"}, LangPython},
		{"go wins over node", []string{"go.mod", "package.json"}, LangGo},
		{"node wins over python", []string{"package.json", "requirements.txt"}, LangNode},
		{"none", []string{"README.md"}, ""},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, m := range tt.markers {
				if err := os.WriteFile(filepath.Join(dir, m), []byte("x"), 0o644); err != nil {
					t.Fatalf("write marker %s: %v", m, err)
				}
			}
			if got := DetectLanguage(dir); got != tt.want {
				t.Errorf("DetectLanguage(%v) = %q, want %q", tt.markers, got, tt.want)
			}
		})
	}
}

func TestParseJSONSummary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "coverage-summary.json")
	if err := os.WriteFile(p, []byte(`{"total":{"lines":{"pct":83.4},"statements":{"pct":80}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseJSONSummary(p)
	if err != nil {
		t.Fatalf("parseJSONSummary: %v", err)
	}
	if got != 83.4 {
		t.Errorf("pct = %v, want 83.4", got)
	}
	if _, err := parseJSONSummary(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParsePyCovJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cov.json")
	if err := os.WriteFile(p, []byte(`{"totals":{"percent_covered":91.25,"num_statements":400}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parsePyCovJSON(p)
	if err != nil {
		t.Fatalf("parsePyCovJSON: %v", err)
	}
	if got != 91.25 {
		t.Errorf("pct = %v, want 91.25", got)
	}
}

func TestGoTotalRegex(t *testing.T) {
	out := "github.com/x/y/foo.go:10:\tFoo\t100.0%\n" +
		"total:\t\t\t\t\t(statements)\t87.3%\n"
	m := goTotalRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("regex did not match total line")
	}
	if m[1] != "87.3" {
		t.Errorf("captured %q, want 87.3", m[1])
	}
}

func TestPackageJSONHasDep(t *testing.T) {
	p := &packageJSON{
		Dependencies:    map[string]string{"react": "^19"},
		DevDependencies: map[string]string{"vitest": "^1"},
	}
	if !p.hasDep("react") {
		t.Error("expected react in deps")
	}
	if !p.hasDep("vitest") {
		t.Error("expected vitest in devDeps")
	}
	if p.hasDep("jest") {
		t.Error("jest should not be present")
	}
}

func TestRunCoverageUnsupported(t *testing.T) {
	dir := t.TempDir() // no markers
	res := RunCoverage(nil, dir)
	if res.Status != StatusUnsupported {
		t.Errorf("status = %q, want unsupported", res.Status)
	}
}

func TestRunCoverageMissingPath(t *testing.T) {
	res := RunCoverage(nil, "/does/not/exist/anywhere")
	if res.Status != StatusFailed {
		t.Errorf("status = %q, want failed", res.Status)
	}
}

func TestTrivyReportParsing(t *testing.T) {
	// Verify the severity bucketing logic against a synthetic report.
	report := trivyReport{}
	report.Results = []struct {
		Vulnerabilities []struct {
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
	}{
		{Vulnerabilities: []struct {
			Severity string `json:"Severity"`
		}{{"CRITICAL"}, {"HIGH"}, {"high"}, {"MEDIUM"}, {"LOW"}, {"WEIRD"}}},
	}
	res := VulnResult{Status: StatusOK}
	for _, r := range report.Results {
		for _, v := range r.Vulnerabilities {
			switch toUpper(v.Severity) {
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
	if res.Critical != 1 || res.High != 2 || res.Medium != 1 || res.Low != 1 || res.Unknown != 1 || res.Total != 6 {
		t.Errorf("bucketing wrong: %+v", res)
	}
}

// toUpper mirrors strings.ToUpper for the test's local bucketing copy.
func toUpper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}
