package office

// Coverage for the live-preview completion gate helpers on Manager:
// HasHealthyDevInstance (any announced slot with no health failures) and
// MissionLooksLikeWebApp (conservative web-app detection).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasHealthyDevInstance(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Manager)
		want  bool
	}{
		{
			name:  "no instances returns false",
			setup: func(m *Manager) {},
			want:  false,
		},
		{
			name: "healthy instance returns true",
			setup: func(m *Manager) {
				if err := m.SetDevInstance("app", "http://jarvis:3001", 1234, true); err != nil {
					t.Fatalf("SetDevInstance: %v", err)
				}
			},
			want: true,
		},
		{
			name: "unhealthy instance returns false",
			setup: func(m *Manager) {
				if err := m.SetDevInstance("app", "http://jarvis:3001", 1234, true); err != nil {
					t.Fatalf("SetDevInstance: %v", err)
				}
				m.mu.Lock()
				m.teamDevInstances["app"].HealthFailures = 2
				m.mu.Unlock()
			},
			want: false,
		},
		{
			name: "announced URL empty returns false",
			setup: func(m *Manager) {
				m.mu.Lock()
				if m.teamDevInstances == nil {
					m.teamDevInstances = map[string]*devInstance{}
				}
				m.teamDevInstances["app"] = &devInstance{URL: ""}
				m.mu.Unlock()
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			m := newTestManager()
			tt.setup(m)

			// Act
			got := m.HasHealthyDevInstance()

			// Assert
			if got != tt.want {
				t.Errorf("HasHealthyDevInstance()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissionLooksLikeWebApp(t *testing.T) {
	tests := []struct {
		name      string
		mission   string
		makeFiles []string // files to create in the working dir
		want      bool
	}{
		{
			name:    "web keyword in mission returns true",
			mission: "Build a marketing dashboard with React",
			want:    true,
		},
		{
			name:    "non-web mission returns false",
			mission: "Refactor the data pipeline parser library",
			want:    false,
		},
		{
			name:    "empty mission returns false",
			mission: "",
			want:    false,
		},
		{
			name:      "package.json in working dir returns true",
			mission:   "Improve the CLI tool",
			makeFiles: []string{"package.json"},
			want:      true,
		},
		{
			name:      "index.html in working dir returns true",
			mission:   "Improve the CLI tool",
			makeFiles: []string{"index.html"},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			m := newTestManager()
			workingDir := t.TempDir()
			for _, f := range tt.makeFiles {
				if err := os.WriteFile(filepath.Join(workingDir, f), []byte("{}"), 0o644); err != nil {
					t.Fatalf("write %s: %v", f, err)
				}
			}
			m.SetSpawnContext("tmpl", workingDir)
			if tt.mission != "" {
				m.mu.Lock()
				m.teamMission = tt.mission
				m.mu.Unlock()
			}

			// Act
			got := m.MissionLooksLikeWebApp()

			// Assert
			if got != tt.want {
				t.Errorf("MissionLooksLikeWebApp()=%v, want %v", got, tt.want)
			}
		})
	}
}
