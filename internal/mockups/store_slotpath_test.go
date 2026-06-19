package mockups

import (
	"path/filepath"
	"testing"
)

// pngBytes is a minimal PNG-signature-bearing payload Save accepts.
var pngBytes = append([]byte{0x89}, []byte("PNG\r\n\x1a\n....")...)

func TestSlotPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pid := "slotproj"
	kind := KindUI

	// Arrange: seed slots 0 and 2 (leave 1 + the rest empty).
	if _, err := Save(pid, kind, 0, pngBytes, 1700000000); err != nil {
		t.Fatalf("seed slot 0: %v", err)
	}
	if _, err := Save(pid, kind, 2, pngBytes, 1700000010); err != nil {
		t.Fatalf("seed slot 2: %v", err)
	}

	cases := []struct {
		name      string
		index     int
		wantErr   bool
		wantSlot0 byte // expected leading filename digit when no error
	}{
		{name: "filled slot 0", index: 0, wantErr: false, wantSlot0: '0'},
		{name: "filled slot 2", index: 2, wantErr: false, wantSlot0: '2'},
		{name: "empty slot 1", index: 1, wantErr: true},
		{name: "negative index", index: -1, wantErr: true},
		{name: "out of range index", index: MockupCount, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act.
			got, err := SlotPath(pid, kind, tc.index)

			// Assert.
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for index %d, got path %q", tc.index, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			base := filepath.Base(got)
			if base[0] != tc.wantSlot0 {
				t.Errorf("SlotPath(%d) = %q, want filename starting with %c", tc.index, got, tc.wantSlot0)
			}
		})
	}
}
