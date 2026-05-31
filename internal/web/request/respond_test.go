package request

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON_EncodesDataAndStatus(t *testing.T) {
	rr := httptest.NewRecorder()
	payload := map[string]any{"hello": "world", "n": float64(42)}

	WriteJSON(rr, http.StatusCreated, payload)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json content type, got %q", ct)
	}
	var decoded map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["hello"] != "world" {
		t.Errorf("expected hello=world, got %v", decoded["hello"])
	}
	if decoded["n"] != float64(42) {
		t.Errorf("expected n=42, got %v", decoded["n"])
	}
}

func TestWriteJSON_NilData(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, http.StatusOK, nil)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	// json.Encode(nil) writes the literal null\n.
	if got := rr.Body.String(); got != "null\n" {
		t.Errorf("expected 'null', got %q", got)
	}
}

func TestWriteSuccess_WrapsDataWithSuccessTrue(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteSuccess(rr, map[string]string{"id": "abc"})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var resp SuccessResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", resp.Data)
	}
	if data["id"] != "abc" {
		t.Errorf("expected data.id=abc, got %v", data["id"])
	}
}

func TestWriteSuccess_NilDataOmitsDataField(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteSuccess(rr, nil)

	// Data has omitempty; with nil it should be absent from the raw JSON.
	raw := rr.Body.String()
	if got := raw; got == "" {
		t.Fatal("expected non-empty body")
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if generic["success"] != true {
		t.Errorf("expected success=true, got %v", generic["success"])
	}
	if _, present := generic["data"]; present {
		t.Errorf("expected data field omitted when nil, raw=%q", raw)
	}
}

// failingWriter implements http.ResponseWriter but always fails on Write,
// so we can exercise the slog.Error fallback branches in WriteCodedError /
// WriteJSON without crashing.
type failingWriter struct {
	header http.Header
	status int
}

func newFailingWriter() *failingWriter {
	return &failingWriter{header: make(http.Header)}
}

func (f *failingWriter) Header() http.Header { return f.header }

func (f *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingWriter) WriteHeader(status int) { f.status = status }

func TestWriteCodedError_WriteFailureLogsAndDoesNotPanic(t *testing.T) {
	fw := newFailingWriter()
	// Must not panic even though the encoder's Write returns an error.
	WriteCodedError(fw, http.StatusInternalServerError, ErrCodeInternal, "boom")

	if fw.status != http.StatusInternalServerError {
		t.Errorf("expected status header 500 to be set, got %d", fw.status)
	}
	if ct := fw.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected content type set before write, got %q", ct)
	}
}

func TestWriteJSON_WriteFailureLogsAndDoesNotPanic(t *testing.T) {
	fw := newFailingWriter()
	WriteJSON(fw, http.StatusOK, map[string]string{"k": "v"})

	if fw.status != http.StatusOK {
		t.Errorf("expected status header 200 to be set, got %d", fw.status)
	}
}

// TestCodeForStatus_AllMappedStatuses drives every switch arm in
// codeForStatus so the mapping is locked in and fully covered.
func TestCodeForStatus_AllMappedStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, ErrCodeInvalidInput},
		{http.StatusNotFound, ErrCodeNotFound},
		{http.StatusUnauthorized, ErrCodeUnauthorized},
		{http.StatusForbidden, ErrCodeForbidden},
		{http.StatusConflict, ErrCodeConflict},
		{http.StatusRequestEntityTooLarge, ErrCodeTooLarge},
		{http.StatusTooManyRequests, ErrCodeTooManyReq},
		{http.StatusInternalServerError, ErrCodeInternal},
	}
	for _, tc := range cases {
		if got := codeForStatus(tc.status); got != tc.want {
			t.Errorf("codeForStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestWriteError_MapsEachStatusToCode confirms WriteError end-to-end picks
// the right code via codeForStatus for representative statuses.
func TestWriteError_MapsEachStatusToCode(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, ErrCodeInvalidInput},
		{http.StatusForbidden, ErrCodeForbidden},
		{http.StatusConflict, ErrCodeConflict},
		{http.StatusRequestEntityTooLarge, ErrCodeTooLarge},
		{http.StatusInternalServerError, ErrCodeInternal},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		WriteError(rr, tc.status, "msg")
		if rr.Code != tc.status {
			t.Errorf("status %d: recorder code = %d", tc.status, rr.Code)
		}
		var resp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("status %d decode: %v", tc.status, err)
		}
		if resp.Code != tc.want {
			t.Errorf("status %d: code = %q, want %q", tc.status, resp.Code, tc.want)
		}
		if resp.Error != "msg" {
			t.Errorf("status %d: error = %q, want msg", tc.status, resp.Error)
		}
	}
}

func TestWriteError_UnmappedStatusEmptyCode(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, http.StatusTeapot, "teapot")
	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != "" {
		t.Errorf("expected empty code for unmapped status, got %q", resp.Code)
	}
	if resp.Error != "teapot" {
		t.Errorf("expected message preserved, got %q", resp.Error)
	}
}
