package handler

// Happy-path coverage for GetRalphWizardFiles / GetRalphWizardFile by seeding
// a case's ralph-wizard/<agent>/ directory with prompt.md + result.json.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kshaham/jarvis/internal/cases"
)

func seedWizard(t *testing.T, casePath string) {
	t.Helper()
	agentDir := filepath.Join(casePath, "ralph-wizard", "planner")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir wizard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "prompt.md"), []byte("# Plan\nstep one"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "result.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write result: %v", err)
	}
}

func TestGetRalphWizardFiles_Happy(t *testing.T) {
	cm := cases.NewCaseManager(t.TempDir())
	info, err := cm.Create("wiz")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	seedWizard(t, info.Path)

	ctx := &Context{CaseManager: cm}
	req := withURLParam(httptest.NewRequest("GET", "/api/cases/wiz/ralph-wizard/files", nil), "caseName", "wiz")
	rec := httptest.NewRecorder()
	ctx.GetRalphWizardFiles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wizard files: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	data, _ := resp["data"].(map[string]interface{})
	files, _ := data["files"].([]interface{})
	if len(files) != 1 {
		t.Fatalf("expected 1 wizard agent entry, got %d", len(files))
	}
}

func TestGetRalphWizardFile_TextAndJSON(t *testing.T) {
	cm := cases.NewCaseManager(t.TempDir())
	info, err := cm.Create("wiz2")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	seedWizard(t, info.Path)
	ctx := &Context{CaseManager: cm}

	// Text file (prompt.md).
	recMD := httptest.NewRecorder()
	ctx.GetRalphWizardFile(recMD, twoParamReq("GET", "/api/cases/wiz2/ralph-wizard/file/planner/prompt.md", "", "caseName", "wiz2", "*", "planner/prompt.md"))
	if recMD.Code != http.StatusOK {
		t.Fatalf("wizard prompt.md: expected 200, got %d: %s", recMD.Code, recMD.Body.String())
	}
	var respMD map[string]interface{}
	_ = json.Unmarshal(recMD.Body.Bytes(), &respMD)
	dataMD, _ := respMD["data"].(map[string]interface{})
	if dataMD["isJson"] != false {
		t.Errorf("prompt.md isJson=%v, want false", dataMD["isJson"])
	}

	// JSON file (result.json) -> parsed populated.
	recJSON := httptest.NewRecorder()
	ctx.GetRalphWizardFile(recJSON, twoParamReq("GET", "/api/cases/wiz2/ralph-wizard/file/planner/result.json", "", "caseName", "wiz2", "*", "planner/result.json"))
	if recJSON.Code != http.StatusOK {
		t.Fatalf("wizard result.json: expected 200, got %d", recJSON.Code)
	}
	var respJSON map[string]interface{}
	_ = json.Unmarshal(recJSON.Body.Bytes(), &respJSON)
	dataJSON, _ := respJSON["data"].(map[string]interface{})
	if dataJSON["isJson"] != true {
		t.Errorf("result.json isJson=%v, want true", dataJSON["isJson"])
	}
}
