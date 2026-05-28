package library

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kshaham/jarvis/internal/testutil/stubbin"
)

// installLLMStub swaps llmBinary for a script that prints
// `payload` to stdout (simulating claude -p --output-format json).
// Returns nothing; cleanup restores via t.Cleanup.
func installLLMStub(t *testing.T, payload string) {
	t.Helper()
	path := stubbin.EchoStub(t, payload)
	prev := SetLLMBinaryForTest(path)
	t.Cleanup(func() { SetLLMBinaryForTest(prev) })
}

// installLLMFailingStub installs a script that exits non-zero.
func installLLMFailingStub(t *testing.T) {
	t.Helper()
	path := stubbin.Install(t, "stub-fail.sh", "#!/bin/sh\nexit 7\n")
	prev := SetLLMBinaryForTest(path)
	t.Cleanup(func() { SetLLMBinaryForTest(prev) })
}

func newSingleCluster() *experienceCluster {
	return &experienceCluster{
		role:   "backend-engineer",
		labels: []string{"auth", "jwt"},
		entries: []*experienceMeta{
			{
				TaskTitle:       "Add JWT refresh",
				OutcomeScore:    0.92,
				ApproachSummary: "Middleware first, handler last.",
				WorkingDir:      "/repo/internal/auth",
			},
			{
				TaskTitle:       "Rotate signing key",
				OutcomeScore:    0.88,
				ApproachSummary: "Wrote rotation script + cron entry.",
				WorkingDir:      "/repo/internal/auth",
			},
			{
				TaskTitle:       "Botch session resume",
				OutcomeScore:    0.50,
				ApproachSummary: "Forgot to clear the refresh cookie.",
				WorkingDir:      "/repo/internal/auth",
			},
		},
	}
}

func TestExtractWorkflow_HappyPathParsesEnvelope(t *testing.T) {
	// Realistic claude -p --output-format json envelope: top-level
	// {result: "<json blob>"}.
	installLLMStub(t, `{"result":"{\"title\":\"Add JWT refresh end-to-end\",\"summary\":\"Build middleware first, then wire the handler.\",\"steps\":[\"Write the middleware\",\"Wire it into the handler\",\"Verify via curl\"],\"known_pitfalls\":[\"Forgetting to clear the refresh cookie\"]}"}`)

	ext, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext == nil {
		t.Fatal("nil result")
	}
	if ext.Title != "Add JWT refresh end-to-end" {
		t.Errorf("title=%q", ext.Title)
	}
	if len(ext.Steps) != 3 {
		t.Errorf("steps=%d want 3", len(ext.Steps))
	}
	if len(ext.KnownPitfalls) != 1 {
		t.Errorf("pitfalls=%d want 1", len(ext.KnownPitfalls))
	}
}

func TestExtractWorkflow_AcceptsBareJSON(t *testing.T) {
	// Some claude builds return bare JSON without the envelope.
	// The parser should fall through to a direct decode.
	installLLMStub(t, `{"title":"Direct","summary":"","steps":["s1"],"known_pitfalls":[]}`)

	ext, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext.Title != "Direct" {
		t.Errorf("title=%q want Direct", ext.Title)
	}
}

func TestExtractWorkflow_StripCodeFenceWrapper(t *testing.T) {
	// Result field wrapped in ```json ... ``` — common when the
	// model echoes its formatting rules. stripCodeFence should
	// peel them off.
	installLLMStub(t, `{"result":"`+"```json\\n"+`{\"title\":\"Fenced\",\"steps\":[\"x\"]}\n`+"```"+`"}`)

	ext, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ext == nil || ext.Title != "Fenced" {
		t.Errorf("title=%v want Fenced", ext)
	}
}

func TestExtractWorkflow_NonZeroExitReturnsError(t *testing.T) {
	installLLMFailingStub(t)
	_, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "exec") {
		t.Errorf("err=%q want stage 'exec'", err)
	}
}

func TestExtractWorkflow_EmptyClusterRejected(t *testing.T) {
	_, err := extractWorkflowWithLLM(context.Background(), &experienceCluster{}, "")
	if err == nil {
		t.Error("expected error for empty cluster")
	}
}

