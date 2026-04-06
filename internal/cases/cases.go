package cases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CaseInfo describes a case directory and its contents.
type CaseInfo struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Description   string    `json:"description,omitempty"`
	CreatedAt     time.Time `json:"createdAt,omitempty"`
	HasInitPrompt bool      `json:"hasInitPrompt"`
	HasFixPlan    bool      `json:"hasFixPlan"`
}

// CaseManager manages case directories under a base directory.
type CaseManager struct {
	mu      sync.RWMutex
	baseDir string
}

// NewCaseManager creates a CaseManager rooted at baseDir, ensuring it exists.
func NewCaseManager(baseDir string) *CaseManager {
	os.MkdirAll(baseDir, 0755)
	return &CaseManager{baseDir: baseDir}
}

// List returns all cases sorted by name.
func (cm *CaseManager) List() []CaseInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	entries, err := os.ReadDir(cm.baseDir)
	if err != nil {
		return nil
	}
	var cases []CaseInfo
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		casePath := filepath.Join(cm.baseDir, e.Name())
		info := CaseInfo{
			Name:          e.Name(),
			Path:          casePath,
			HasInitPrompt: fileExists(filepath.Join(casePath, "initprompt.md")),
			HasFixPlan:    fileExists(filepath.Join(casePath, "@fix_plan.md")),
		}
		if fi, err := e.Info(); err == nil {
			info.CreatedAt = fi.ModTime()
		}
		// Read description from config.json if present.
		if data, err := os.ReadFile(filepath.Join(casePath, "config.json")); err == nil {
			var cfg map[string]interface{}
			if json.Unmarshal(data, &cfg) == nil {
				if desc, ok := cfg["description"].(string); ok {
					info.Description = desc
				}
			}
		}
		cases = append(cases, info)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases
}

// Get returns information about a single case by name.
func (cm *CaseManager) Get(name string) (*CaseInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	casePath := filepath.Join(cm.baseDir, name)
	if _, err := os.Stat(casePath); err != nil {
		return nil, err
	}
	info := &CaseInfo{
		Name:          name,
		Path:          casePath,
		HasInitPrompt: fileExists(filepath.Join(casePath, "initprompt.md")),
		HasFixPlan:    fileExists(filepath.Join(casePath, "@fix_plan.md")),
	}
	return info, nil
}

// Create creates a new case directory and returns its info.
func (cm *CaseManager) Create(name string) (*CaseInfo, error) {
	if !isValidCaseName(name) {
		return nil, fmt.Errorf("invalid case name: %q", name)
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	casePath := filepath.Join(cm.baseDir, name)
	if err := os.MkdirAll(casePath, 0755); err != nil {
		return nil, err
	}
	return &CaseInfo{Name: name, Path: casePath, CreatedAt: time.Now()}, nil
}

// Delete removes a case directory and all its contents.
func (cm *CaseManager) Delete(name string) error {
	if !isValidCaseName(name) {
		return fmt.Errorf("invalid case name: %q", name)
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	casePath := filepath.Join(cm.baseDir, name)
	// Verify resolved path is still under baseDir
	abs, _ := filepath.Abs(casePath)
	absBase, _ := filepath.Abs(cm.baseDir)
	if !strings.HasPrefix(abs, absBase+string(filepath.Separator)) {
		return fmt.Errorf("invalid case name: %q", name)
	}
	return os.RemoveAll(casePath)
}

// isValidCaseName checks that a case name is safe for use in file paths.
func isValidCaseName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return !strings.Contains(name, "..")
}

// Link creates a symbolic link from the cases directory to an existing folder.
// The name must match [a-zA-Z0-9_-]+. The target path must exist and be a directory.
// Returns the linked case info on success.
func (cm *CaseManager) Link(name, folderPath string) (*CaseInfo, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Validate the target path exists and is a directory
	info, err := os.Stat(folderPath)
	if err != nil {
		return nil, fmt.Errorf("folder not found: %s", folderPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Check if case name already exists
	casePath := filepath.Join(cm.baseDir, name)
	if _, err := os.Stat(casePath); err == nil {
		return nil, fmt.Errorf("a case with name %q already exists", name)
	}

	// Create symlink from cases dir to the target folder
	if err := os.Symlink(folderPath, casePath); err != nil {
		return nil, fmt.Errorf("failed to create symlink: %w", err)
	}

	return &CaseInfo{
		Name:          name,
		Path:          folderPath,
		HasInitPrompt: fileExists(filepath.Join(folderPath, "initprompt.md")),
		HasFixPlan:    fileExists(filepath.Join(folderPath, "@fix_plan.md")),
		CreatedAt:     time.Now(),
	}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
