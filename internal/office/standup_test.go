package office

import "testing"

func TestStandupStateHasID(t *testing.T) {
	st := &StandupState{ID: "abc"}
	if st.ID != "abc" {
		t.Fatalf("ID field missing or wrong")
	}
}
