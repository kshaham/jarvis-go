package handler

// Round-4 fills:
//   - MaskSensitiveSettings: the openaiApiKey + geminiApiKey + claudeApiKey mask
//     arms (config.go:119-130) the existing zai-only test doesn't reach.
//   - CreateSettingsAccount: the bad-JSON 400 arm (226-229).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaskSensitiveSettings_AllProviderKeys(t *testing.T) {
	out := MaskSensitiveSettings(map[string]interface{}{
		"claudeApiKey": "sk-ant-claudekeyvalue1234",
		"openaiApiKey": "sk-openaikeyvalue567890ab",
		"geminiApiKey": "AIzaGeminikeyvalueABCDEF",
		"plain":        "visible",
	})
	for _, k := range []string{"claudeApiKey", "openaiApiKey", "geminiApiKey"} {
		masked, _ := out[k].(string)
		if masked == "" {
			t.Errorf("%s should be present and masked, got empty", k)
		}
		if !strings.Contains(masked, "*") {
			t.Errorf("%s should contain mask chars, got %q", k, masked)
		}
	}
	if out["plain"] != "visible" {
		t.Errorf("plain should pass through, got %v", out["plain"])
	}
}

func TestCreateSettingsAccount_BadJSON400(t *testing.T) {
	ctx := newOfficeExtCtx(t)
	req := httptest.NewRequest("POST", "/api/settings/accounts", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx.CreateSettingsAccount(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}