func TestExtractWorkflow_TimeoutReturnsTimeoutError(t *testing.T) {
	// Stub sleeps longer than the test-time timeout.
	path := stubbin.Install(t, "stub-slow.sh", "#!/bin/sh\nsleep 5\n")
	prevBin := SetLLMBinaryForTest(path)
	t.Cleanup(func() { SetLLMBinaryForTest(prevBin) })
	prevTO := SetLLMTimeoutForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetLLMTimeoutForTest(prevTO) })

	_, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "exec") {
		t.Errorf("err=%q want timeout/exec stage", err)
	}
}

func TestExtractWorkflow_EmptyResponseTriggersError(t *testing.T) {
	// Model returned empty struct → caller must treat as failure
	// so memify falls back to the heuristic Workflow.
	installLLMStub(t, `{"result":"{}"}`)

	_, err := extractWorkflowWithLLM(context.Background(), newSingleCluster(), "")
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestBuildExtractPrompt_IncludesAllSignals(t *testing.T) {
	prompt := buildExtractPrompt(newSingleCluster())
	for _, want := range []string{
		"Agent role: backend-engineer",
		"Task labels: auth, jwt",
		"Add JWT refresh",
		"outcome 0.92",
		"Middleware first",
		"JSON object",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", want, prompt)
		}
	}
}

func TestStripCodeFence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"```json\n{\"x\":1}\n```", `{"x":1}`},
		{"```\n{\"x\":1}\n```", `{"x":1}`},
		{"```json\n{\"x\":1}", `{"x":1}`}, // unclosed fence
		{"  \n```\nhi\n```\n  ", "hi"},
	}
	for _, tc := range cases {
		if got := stripCodeFence(tc.in); got != tc.want {
			t.Errorf("stripCodeFence(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestMergeIntoWorkflow_OverridesAuthoredFieldsOnly(t *testing.T) {
	base := Workflow{
		Slug:                 "auth",
		Title:                "heuristic title",
		DerivedFromTaskIDs:   []string{"t1", "t2"},
		AvgOutcome:           0.85,
		ReuseCountAtCreation: 2,
		AgentRole:            "backend",
		TaskLabels:           []string{"auth"},
		WorkingDirHints:      []string{"/repo"},
		Steps:                []string{"heuristic step"},
		KnownPitfalls:        []string{"heuristic pitfall"},
	}
	ext := &ExtractedWorkflow{
		Title:         "llm title",
		Steps:         []string{"llm step 1", "llm step 2"},
		KnownPitfalls: []string{"llm pitfall"},
		Summary:       "the gist",
	}
	got := mergeIntoWorkflow(base, ext)

	// Identity / provenance unchanged.
	if got.Slug != "auth" || got.AvgOutcome != 0.85 ||
		got.ReuseCountAtCreation != 2 || got.AgentRole != "backend" {
		t.Errorf("identity fields drifted: %+v", got)
	}
	// LLM overrides title + steps + pitfalls.
	if got.Title != "llm title" {
		t.Errorf("title=%q want llm title", got.Title)
	}
	// Summary lands as first Step (Workflow has no Summary field).
	if len(got.Steps) != 3 || !strings.HasPrefix(got.Steps[0], "_Summary_") {
		t.Errorf("summary should prepend Steps; got: %v", got.Steps)
	}
	if len(got.KnownPitfalls) != 1 || got.KnownPitfalls[0] != "llm pitfall" {
		t.Errorf("pitfalls=%v want [llm pitfall]", got.KnownPitfalls)
	}
}

func TestMergeIntoWorkflow_EmptyExtractKeepsBase(t *testing.T) {
	base := Workflow{Slug: "x", Title: "base", Steps: []string{"a"}}
	got := mergeIntoWorkflow(base, &ExtractedWorkflow{})
	if got.Title != "base" || len(got.Steps) != 1 {
		t.Errorf("empty ext should keep base; got: %+v", got)
	}
}

func TestMergeIntoWorkflow_NilExtractReturnsBase(t *testing.T) {
	base := Workflow{Slug: "x", Title: "base"}
	got := mergeIntoWorkflow(base, nil)
	if got.Title != "base" {
		t.Errorf("nil ext should return base; got: %+v", got)
	}
}
