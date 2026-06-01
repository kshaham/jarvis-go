package claudeauth

import (
	"testing"
	"time"
)

// TestSetClockForTest verifies the test clock hook swaps the time source and
// the returned restore function puts the original source back.
func TestSetClockForTest(t *testing.T) {
	fixed := time.Unix(1700000000, 0)
	restore := SetClockForTest(func() time.Time { return fixed })
	if !timeNow().Equal(fixed) {
		t.Fatalf("expected fixed clock, got %v", timeNow())
	}
	restore()
	if timeNow().Equal(fixed) {
		t.Fatal("restore did not reinstate the original clock")
	}
}
