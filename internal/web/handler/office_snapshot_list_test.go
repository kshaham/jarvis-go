package handler

// Coverage for OfficeListSnapshots + OfficeDeleteSnapshot. Reuses the
// seedSnapshotFile + newSnapshotCtx helpers from office_snapshot_test.go,
// which isolate ~/.jarvis/office-memory/snapshots into a temp HOME.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/office"
)

func TestOfficeListSnapshots_EmptyThenPopulated(t *testing.T) {
	ctx := newSnapshotCtx(t)

	// Empty case: returns an empty (non-null) array.
	req := httptest.NewRequest("GET", "/api/office/snapshots", nil)
	rec := httptest.NewRecorder()
	ctx.OfficeListSnapshots(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	snaps, _ := resp["snapshots"].([]interface{})
	if len(snaps) != 0 {
		t.Errorf("expected empty snapshots, got %d", len(snaps))
	}

	// Seed one snapshot file, then list again.
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:      "release-cut",
		CreatedAt: time.Now(),
		Members:   []office.SnapshotMemberEntry{{ConfigID: "vex", Config: office.TeamMemberConfig{ID: "vex", Name: "Vex"}}},
	})
	rec2 := httptest.NewRecorder()
	ctx.OfficeListSnapshots(rec2, httptest.NewRequest("GET", "/api/office/snapshots", nil))
	var resp2 map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	snaps2, _ := resp2["snapshots"].([]interface{})
	if len(snaps2) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps2))
	}
}

func TestOfficeDeleteSnapshot_Happy(t *testing.T) {
	ctx := newSnapshotCtx(t)
	seedSnapshotFile(t, &office.TeamSnapshot{
		Name:      "to-delete",
		CreatedAt: time.Now(),
		Members:   []office.SnapshotMemberEntry{{ConfigID: "vex", Config: office.TeamMemberConfig{ID: "vex", Name: "Vex"}}},
	})

	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/snapshots/to-delete", nil), "name", "to-delete")
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["deleted"] != "to-delete" {
		t.Errorf("deleted=%v, want to-delete", resp["deleted"])
	}

	// It should be gone from the listing.
	recList := httptest.NewRecorder()
	ctx.OfficeListSnapshots(recList, httptest.NewRequest("GET", "/api/office/snapshots", nil))
	var listed map[string]interface{}
	_ = json.Unmarshal(recList.Body.Bytes(), &listed)
	snaps, _ := listed["snapshots"].([]interface{})
	if len(snaps) != 0 {
		t.Errorf("expected snapshot removed, got %d remaining", len(snaps))
	}
}

func TestOfficeDeleteSnapshot_MissingIsIdempotent(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// office.DeleteSnapshot treats os.IsNotExist as success, so deleting a
	// snapshot that was never created is a 200 no-op.
	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/snapshots/ghost", nil), "name", "ghost")
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteSnapshot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (idempotent delete) for missing snapshot, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOfficeDeleteSnapshot_InvalidName(t *testing.T) {
	ctx := newSnapshotCtx(t)
	// An unsafe name (path traversal) fails ValidateSnapshotName -> 400,
	// never touching the filesystem.
	req := withURLParam(httptest.NewRequest("DELETE", "/api/office/snapshots/..%2Fetc", nil), "name", "../etc")
	rec := httptest.NewRecorder()
	ctx.OfficeDeleteSnapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid snapshot name, got %d: %s", rec.Code, rec.Body.String())
	}
}
