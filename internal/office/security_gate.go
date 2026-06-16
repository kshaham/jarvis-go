package office

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This file is the loop-engineering "security tax" (requirement I): an
// unattended loop is an unattended attack surface. Before the loop replenishes
// an idle agent with fresh work, a lightweight secret scan runs over the
// working tree; any committed-secret finding halts replenishment and surfaces
// office:securityIssueDetected. Task titles are redacted before they reach the
// logs so a long run can't scatter secrets across log files nobody monitors.

// secretPatterns matches high-signal credential shapes. Kept deliberately tight
// to avoid false positives that would cry wolf and get the gate ignored.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`), // AWS access key id
	// OpenAI secret keys. Two explicit shapes: the modern project/service-account/
	// admin prefixes (sk-proj-/sk-svcacct-/sk-admin- followed by an alnum body),
	// and the legacy alnum-only form. Deliberately NOT the broad
	// `sk-[alnum][alnum_-]{19,}` -- that also matched ordinary hyphenated names
	// (sk-learn-..., sk-service-key-...) and a single committed match HALTS the
	// whole replenishment loop, so a false positive here is expensive.
	regexp.MustCompile(`sk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),                               // GitHub personal access token
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                      // Slack token
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`), // private key block
	regexp.MustCompile(`(?i)aws_secret_access_key\s*[=:]\s*[A-Za-z0-9/+]{40}`),
}

// secretScanMaxFileBytes skips files larger than this (build artifacts, blobs).
const secretScanMaxFileBytes = 512 * 1024

// secretScanMaxFiles bounds the walk so a huge repo can't stall the monitor tick.
const secretScanMaxFiles = 2000

// SecurityFinding is one detected committed secret.
type SecurityFinding struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

// scanForSecrets walks workingDir for committed secrets, skipping VCS/dependency
// directories, large files, and obvious binaries. Bounded by secretScanMaxFiles.
func scanForSecrets(workingDir string) []SecurityFinding {
	if workingDir == "" {
		return nil
	}
	var findings []SecurityFinding
	seen := 0
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "dist": true,
		"build": true, ".next": true, "target": true, "__pycache__": true,
	}
	_ = filepath.WalkDir(workingDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if seen >= secretScanMaxFiles {
			return filepath.SkipAll
		}
		seen++
		info, ierr := d.Info()
		if ierr != nil || info.Size() > secretScanMaxFileBytes {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, re := range secretPatterns {
			if re.Match(data) {
				rel, _ := filepath.Rel(workingDir, path)
				findings = append(findings, SecurityFinding{Path: rel, Pattern: re.String()})
				break // one finding per file is enough
			}
		}
		return nil
	})
	return findings
}

// RedactSecrets replaces credential-shaped substrings with [REDACTED] so they
// don't leak into logs. Used to sanitize agent-controlled strings (task titles,
// descriptions) before they reach slog.
func RedactSecrets(s string) string {
	out := s
	for _, re := range secretPatterns {
		out = re.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

// maybeRunSecurityScan runs a bounded secret scan over the team working
// directory and halts replenishment if anything is found. Gated by a cooldown
// (securityScanCooldown) so the disk walk only fires a few times an hour. When
// findings exist it returns true (caller should suppress replenishment) and
// broadcasts office:securityIssueDetected exactly once per finding-set change.
func (m *Manager) maybeRunSecurityScan() (blocked bool) {
	m.mu.Lock()
	// The cooldown throttles the disk walk only while the tree is CLEAN. When
	// we're currently blocked, bypass the cooldown and re-scan every tick so
	// replenishment resumes as soon as the operator removes the secret --
	// otherwise a fixed tree stays paused for up to a full cooldown.
	if !m.securityBlocked && !m.lastSecurityScanAt.IsZero() && time.Since(m.lastSecurityScanAt) < securityScanCooldown {
		m.mu.Unlock()
		return false
	}
	m.lastSecurityScanAt = time.Now()
	workingDir := m.lastWorkingDir
	m.mu.Unlock()

	findings := scanForSecrets(workingDir)

	m.mu.Lock()
	was := m.securityBlocked
	m.securityBlocked = len(findings) > 0
	nowBlocked := m.securityBlocked
	m.mu.Unlock()

	if nowBlocked && !was {
		paths := make([]string, 0, len(findings))
		for _, f := range findings {
			paths = append(paths, f.Path)
		}
		slog.Warn("security scan halted replenishment: committed secrets detected",
			"count", len(findings), "files", strings.Join(paths, ", "))
		m.broadcast("office:securityIssueDetected", map[string]interface{}{
			"findings": findings,
		})
		m.NotifyOfficeEvent("Security issue detected",
			"Replenishment paused: committed secrets found in "+strings.Join(paths, ", "))
		if err := RecordEscalation(EscalationRecord{
			Reason: "security_issue",
			Detail: "committed secrets detected in: " + strings.Join(paths, ", "),
		}); err != nil {
			slog.Debug("record security escalation failed", "err", err)
		}
	}
	return nowBlocked
}

// securityScanCooldown gates the secret scan so the disk walk runs a few times
// an hour at most, not on every 15s idle tick.
const securityScanCooldown = 15 * time.Minute
