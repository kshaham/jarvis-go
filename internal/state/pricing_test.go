package state

import "testing"

func TestComputeCost_Sonnet(t *testing.T) {
	// 1M input tokens + 1M output tokens at Sonnet rates = $3 + $15 = $18.
	got := ComputeCost("claude-sonnet-4-6-20251001", 1_000_000, 1_000_000)
	if got != 18.0 {
		t.Errorf("sonnet 1M+1M expected $18, got $%.4f", got)
	}
}

func TestComputeCost_UnknownModelFallsBackToSonnet(t *testing.T) {
	got := ComputeCost("some-new-model-we-dont-know", 1_000_000, 0)
	if got != 3.0 {
		t.Errorf("unknown model 1M input expected $3 (sonnet default), got $%.4f", got)
	}
}

func TestComputeCost_EmptyModelFallsBack(t *testing.T) {
	got := ComputeCost("", 0, 1_000_000)
	if got != 15.0 {
		t.Errorf("empty model 1M output expected $15 (sonnet default), got $%.4f", got)
	}
}

func TestComputeCost_Opus(t *testing.T) {
	got := ComputeCost("claude-opus-4-7", 1_000_000, 0)
	if got != 15.0 {
		t.Errorf("opus 1M input expected $15, got $%.4f", got)
	}
}

func TestComputeCost_ZaiGLM(t *testing.T) {
	// GLM-4.7: 0.50 input, 1.50 output per million.
	got := ComputeCost("glm-4.7-some-variant", 2_000_000, 1_000_000)
	want := 2*0.50 + 1*1.50
	if got != want {
		t.Errorf("glm-4.7 2M+1M expected $%.4f, got $%.4f", want, got)
	}
}
