package main

import (
	"strings"
	"testing"
)

func TestHandleResourceRead_Sessions(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[{"id":"s1"},{"id":"s2"}]`))
	contents, err := handleResourceRead("jarvis://sessions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	c := contents[0]
	if c.URI != "jarvis://sessions" || c.MimeType != "application/json" {
		t.Fatalf("unexpected content metadata: %+v", c)
	}
	if !strings.Contains(c.Text, "s1") || !strings.Contains(c.Text, "s2") {
		t.Fatalf("expected session ids in text, got %q", c.Text)
	}
	if cap.get().Path != "/api/sessions" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleResourceRead_Agents(t *testing.T) {
	cap := &captureRequest{}
	withServer(t, recordingHandler(cap, 200, `[{"id":"ag1"}]`))
	contents, err := handleResourceRead("jarvis://agents")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 || contents[0].URI != "jarvis://agents" {
		t.Fatalf("unexpected contents: %+v", contents)
	}
	if !strings.Contains(contents[0].Text, "ag1") {
		t.Fatalf("expected ag1 in text, got %q", contents[0].Text)
	}
	if cap.get().Path != "/api/subagents" {
		t.Fatalf("unexpected path %s", cap.get().Path)
	}
}

func TestHandleResourceRead_SessionsUpstreamError(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 500, "boom"))
	contents, err := handleResourceRead("jarvis://sessions")
	if err == nil {
		t.Fatal("expected error when upstream fails")
	}
	if contents != nil {
		t.Fatalf("expected nil contents on error, got %+v", contents)
	}
}

func TestHandleResourceRead_AgentsUpstreamError(t *testing.T) {
	withServer(t, recordingHandler(&captureRequest{}, 503, "down"))
	_, err := handleResourceRead("jarvis://agents")
	if err == nil {
		t.Fatal("expected error when upstream fails")
	}
}

func TestHandleResourceRead_UnknownURI(t *testing.T) {
	contents, err := handleResourceRead("jarvis://does-not-exist")
	if err != nil {
		t.Fatalf("unknown URI should return nil,nil; got err %v", err)
	}
	if contents != nil {
		t.Fatalf("expected nil contents for unknown URI, got %+v", contents)
	}
}

func TestAllResources_HaveRequiredFields(t *testing.T) {
	if len(allResources) == 0 {
		t.Fatal("allResources should not be empty")
	}
	for _, r := range allResources {
		if r.URI == "" || r.Name == "" || r.MimeType == "" {
			t.Errorf("resource missing required field: %+v", r)
		}
	}
}
