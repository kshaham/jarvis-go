package strs

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"first non-empty", []string{"a", "b"}, "a"},
		{"whitespace skipped", []string{"   ", "\t\n", "real"}, "real"},
		{"no args", nil, ""},
		{"single", []string{"only"}, "only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FirstNonEmpty(tc.in...)
			if got != tc.want {
				t.Errorf("FirstNonEmpty(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsSafeRelativePath_Accepts(t *testing.T) {
	for _, p := range []string{
		"",
		"foo.txt",
		"dir/sub/file.txt",
		"dir-with-dash/file_x.json",
		"file.with.dots.png",
		"a",
	} {
		if !IsSafeRelativePath(p) {
			t.Errorf("expected %q to be safe", p)
		}
	}
}

func TestIsSafeRelativePath_RejectsTraversal(t *testing.T) {
	for _, p := range []string{
		"..",
		"../escape",
		"dir/../escape",
		"foo/..bar",  // contains "..", reject conservatively
		"foo/bar/..",
	} {
		if IsSafeRelativePath(p) {
			t.Errorf("expected %q to be REJECTED (traversal)", p)
		}
	}
}

func TestIsSafeRelativePath_RejectsAbsolute(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "/", "/a"} {
		if IsSafeRelativePath(p) {
			t.Errorf("expected %q to be REJECTED (absolute)", p)
		}
	}
}

func TestIsSafeRelativePath_RejectsNULByte(t *testing.T) {
	if IsSafeRelativePath("foo\x00bar") {
		t.Error("NUL byte should be rejected")
	}
}

func TestIsValidSessionID(t *testing.T) {
	cases := []struct {
		name string
		sid  string
		want bool
	}{
		{"canonical office-uuid", "office-12345abc", true},
		{"min length 8", "abcd1234", true},
		{"max length 128", string(make([]byte, 128, 128)), false}, // null bytes
		{"hyphens and underscores", "sess_001-foo", true},
		{"empty", "", false},
		{"too short", "abc", false},
		{"contains slash", "foo/bar1234", false},
		{"contains dot", "foo.bar1234", false},
		{"path traversal", "../etc/pass", false},
		{"shell metacharacter", "x;rm -rf /", false},
		{"129 chars", string(make([]byte, 129, 129)), false},
	}
	// Build a real 128-char valid sid to test the upper bound.
	long := make([]byte, 128)
	for i := range long {
		long[i] = 'a'
	}
	cases = append(cases, struct {
		name string
		sid  string
		want bool
	}{"128 alpha", string(long), true})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidSessionID(tc.sid)
			if got != tc.want {
				t.Errorf("IsValidSessionID(%q) = %v, want %v", tc.sid, got, tc.want)
			}
		})
	}
}
