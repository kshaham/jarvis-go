package mockups

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasUI(t *testing.T) {
	cases := []struct {
		name  string
		write map[string]string
		want  bool
	}{
		{"react", map[string]string{"package.json": `{"dependencies":{"react":"^19"}}`}, true},
		{"vue dev", map[string]string{"package.json": `{"devDependencies":{"vue":"^3"}}`}, true},
		{"vite", map[string]string{"package.json": `{"devDependencies":{"vite":"^5"}}`}, true},
		{"index.html", map[string]string{"index.html": "<html></html>"}, true},
		{"backend only", map[string]string{"package.json": `{"dependencies":{"express":"^4"}}`}, false},
		{"go", map[string]string{"go.mod": "module x"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tc.write {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(c), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := HasUI(dir); got != tc.want {
				t.Errorf("HasUI = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	if !ValidateProjectID("a1b2-c3") {
		t.Error("uuid-ish id should be valid")
	}
	if ValidateProjectID("../escape") || ValidateProjectID("") {
		t.Error("traversal/empty must be invalid")
	}
	if !ValidateFilename("0-1700000000.png") {
		t.Error("valid filename rejected")
	}
	for _, bad := range []string{"6-1700000000.png", "0-1.png", "0-1700000000.jpg", "x-1700000000.png", "../e.png"} {
		if ValidateFilename(bad) {
			t.Errorf("filename %q should be invalid", bad)
		}
	}
}

func TestHasDocs(t *testing.T) {
	cases := []struct {
		name  string
		write map[string]string
		want  bool
	}{
		{"readme", map[string]string{"README.md": "# X"}, true},
		{"any md", map[string]string{"NOTES.md": "x"}, true},
		{"no docs", map[string]string{"main.go": "package x"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, c := range tc.write {
				if err := os.WriteFile(filepath.Join(dir, f), []byte(c), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := HasDocs(dir); got != tc.want {
				t.Errorf("HasDocs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateKind(t *testing.T) {
	if !ValidateKind("ui") || !ValidateKind("presentation") {
		t.Error("ui/presentation must be valid kinds")
	}
	if ValidateKind("") || ValidateKind("deck") {
		t.Error("unknown kinds must be invalid")
	}
}

func TestSaveListLoadPrune(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pid := "proj-123"
	kind := KindPresentation
	png := append([]byte{0x89}, []byte("PNG\r\n\x1a\n....")...) // minimal PNG-ish header

	// Save two for slot 0 (newest wins) + one for slot 1.
	if _, err := Save(pid, kind, 0, png, 1700000000); err != nil {
		t.Fatalf("save 0a: %v", err)
	}
	if _, err := Save(pid, kind, 0, png, 1700000005); err != nil {
		t.Fatalf("save 0b: %v", err)
	}
	if _, err := Save(pid, kind, 1, png, 1700000010); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	list, err := List(pid, kind)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 files (slot 0 pruned to newest + slot 1), got %d: %v", len(list), list)
	}
	if list[0][0] != '0' || list[1][0] != '1' {
		t.Errorf("files not ordered by slot: %v", list)
	}
	// The other kind is isolated (no files leak across kinds).
	if l, _ := List(pid, KindUI); len(l) != 0 {
		t.Errorf("kind isolation broken: ui has %v", l)
	}
	data, err := Load(pid, kind, list[0])
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(data) != len(png) {
		t.Errorf("loaded %d bytes, want %d", len(data), len(png))
	}

	// Reject non-PNG + bad index + bad kind.
	if _, err := Save(pid, kind, 0, []byte("notpng!!"), 1700000020); err == nil {
		t.Error("non-PNG should be rejected")
	}
	if _, err := Save(pid, kind, 9, png, 1700000020); err == nil {
		t.Error("out-of-range index should be rejected")
	}
	if _, err := Save(pid, "deck", 0, png, 1700000020); err == nil {
		t.Error("bad kind should be rejected")
	}

	if err := ClearProject(pid, kind); err != nil {
		t.Fatal(err)
	}
	if l, _ := List(pid, kind); len(l) != 0 {
		t.Errorf("expected empty after clear, got %v", l)
	}
}
