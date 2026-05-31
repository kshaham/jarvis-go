package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// faultyBody is a ReadCloser whose Read always fails, used to exercise the
// io.ReadAll error branch in doRequest.
type faultyBody struct{}

func (faultyBody) Read([]byte) (int, error) { return 0, errors.New("read failed mid-body") }
func (faultyBody) Close() error             { return nil }

// faultyTransport returns a 200 response whose body errors on read.
type faultyTransport struct{}

func (faultyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       faultyBody{},
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestDoRequest_BodyReadErrorSurfaces(t *testing.T) {
	prevURL := apiURL
	prevTransport := httpClient.Transport
	apiURL = "http://example.invalid"
	httpClient.Transport = faultyTransport{}
	defer func() {
		apiURL = prevURL
		httpClient.Transport = prevTransport
	}()

	_, err := apiGet("/api/x")
	if err == nil {
		t.Fatal("expected error when response body read fails")
	}
	if !strings.Contains(err.Error(), "failed to read response") {
		t.Fatalf("expected read-failure context, got %v", err)
	}
}

// ensure io import is used even if other references are removed.
var _ = io.Discard

func TestAttachAuth_SetsHeaderWhenTokenPresent(t *testing.T) {
	setEnv(t, "JARVIS_AGENT_TOKEN", "secret-token")
	req, err := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	attachAuth(req)
	if got := req.Header.Get("X-Jarvis-Agent-Token"); got != "secret-token" {
		t.Fatalf("expected token header, got %q", got)
	}
}

func TestAttachAuth_NoHeaderWhenTokenAbsent(t *testing.T) {
	setEnv(t, "JARVIS_AGENT_TOKEN", "")
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/x", nil)
	attachAuth(req)
	if got := req.Header.Get("X-Jarvis-Agent-Token"); got != "" {
		t.Fatalf("expected no token header, got %q", got)
	}
}

func TestDoRequest_GetSuccessJSONObject(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"hello":"world","n":3}`))
	setEnv(t, "JARVIS_AGENT_TOKEN", "tok")

	got, err := apiGet("/api/thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["hello"] != "world" {
		t.Fatalf("expected hello=world, got %v", m["hello"])
	}
	rec := cap.get()
	if rec.Method != http.MethodGet {
		t.Fatalf("expected GET, got %s", rec.Method)
	}
	if rec.Path != "/api/thing" {
		t.Fatalf("expected path /api/thing, got %s", rec.Path)
	}
	// Auth header should be attached.
	if rec.Headers.Get("X-Jarvis-Agent-Token") != "tok" {
		t.Fatalf("expected auth header to be forwarded")
	}
}

func TestDoRequest_EmptyBodyReturnsSuccessMap(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, ""))

	got, err := apiGet("/api/empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if m["success"] != true {
		t.Fatalf("expected success=true for empty body, got %v", m)
	}
}

func TestDoRequest_NonJSONBodyReturnedAsString(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 200, "plain text not json"))
	got, err := apiGet("/api/text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if s != "plain text not json" {
		t.Fatalf("unexpected string: %q", s)
	}
}

func TestDoRequest_ErrorStatusReturnsError(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 404, `{"error":"not found"}`))
	_, err := apiGet("/api/missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "API error 404") {
		t.Fatalf("expected API error 404 in message, got %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected body in error message, got %v", err)
	}
}

func TestDoRequest_500Error(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "boom"))
	_, err := apiGet("/api/boom")
	if err == nil || !strings.Contains(err.Error(), "API error 500") {
		t.Fatalf("expected API error 500, got %v", err)
	}
}

func TestDoRequest_TransportErrorWhenServerUnreachable(t *testing.T) {
	prev := apiURL
	// Point at a closed port to force a transport-level failure.
	apiURL = "http://127.0.0.1:1"
	defer func() { apiURL = prev }()

	_, err := apiGet("/api/x")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !strings.Contains(err.Error(), "HTTP GET") {
		t.Fatalf("expected HTTP GET failure context, got %v", err)
	}
}

func TestDoRequest_InvalidMethodCreatesRequestError(t *testing.T) {
	// An invalid HTTP method (containing a space) makes http.NewRequest fail.
	_, err := doRequest("BAD METHOD", "/p", nil, "")
	if err == nil {
		t.Fatal("expected request creation error")
	}
	if !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("expected creation error, got %v", err)
	}
}

func TestApiPost_SendsJSONBodyAndContentType(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"ok":true}`))

	payload := map[string]interface{}{"a": 1, "b": "two"}
	got, err := apiPost("/api/create", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m, _ := got.(map[string]interface{}); m["ok"] != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	rec := cap.get()
	if rec.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", rec.Method)
	}
	if rec.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("expected application/json content type, got %q", rec.Headers.Get("Content-Type"))
	}
	if !strings.Contains(rec.Body, `"b":"two"`) {
		t.Fatalf("expected JSON body to contain b:two, got %q", rec.Body)
	}
}

func TestApiPost_MarshalErrorOnUnsupportedPayload(t *testing.T) {
	// Channels cannot be marshaled to JSON.
	_, err := apiPost("/api/x", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "failed to marshal request") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestApiPut_SendsJSONBody(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"updated":true}`))

	got, err := apiPut("/api/update", map[string]interface{}{"x": "y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m, _ := got.(map[string]interface{}); m["updated"] != true {
		t.Fatalf("expected updated=true, got %v", got)
	}
	rec := cap.get()
	if rec.Method != http.MethodPut {
		t.Fatalf("expected PUT, got %s", rec.Method)
	}
	if !strings.Contains(rec.Body, `"x":"y"`) {
		t.Fatalf("expected body x:y, got %q", rec.Body)
	}
}

func TestApiPut_MarshalError(t *testing.T) {
	_, err := apiPut("/api/x", make(chan int))
	if err == nil || !strings.Contains(err.Error(), "failed to marshal request") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestApiDelete_SendsDeleteMethod(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `{"deleted":true}`))

	got, err := apiDelete("/api/del/123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m, _ := got.(map[string]interface{}); m["deleted"] != true {
		t.Fatalf("expected deleted=true, got %v", got)
	}
	rec := cap.get()
	if rec.Method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", rec.Method)
	}
	if rec.Path != "/api/del/123" {
		t.Fatalf("expected path /api/del/123, got %s", rec.Path)
	}
}

func TestApiGet_ReturnsJSONArray(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 200, `[{"id":"a"},{"id":"b"}]`))
	got, err := apiGet("/api/list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := got.([]interface{})
	if !ok {
		t.Fatalf("expected slice, got %T", got)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
}
