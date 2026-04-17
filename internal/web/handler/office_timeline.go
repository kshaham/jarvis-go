package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/claudeman/jarvis-go/internal/office"
	"github.com/claudeman/jarvis-go/internal/web/request"
)

// TimelineEntry is the unified shape returned by /api/office/timeline.
// Sources (audit, task, standup, message, learning) all map to this type
// so the UI can render a single chronological list without knowing each
// source's internal schema.
type TimelineEntry struct {
	Timestamp time.Time   `json:"timestamp"`
	Kind      string      `json:"kind"`
	AgentID   string      `json:"agentId,omitempty"`
	AgentName string      `json:"agentName,omitempty"`
	Title     string      `json:"title"`
	Summary   string      `json:"summary,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// maxTimelineLimit caps per-request pagination so a buggy client can't ask
// for the entire retained history in one call.
const maxTimelineLimit = 500

// OfficeTimeline returns a merged, paginated view of audit log + task
// transitions + standup completions + message log. Accepts:
//
//	?limit=100      (default 100, max 500)
//	?offset=0
//	?agent=<sessionId>
//	?type=audit|task|standup|message
//	?since=<unix_millis>
//
// GET /api/office/timeline
func (ctx *Context) OfficeTimeline(w http.ResponseWriter, r *http.Request) {
	if ctx.OfficeManager == nil {
		request.WriteError(w, http.StatusServiceUnavailable, "office manager not initialized")
		return
	}

	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxTimelineLimit {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	agentFilter := q.Get("agent")
	typeFilter := q.Get("type")
	var sinceTS time.Time
	if v := q.Get("since"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			sinceTS = time.UnixMilli(ms)
		}
	}

	all := ctx.collectTimelineEntries()

	// Filter.
	filtered := make([]TimelineEntry, 0, len(all))
	for _, e := range all {
		if typeFilter != "" && e.Kind != typeFilter {
			continue
		}
		if agentFilter != "" && e.AgentID != agentFilter {
			continue
		}
		if !sinceTS.IsZero() && e.Timestamp.Before(sinceTS) {
			continue
		}
		filtered = append(filtered, e)
	}

	// Sort desc by timestamp. Stable so equal timestamps preserve source order.
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	total := len(filtered)
	if offset >= total {
		filtered = nil
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"entries": filtered,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"hasMore": offset+len(filtered) < total,
	})
}

// collectTimelineEntries gathers entries from the four source systems.
// Pure in-memory; relies on existing retention bounds (messages capped at
// MaxMessages, audit at AuditMaxEntries, tasks live only while agents
// exist). No persistence or caching -- cheap enough to recompute.
func (ctx *Context) collectTimelineEntries() []TimelineEntry {
	m := ctx.OfficeManager
	roster := m.GetRoster()

	// Map sessionID -> agent name so we can label entries even when the
	// source only carries the ID.
	nameByID := make(map[string]string, len(roster))
	for _, a := range roster {
		if a != nil && a.Config != nil {
			nameByID[a.SessionID] = a.Config.Name
		}
	}

	out := make([]TimelineEntry, 0, 256)

	// Audit entries.
	for _, e := range m.GetAuditEntries() {
		name := e.AgentName
		if name == "" {
			name = nameByID[e.SessionID]
		}
		out = append(out, TimelineEntry{
			Timestamp: e.Timestamp,
			Kind:      "audit",
			AgentID:   e.SessionID,
			AgentName: name,
			Title:     e.Event,
			Data:      e.Data,
		})
	}

	// Task transitions: emit up to three entries per task (assigned,
	// started, completed). Skip zero-valued timestamps so a never-started
	// task only contributes one entry.
	for _, a := range roster {
		if a == nil || a.Config == nil {
			continue
		}
		for _, t := range a.GetTasks() {
			if !t.AssignedAt.IsZero() {
				out = append(out, TimelineEntry{
					Timestamp: t.AssignedAt,
					Kind:      "task",
					AgentID:   a.SessionID,
					AgentName: a.Config.Name,
					Title:     "Task assigned: " + t.Title,
					Summary:   string(t.Status),
					Data:      map[string]interface{}{"taskId": t.ID, "status": t.Status, "priority": t.Priority, "transition": "assigned"},
				})
			}
			if !t.StartedAt.IsZero() {
				out = append(out, TimelineEntry{
					Timestamp: t.StartedAt,
					Kind:      "task",
					AgentID:   a.SessionID,
					AgentName: a.Config.Name,
					Title:     "Task started: " + t.Title,
					Data:      map[string]interface{}{"taskId": t.ID, "transition": "started"},
				})
			}
			if !t.CompletedAt.IsZero() {
				out = append(out, TimelineEntry{
					Timestamp: t.CompletedAt,
					Kind:      "task",
					AgentID:   a.SessionID,
					AgentName: a.Config.Name,
					Title:     "Task " + string(t.Status) + ": " + t.Title,
					Data:      map[string]interface{}{"taskId": t.ID, "status": t.Status, "transition": "completed"},
				})
			}
		}
	}

	// Messages.
	for _, msg := range m.GetMessages() {
		senderName := msg.FromName
		if senderName == "" {
			senderName = nameByID[msg.From]
		}
		recipient := msg.ToName
		if recipient == "" {
			recipient = nameByID[msg.To]
		}
		title := senderName + " → " + recipient
		if recipient == "" {
			title = senderName + " → team"
		}
		summary := msg.Content
		if len(summary) > 280 {
			summary = summary[:280] + "..."
		}
		out = append(out, TimelineEntry{
			Timestamp: msg.Timestamp,
			Kind:      "message",
			AgentID:   msg.From,
			AgentName: senderName,
			Title:     title,
			Summary:   summary,
			Data:      map[string]interface{}{"to": msg.To, "toName": recipient},
		})
	}

	// Standup completions. Use the handler's existing list helper if
	// present; fall back to in-memory standup when no history has been
	// persisted yet.
	for _, s := range listPersistedStandups() {
		out = append(out, TimelineEntry{
			Timestamp: time.UnixMilli(s.CompletedAt),
			Kind:      "standup",
			Title:     "Standup: " + strings.Join(s.Participants, ", "),
			Summary:   fmtStandupSummary(s),
			Data:      map[string]interface{}{"standupId": s.ID, "responded": s.RespondedCount, "timedOut": s.TimedOutCount},
		})
	}

	return out
}

// fmtStandupSummary is a deliberately tiny formatter so the timeline list
// item has useful text without re-rendering the full transcript.
func fmtStandupSummary(s office.PersistedStandup) string {
	if s.ParticipantCount == 0 {
		return ""
	}
	return strconv.Itoa(s.RespondedCount) + "/" + strconv.Itoa(s.ParticipantCount) + " responded"
}

// listPersistedStandups enumerates ~/.jarvis/office-memory/standups/*.json
// and parses each file. Mirrors what OfficeGetStandupHistory does but
// returns typed values rather than the raw directory listing the handler
// returns to the client.
func listPersistedStandups() []office.PersistedStandup {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(homeDir, ".jarvis", "office-memory", "standups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]office.PersistedStandup, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s office.PersistedStandup
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
