package quality

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kshaham/jarvis/internal/projects"
	"github.com/kshaham/jarvis/internal/safego"
)

// settingsKey stores the per-project results cache in the shared settings.
const settingsKey = "qualityResults"

// Bounds on the external tools. Coverage runs a whole test suite so it gets
// the most headroom; a Trivy scan is faster but the first run downloads the
// vuln DB.
const (
	coverageTimeout = 8 * time.Minute
	scanTimeout     = 5 * time.Minute
	reactTimeout    = 5 * time.Minute
)

// SSE event names (mirrored in the SPA's sse.ts).
const (
	EventCoverageComplete = "tests:coverageComplete"
	EventScanComplete     = "tests:scanComplete"
	EventCoverageProgress = "tests:coverageProgress"
	EventScanProgress     = "tests:scanProgress"
	EventReactComplete    = "tests:reactComplete"
	EventReactProgress    = "tests:reactProgress"
)

// SettingsStore is the minimal settings surface (same shape projects uses);
// declared here so this package imports nothing from internal/state.
type SettingsStore interface {
	GetSettings() map[string]interface{}
	UpdateSettings(updates map[string]interface{})
}

// Broadcaster is the SSE surface (satisfied by *sse.Broker).
type Broadcaster interface {
	Broadcast(event string, data interface{})
}

// ErrProjectNotFound is returned when a job is requested for an unknown id.
var ErrProjectNotFound = errors.New("project not found")

// Manager owns the coverage/scan jobs: it resolves saved projects, coalesces
// duplicate in-flight runs, executes the tools off the request goroutine, and
// caches + broadcasts results.
type Manager struct {
	projects *projects.Service
	store    SettingsStore
	bus      Broadcaster
	root     context.Context
	running  sync.Map // jobKey ("cov:<id>" / "vuln:<id>") -> struct{}
}

// NewManager wires the dependencies. root bounds every spawned job's lifetime
// to the daemon's lifetime.
func NewManager(root context.Context, p *projects.Service, store SettingsStore, bus Broadcaster) *Manager {
	if root == nil {
		root = context.Background()
	}
	return &Manager{projects: p, store: store, bus: bus, root: root}
}

// Snapshot returns the cached results for every project id, with the live
// "running" status overlaid for any job currently in flight.
func (m *Manager) Snapshot() map[string]ProjectResults {
	out := m.loadCache()
	if out == nil {
		out = map[string]ProjectResults{}
	}
	m.running.Range(func(k, _ any) bool {
		key, _ := k.(string)
		switch {
		case strings.HasPrefix(key, "cov:"):
			id := strings.TrimPrefix(key, "cov:")
			pr := out[id]
			pr.Coverage.Status = StatusRunning
			out[id] = pr
		case strings.HasPrefix(key, "vuln:"):
			id := strings.TrimPrefix(key, "vuln:")
			pr := out[id]
			pr.Vuln.Status = StatusRunning
			out[id] = pr
		case strings.HasPrefix(key, "react:"):
			id := strings.TrimPrefix(key, "react:")
			pr := out[id]
			pr.React.Status = StatusRunning
			out[id] = pr
		}
		return true
	})
	return out
}

// StartCoverage kicks off (or no-ops if already running) a coverage run for
// the project. Returns ErrProjectNotFound for unknown ids.
func (m *Manager) StartCoverage(projectID string) error {
	proj, ok := m.resolve(projectID)
	if !ok {
		return ErrProjectNotFound
	}
	key := "cov:" + projectID
	if _, loaded := m.running.LoadOrStore(key, struct{}{}); loaded {
		return nil // already running — idempotent
	}
	m.bus.Broadcast(EventCoverageProgress, map[string]any{"projectId": projectID, "status": StatusRunning})
	safego.Go("quality.coverage", func() {
		defer m.running.Delete(key)
		ctx, cancel := context.WithTimeout(m.root, coverageTimeout)
		defer cancel()
		res := runCoverageForProject(ctx, proj)
		res.LastRunAt = nowRFC3339()
		m.updateCache(projectID, func(pr *ProjectResults) { pr.Coverage = res })
		m.bus.Broadcast(EventCoverageComplete, map[string]any{"projectId": projectID, "coverage": res})
	})
	return nil
}

