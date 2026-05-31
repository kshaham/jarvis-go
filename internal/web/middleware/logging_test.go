package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureSlog redirects the default slog logger to an in-memory buffer for the
// duration of the test and restores it afterward.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// decodeLogLines parses each JSON log line into a map.
func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func TestRequestLogger_LogsRequestWithFields(t *testing.T) {
	buf := captureSlog(t)

	// Chain RequestID in front so the logger sees a correlation ID from
	// context. The incoming header is honored, giving us a deterministic ID.
	h := RequestID(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))

	req := httptest.NewRequest("POST", "/api/sessions", nil)
	req.Header.Set("X-Request-ID", "req-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status not propagated: got %d", rec.Code)
	}

	lines := decodeLogLines(t, buf)
	if len(lines) == 0 {
		t.Fatal("expected a log line")
	}
	last := lines[len(lines)-1]
	if last["msg"] != "request" {
		t.Errorf("msg = %v, want request", last["msg"])
	}
	if last["method"] != "POST" {
		t.Errorf("method = %v", last["method"])
	}
	if last["path"] != "/api/sessions" {
		t.Errorf("path = %v", last["path"])
	}
	// JSON numbers decode as float64.
	if last["status"] != float64(http.StatusCreated) {
		t.Errorf("status = %v, want 201", last["status"])
	}
	if last["request_id"] != "req-123" {
		t.Errorf("request_id = %v", last["request_id"])
	}
	if _, ok := last["duration"]; !ok {
		t.Error("expected duration field")
	}
}

func TestRequestLogger_SSEPathLogsConnectionOnly(t *testing.T) {
	buf := captureSlog(t)

	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/events", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	lines := decodeLogLines(t, buf)
	if len(lines) == 0 {
		t.Fatal("expected a log line for SSE")
	}
	last := lines[len(lines)-1]
	if last["msg"] != "SSE connection" {
		t.Errorf("msg = %v, want 'SSE connection'", last["msg"])
	}
	// SSE branch must not include method/path/duration request fields.
	if _, ok := last["method"]; ok {
		t.Error("SSE log should not include method field")
	}
}

func TestRequestLogger_DefaultStatusIs200(t *testing.T) {
	buf := captureSlog(t)
	// Handler that never calls WriteHeader; recorder default is 200.
	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}))
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	lines := decodeLogLines(t, buf)
	last := lines[len(lines)-1]
	if last["status"] != float64(200) {
		t.Errorf("default status = %v, want 200", last["status"])
	}
}

func TestNormalizeRoute(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"short root", "/", "/"},
		{"two segments kept", "/api/status", "/api/status"},
		{"hex id collapsed", "/api/sessions/a1b2c3d4e5f6", "/api/sessions/:id"},
		{"numeric id collapsed", "/api/jobs/12345678", "/api/jobs/:id"},
		{"uuid collapsed", "/api/sessions/abcdef01-2345-6789-abcd-ef0123456789", "/api/sessions/:id"},
		{"short third segment kept", "/api/sessions/new", "/api/sessions/new"},
		{"multiple id segments", "/api/sessions/a1b2c3d4/files/deadbeef00", "/api/sessions/:id/files/:id"},
		{"non-id word kept", "/api/office/whiteboard", "/api/office/whiteboard"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRoute(tc.in); got != tc.want {
				t.Errorf("normalizeRoute(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeRoute_TruncatesLongPaths(t *testing.T) {
	// Build a deterministic long path of valid chars.
	long := "/api/sessions/"
	for i := 0; i < 200; i++ {
		long += "x"
	}
	got := normalizeRoute(long)
	if len(got) > 84 { // 80 cap + small slack from collapsing
		t.Errorf("expected truncation, got length %d", len(got))
	}
}

func TestSplitSegments(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"/", nil},
		{"/a", []string{"", "a"}},
		{"/a/b", []string{"", "a", "b"}},
		{"/a/b/c", []string{"", "a", "b", "c"}},
		{"/a/", []string{"", "a", ""}},
	}
	for _, tc := range tests {
		got := splitSegments(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitSegments(%q) len = %d, want %d (%v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitSegments(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestJoinSegments(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, "/"},
		{[]string{}, "/"},
		{[]string{""}, "/"},
		{[]string{"", "api", "status"}, "/api/status"},
		{[]string{"", "a", "b", "c"}, "/a/b/c"},
	}
	for _, tc := range tests {
		if got := joinSegments(tc.in); got != tc.want {
			t.Errorf("joinSegments(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitJoinRoundTrip(t *testing.T) {
	paths := []string{"/api/status", "/a/b/c", "/api/sessions/foo"}
	for _, p := range paths {
		if got := joinSegments(splitSegments(p)); got != p {
			t.Errorf("round trip %q -> %q", p, got)
		}
	}
}

func TestLooksLikeID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a1b2c3d4", true},                                // 8 hex chars
		{"a1b2c3d", false},                                // 7 chars (too short)
		{"12345678", true},                                // numeric
		{"deadbeefcafe", true},                            // long hex
		{"abcdef01-2345-6789-abcd-ef0123456789", true},    // uuid with dashes
		{"sessions", false},                               // letters beyond hex range
		{"new", false},                                    // short word
		{"GHIJKLMN", false},                               // non-hex letters
		{"A1B2C3D4", true},                                // uppercase hex
		{"", false},                                       // empty
	}
	for _, tc := range tests {
		if got := looksLikeID(tc.in); got != tc.want {
			t.Errorf("looksLikeID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestStatusRecorder_WriteHeaderAndFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, statusCode: 200}
	sr.WriteHeader(http.StatusTeapot)
	if sr.statusCode != http.StatusTeapot {
		t.Errorf("statusCode = %d, want 418", sr.statusCode)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("underlying recorder code = %d, want 418", rec.Code)
	}
	// httptest.ResponseRecorder implements http.Flusher, so Flush should
	// reach the underlying writer without panicking.
	sr.Flush()
	if !rec.Flushed {
		t.Error("expected underlying recorder to be flushed")
	}
}

func TestStatusRecorder_FlushNoFlusherIsNoop(t *testing.T) {
	// A ResponseWriter that does NOT implement http.Flusher must not panic.
	sr := &statusRecorder{ResponseWriter: nonFlusher{}, statusCode: 200}
	sr.Flush() // should be a no-op
}

// nonFlusher is a minimal ResponseWriter that does not implement http.Flusher.
type nonFlusher struct{}

func (nonFlusher) Header() http.Header        { return http.Header{} }
func (nonFlusher) Write(b []byte) (int, error) { return len(b), nil }
func (nonFlusher) WriteHeader(int)            {}
