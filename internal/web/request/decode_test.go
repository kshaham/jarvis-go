package request

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// validatableReq implements Validator so we can exercise the validation
// branch of DecodeAndValidate without depending on production request types.
type validatableReq struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func (v validatableReq) Validate() error {
	if v.Name == "" {
		return errors.New("name is required")
	}
	if v.Age < 0 {
		return errors.New("age must be non-negative")
	}
	return nil
}

// plainReq does NOT implement Validator; used to confirm the validation
// branch is skipped for non-Validator destinations.
type plainReq struct {
	Field string `json:"field"`
}

// decodeBody is a small helper to read+decode the recorder body as an
// ErrorResponse. Named uniquely to avoid clashing with errors_test.go.
func decodeErrorBody(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v (raw=%q)", err, rr.Body.String())
	}
	return resp
}

func newJSONRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
}

func TestDecodeAndValidate_ValidPayload(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"name":"alice","age":30}`)

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if !ok {
		t.Fatalf("expected ok=true, got false; body=%q", rr.Body.String())
	}
	if dst.Name != "alice" {
		t.Errorf("expected Name=alice, got %q", dst.Name)
	}
	if dst.Age != 30 {
		t.Errorf("expected Age=30, got %d", dst.Age)
	}
	// On success nothing should have been written to the response.
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty response body on success, got %q", rr.Body.String())
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected default 200 status (untouched), got %d", rr.Code)
	}
}

func TestDecodeAndValidate_NonValidatorDestination(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"field":"value"}`)

	var dst plainReq
	ok := DecodeAndValidate(rr, r, &dst)

	if !ok {
		t.Fatalf("expected ok=true for non-validator dst, got false; body=%q", rr.Body.String())
	}
	if dst.Field != "value" {
		t.Errorf("expected Field=value, got %q", dst.Field)
	}
}

func TestDecodeAndValidate_MalformedJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"name":"alice",`) // truncated/malformed

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false for malformed JSON")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Code != ErrCodeInvalidInput {
		t.Errorf("expected code %q, got %q", ErrCodeInvalidInput, resp.Code)
	}
	if !strings.Contains(resp.Error, "invalid JSON body") {
		t.Errorf("expected 'invalid JSON body' prefix, got %q", resp.Error)
	}
}

func TestDecodeAndValidate_TypeMismatchJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	// age is a string, dst expects an int -> json decode type error (not MaxBytes).
	r := newJSONRequest(`{"name":"alice","age":"oops"}`)

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false for type-mismatched JSON")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if !strings.Contains(resp.Error, "invalid JSON body") {
		t.Errorf("expected invalid JSON body message, got %q", resp.Error)
	}
}

func TestDecodeAndValidate_EmptyBodyIsEOF(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(``) // empty body -> io.EOF from decoder

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false for empty body")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if !strings.Contains(resp.Error, "invalid JSON body") {
		t.Errorf("expected invalid JSON body message, got %q", resp.Error)
	}
}

func TestDecodeAndValidate_ValidationFails_MissingField(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"age":21}`) // name missing -> Validate() error

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false when Validate() fails")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if resp.Error != "name is required" {
		t.Errorf("expected validation message verbatim, got %q", resp.Error)
	}
	if resp.Code != ErrCodeInvalidInput {
		t.Errorf("expected code %q, got %q", ErrCodeInvalidInput, resp.Code)
	}
}

func TestDecodeAndValidate_ValidationFails_BoundaryValue(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"name":"bob","age":-1}`) // negative age -> Validate error

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false for boundary-failing value")
	}
	resp := decodeErrorBody(t, rr)
	if resp.Error != "age must be non-negative" {
		t.Errorf("unexpected validation message: %q", resp.Error)
	}
}

func TestDecodeAndValidate_ValidationPasses_BoundaryZero(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(`{"name":"bob","age":0}`) // age==0 is the inclusive boundary

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if !ok {
		t.Fatalf("expected ok=true at boundary age=0, got false; body=%q", rr.Body.String())
	}
	if dst.Age != 0 {
		t.Errorf("expected Age=0, got %d", dst.Age)
	}
}

func TestDecodeAndValidateMax_OversizedBody(t *testing.T) {
	rr := httptest.NewRecorder()
	// Build a JSON string field comfortably larger than the cap.
	big := strings.Repeat("x", 64)
	r := newJSONRequest(`{"name":"` + big + `"}`)

	var dst validatableReq
	ok := DecodeAndValidateMax(rr, r, &dst, 16) // cap below body size

	if ok {
		t.Fatal("expected ok=false for oversized body")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if resp.Error != "request body too large" {
		t.Errorf("expected oversized message, got %q", resp.Error)
	}
	if resp.Code != ErrCodeTooLarge {
		t.Errorf("expected code %q, got %q", ErrCodeTooLarge, resp.Code)
	}
}

func TestDecodeAndValidateMax_ExactlyAtCapSucceeds(t *testing.T) {
	rr := httptest.NewRecorder()
	body := `{"name":"ab","age":1}`
	r := newJSONRequest(body)

	var dst validatableReq
	// cap exactly the body length -> should decode fine.
	ok := DecodeAndValidateMax(rr, r, &dst, int64(len(body)))

	if !ok {
		t.Fatalf("expected ok=true when body is exactly at cap, got false; body=%q", rr.Body.String())
	}
	if dst.Name != "ab" || dst.Age != 1 {
		t.Errorf("unexpected decoded values: %+v", dst)
	}
}

func TestDecodeAndValidateMax_CustomLargeCapAllowsBigBody(t *testing.T) {
	rr := httptest.NewRecorder()
	big := strings.Repeat("y", 4096)
	body := `{"name":"` + big + `","age":2}`
	r := newJSONRequest(body)

	var dst validatableReq
	ok := DecodeAndValidateMax(rr, r, &dst, 1<<20)

	if !ok {
		t.Fatalf("expected ok=true with large cap, got false; body=%q", rr.Body.String())
	}
	if len(dst.Name) != 4096 {
		t.Errorf("expected 4096-char name, got %d chars", len(dst.Name))
	}
}

// errReader returns an error partway through Read to drive a non-MaxBytes,
// non-syntax decode failure (an underlying transport read error).
type errReader struct {
	data    []byte
	pos     int
	failAt  int
	readErr error
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.pos >= e.failAt {
		return 0, e.readErr
	}
	n := copy(p, e.data[e.pos:e.failAt])
	e.pos += n
	return n, nil
}

func (e *errReader) Close() error { return nil }

func TestDecodeAndValidate_BodyReadError(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newJSONRequest(``)
	// Replace the body with a reader that errors mid-stream. This surfaces
	// as a generic decode error (400), not a MaxBytes error.
	r.Body = &errReader{
		data:    []byte(`{"name":"alice"`),
		failAt:  5,
		readErr: io.ErrUnexpectedEOF,
	}

	var dst validatableReq
	ok := DecodeAndValidate(rr, r, &dst)

	if ok {
		t.Fatal("expected ok=false on body read error")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on read error, got %d", rr.Code)
	}
	resp := decodeErrorBody(t, rr)
	if !strings.Contains(resp.Error, "invalid JSON body") {
		t.Errorf("expected invalid JSON body message, got %q", resp.Error)
	}
}