// StartScan kicks off (or no-ops if already running) a Trivy scan.
func (m *Manager) StartScan(projectID string) error {
	proj, ok := m.resolve(projectID)
	if !ok {
		return ErrProjectNotFound
	}
	key := "vuln:" + projectID
	if _, loaded := m.running.LoadOrStore(key, struct{}{}); loaded {
		return nil
	}
	m.bus.Broadcast(EventScanProgress, map[string]any{"projectId": projectID, "status": StatusRunning})
	safego.Go("quality.scan", func() {
		defer m.running.Delete(key)
		ctx, cancel := context.WithTimeout(m.root, scanTimeout)
		defer cancel()
		res := runScanForProject(ctx, proj)
		res.LastScanAt = nowRFC3339()
		m.updateCache(projectID, func(pr *ProjectResults) { pr.Vuln = res })
		m.bus.Broadcast(EventScanComplete, map[string]any{"projectId": projectID, "vuln": res})
	})
	return nil
}

// StartReact kicks off (or no-ops if already running) a react-doctor scan.
func (m *Manager) StartReact(projectID string) error {
	proj, ok := m.resolve(projectID)
	if !ok {
		return ErrProjectNotFound
	}
	key := "react:" + projectID
	if _, loaded := m.running.LoadOrStore(key, struct{}{}); loaded {
		return nil
	}
	m.bus.Broadcast(EventReactProgress, map[string]any{"projectId": projectID, "status": StatusRunning})
	safego.Go("quality.react", func() {
		defer m.running.Delete(key)
		ctx, cancel := context.WithTimeout(m.root, reactTimeout)
		defer cancel()
		res := runReactForProject(ctx, proj)
		res.LastRunAt = nowRFC3339()
		m.updateCache(projectID, func(pr *ProjectResults) { pr.React = res })
		m.bus.Broadcast(EventReactComplete, map[string]any{"projectId": projectID, "react": res})
	})
	return nil
}

// runCoverageForProject guards on a usable path before running.
func runCoverageForProject(ctx context.Context, p projects.Project) CoverageResult {
	if p.Path == "" {
		return CoverageResult{Status: StatusFailed, Error: "project has no local path yet (clone it first)"}
	}
	return RunCoverage(ctx, p.Path)
}

func runScanForProject(ctx context.Context, p projects.Project) VulnResult {
	if p.Path == "" {
		return VulnResult{Status: StatusFailed, Error: "project has no local path yet (clone it first)"}
	}
	return RunVulnScan(ctx, p.Path)
}

func runReactForProject(ctx context.Context, p projects.Project) ReactResult {
	if p.Path == "" {
		return ReactResult{Status: StatusFailed, Error: "project has no local path yet (clone it first)"}
	}
	return RunReactDoctor(ctx, p.Path)
}

func (m *Manager) resolve(id string) (projects.Project, bool) {
	for _, p := range m.projects.List() {
		if p.ID == id {
			return p, true
		}
	}
	return projects.Project{}, false
}

// loadCache decodes the results map, tolerating the []interface{}/map form a
// JSON round-trip through the on-disk state file produces (same trick as
// projects.load).
func (m *Manager) loadCache() map[string]ProjectResults {
	raw, ok := m.store.GetSettings()[settingsKey]
	if !ok || raw == nil {
		return map[string]ProjectResults{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return map[string]ProjectResults{}
	}
	out := map[string]ProjectResults{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]ProjectResults{}
	}
	return out
}

// updateCache applies mutate to the project's entry and persists the map.
func (m *Manager) updateCache(id string, mutate func(*ProjectResults)) {
	cache := m.loadCache()
	pr := cache[id]
	mutate(&pr)
	cache[id] = pr
	m.store.UpdateSettings(map[string]interface{}{settingsKey: cache})
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
