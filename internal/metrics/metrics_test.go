package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ExposesCustomMetrics(t *testing.T) {
	// Touch a few counters/gauges so they appear in the output.
	SessionsCreated.Inc()
	SSEClients.Set(3)
	SessionsStopped.WithLabelValues("user").Inc()
	CostUSD.WithLabelValues("claude-sonnet-4-6").Add(0.0123)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	Handler().ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	mustContain := []string{
		"jarvis_sessions_created_total",
		"jarvis_sessions_stopped_total",
		"jarvis_sse_clients",
		"jarvis_cost_usd_total",
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
