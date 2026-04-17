package request

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteCodedError_PopulatesCode(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteCodedError(rr, http.StatusTooManyRequests, ErrCodeSpendingFrozen, "cap reached")

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != ErrCodeSpendingFrozen {
		t.Errorf("expected code %q, got %q", ErrCodeSpendingFrozen, resp.Code)
	}
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Error != "cap reached" {
		t.Errorf("unexpected message: %q", resp.Error)
	}
}

func TestWriteError_AutoPopulatesCode(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, http.StatusNotFound, "missing")
	var resp ErrorResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Code != ErrCodeNotFound {
		t.Errorf("expected auto-populated not_found code, got %q", resp.Code)
	}
}

func TestCodeForStatus_UnknownReturnsEmpty(t *testing.T) {
	if c := codeForStatus(418); c != "" {
		t.Errorf("expected empty for unmapped status, got %q", c)
	}
}
