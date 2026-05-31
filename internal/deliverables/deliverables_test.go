package deliverables

import (
	"testing"
)

func TestTitleToSlug(t *testing.T) {
	cases := map[string]string{
		"Research Brief":          "research-brief",
		"Q3 Findings & Insights!": "q3-findings-insights",
		"  Trimmed  ":             "trimmed",
		"UPPER_case-mix":          "upper-case-mix",
		"":                        "doc",
		"###":                     "doc",
	}
	for in, want := range cases {
		if got := TitleToSlug(in); got != want {
			t.Errorf("TitleToSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// long titles are truncated and produce a valid slug
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	if got := TitleToSlug(long); !ValidateSlug(got) {
		t.Errorf("long-title slug %q is invalid", got)
	}
}

func TestValidate(t *testing.T) {
	if !ValidateProjectID("p-1") || ValidateProjectID("../x") || ValidateProjectID("") {
		t.Error("project id validation wrong")
	}
	if !ValidateSlug("research-brief") || ValidateSlug("Bad Slug") || ValidateSlug("../x") {
		t.Error("slug validation wrong")
	}
}

func TestSaveListGetVersions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pid := "proj-1"

	// Two versions of the same doc + a different doc.
	s1, err := SaveVersion(pid, "Research Brief", "research-brief", "# v1\nfirst", 1700000000)
	if err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if _, err := SaveVersion(pid, "Research Brief", "research-brief", "# v2\nsecond", 1700000100); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if _, err := SaveVersion(pid, "Project Plan", "project-plan", "# plan", 1700000050); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	list, err := List(pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 deliverables, got %d: %+v", len(list), list)
	}
	// newest-updated first → Research Brief (v2 @ 1700000100) before Project Plan (@ 1700000050)
	if list[0].Slug != "research-brief" {
		t.Errorf("order wrong, first = %q", list[0].Slug)
	}
	if list[0].Title != "Research Brief" || list[0].Type != "research-brief" {
		t.Errorf("meta wrong: %+v", list[0])
	}
	if len(list[0].Versions) != 2 || list[0].Versions[0] != 1700000100 {
		t.Errorf("versions wrong: %v", list[0].Versions)
	}

	// Latest content.
	content, meta, err := Get(pid, s1, 0)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if content != "# v2\nsecond" || meta.Title != "Research Brief" {
		t.Errorf("latest content/meta wrong: %q / %+v", content, meta)
	}
	// Specific older version.
	old, _, err := Get(pid, s1, 1700000000)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if old != "# v1\nfirst" {
		t.Errorf("v1 content wrong: %q", old)
	}

	// Reject empty + path-escape slug.
	if _, err := SaveVersion(pid, "Empty", "x", "", 1700000200); err == nil {
		t.Error("empty content should be rejected")
	}
	if _, _, err := Get(pid, "../escape", 0); err == nil {
		t.Error("bad slug should be rejected")
	}
}
