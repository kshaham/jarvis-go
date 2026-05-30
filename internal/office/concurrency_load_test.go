package office

// Concurrent-load regression guards for the office data-race classes that
// were fixed by making the task readers deep-copy under a.mu and routing
// SessionID / Config / Whiteboard reads through their owning lock.
//
// Each test spins up N>=20 reader goroutines and N>=20 writer goroutines
// that all unblock off a single closed-channel start barrier (no
// time.Sleep ordering, no flakiness) and then hammer the same shared state
// for a few hundred iterations. Run under the race detector:
//
//	go test -race -run TestConcurrentLoad -count=1 ./internal/office/
//
// If any of these fire a DATA RACE, that is a real remaining race in the
// production code -- the test is written to surface it with a stack, not to
// paper over it.

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

const (
	// readerGoroutines / writerGoroutines exceed the >=20 floor so the
	// scheduler has plenty of contending goroutines to interleave.
	readerGoroutines = 32
	writerGoroutines = 32
	// loadIterations is the per-goroutine loop count. A few hundred
	// iterations per goroutine keeps the race window open long enough to
	// be hit reliably while keeping the test fast.
	loadIterations = 400
)

// seedTasks gives an agent a deterministic set of tasks to mutate/read.
func seedTasks(a *AgentState, ids ...string) {
	for _, id := range ids {
		a.AddTask(&Task{
			ID:         id,
			Title:      "task " + id,
			Status:     TaskInProgress,
			Priority:   "medium",
			AssignedAt: time.Now(),
			StartedAt:  time.Now(),
		})
	}
}

// startBarrier returns a channel that, once closed, releases every
// goroutine waiting on it at (as close as the scheduler allows to) the same
// instant -- a deterministic concurrent launch with no time.Sleep.
func startBarrier() chan struct{} {
	return make(chan struct{})
}

// TestConcurrentLoad_TaskAccessorsVsMutators exercises class (A): the
// deep-copying task readers (BuildMemberPayload / GetOfficeState / GetTasks
// / GetTasksCopy / ActiveTask) running concurrently with the in-place task
// mutators (AddTask / UpdateTaskStatus / SetTaskProgress / SetTaskCriteria /
// IncrementCriteriaReject / AddLabel / RemoveLabel). After the deep-copy fix
// this must be race-clean.
func TestConcurrentLoad_TaskAccessorsVsMutators(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	cfg := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "strategic", Color: "#0a0"}
	m.RegisterAgent("sess-vex", "vex", cfg)
	agent := m.GetAgent("sess-vex")

	// Seed a fixed set of tasks the writers will mutate and readers will copy.
	taskIDs := []string{"t-0", "t-1", "t-2", "t-3"}
	seedTasks(agent, taskIDs...)

	start := startBarrier()
	var wg sync.WaitGroup

	// Readers: every accessor that now deep-copies under a.mu, plus the
	// manager-level marshaling readers that walk those copies.
	for r := 0; r < readerGoroutines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				_ = m.BuildMemberPayload(agent)
				_ = m.GetOfficeState()
				tasks := agent.GetTasks()
				for _, tk := range tasks {
					// Touch the reference fields the deep copy is meant to
					// protect; a shallow copy would race the mutators here.
					_ = len(tk.Labels)
					_ = len(tk.AcceptanceCriteria)
					_ = len(tk.ProgressLog)
					_ = tk.Status
					_ = tk.CriteriaRejectCount
				}
				_ = agent.GetTasksCopy()
				if at := agent.ActiveTask(); at != nil {
					_ = at.Title
					_ = len(at.ProgressLog)
				}
				_ = agent.GetTaskLabels("t-0")
			}
		}()
	}

	// Writers: the in-place mutators that share a.taskQueue.Tasks.
	for w := 0; w < writerGoroutines; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			<-start
			statuses := []TaskStatus{TaskInProgress, TaskAcknowledged, TaskBlocked, TaskInProgress}
			for i := 0; i < loadIterations; i++ {
				id := taskIDs[i%len(taskIDs)]
				agent.UpdateTaskStatus(id, statuses[i%len(statuses)])
				agent.SetTaskProgress(id, fmt.Sprintf("w%d progress %d", wid, i))
				_ = agent.SetTaskCriteria(id, "set", []string{fmt.Sprintf("crit-%d-%d", wid, i)})
				agent.IncrementCriteriaReject(id)
				_ = agent.AddLabel(id, fmt.Sprintf("lbl%d", wid%5))
				_ = agent.RemoveLabel(id, fmt.Sprintf("lbl%d", (wid+1)%5))
				// Occasionally grow the queue so AddTask races the readers' len().
				if i%97 == 0 {
					agent.AddTask(&Task{
						ID:         fmt.Sprintf("extra-%d-%d", wid, i),
						Title:      "extra",
						Status:     TaskPending,
						AssignedAt: time.Now(),
					})
				}
			}
		}(w)
	}

	close(start)
	wg.Wait()
}

// TestConcurrentLoad_SessionIDReassign exercises class (B): readers that
// resolve an agent's SessionID through the manager (GetOfficeState ->
// sessionIDOf, BuildMemberPayload -> sessionIDOf) running concurrently with
// repeated ReassignSessionID(old,new) calls that mutate AgentState.SessionID
// under m.mu. The sessionIDOf read path takes m.mu.RLock, so this should be
// race-clean; if -race fires it is a genuinely-unsynchronized SessionID read.
func TestConcurrentLoad_SessionIDReassign(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	cfg := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend", Tier: "worker", Color: "#00a"}
	m.RegisterAgent("sess-0", "bolt", cfg)
	agent := m.GetAgent("sess-0")
	seedTasks(agent, "t-a", "t-b")

	start := startBarrier()
	var wg sync.WaitGroup

	// Readers: every manager-level read that surfaces a SessionID.
	for r := 0; r < readerGoroutines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				_ = m.GetOfficeState()
				// GetRoster returns live *AgentState; resolve SessionID
				// through the manager so the read is synchronized.
				for _, a := range m.GetRoster() {
					_ = m.BuildMemberPayload(a)
				}
				_ = m.GetWhiteboard()
				_ = m.ListPendingHandoffs()
			}
		}()
	}

	// A single rename loop, plus extra renamer goroutines that cycle the
	// id through a private namespace and back so exactly one mapping is
	// ever live (ReassignSessionID rejects collisions). The renamers
	// serialize on m.mu, so the live session id walks a deterministic
	// ring without ever colliding.
	var renameMu sync.Mutex
	current := "sess-0"
	counter := 0
	rename := func() {
		renameMu.Lock()
		defer renameMu.Unlock()
		counter++
		next := fmt.Sprintf("sess-%d", counter)
		if m.ReassignSessionID(current, next) {
			current = next
		}
	}
	for w := 0; w < writerGoroutines; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				rename()
			}
		}()
	}

	close(start)
	wg.Wait()
}

// TestConcurrentLoad_Handoff exercises class (C): concurrent
// InitiateHandoff / AcceptHandoff / RejectHandoff / ListPendingHandoffs /
// resolveHandoffPair against the same task moving between two agents. The
// handoff mutators all operate under the owning agent's a.mu; the readers
// (ListPendingHandoffs via GetTasksCopy, BuildMemberPayload) deep-copy.
// Repeatedly cycling a single task between the two queues keeps the
// initiate/accept critical sections contending.
func TestConcurrentLoad_Handoff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	vex := &TeamMemberConfig{ID: "vex", Name: "Vex", Role: "Frontend", Tier: "worker"}
	bolt := &TeamMemberConfig{ID: "bolt", Name: "Bolt", Role: "Backend", Tier: "worker"}
	m.RegisterAgent("sess-vex", "vex", vex)
	m.RegisterAgent("sess-bolt", "bolt", bolt)
	agentVex := m.GetAgent("sess-vex")
	agentBolt := m.GetAgent("sess-bolt")

	// One shared task lives on whichever agent currently owns it. A driver
	// loop guarded by ownerMu performs initiate-then-resolve transitions so
	// the task ping-pongs between the two queues; the contention is between
	// that driver, the lock-free ListPendingHandoffs / resolveHandoffPair
	// readers, and the payload marshalers -- all hitting the same live task
	// and handoff pointer.
	const taskID = "t-handoff"
	agentVex.AddTask(&Task{
		ID:         taskID,
		Title:      "Refactor auth",
		Status:     TaskInProgress,
		Priority:   "medium",
		AssignedAt: time.Now(),
		StartedAt:  time.Now(),
	})

	start := startBarrier()
	var wg sync.WaitGroup

	// Readers: handoff list + the manager state marshalers that walk tasks.
	for r := 0; r < readerGoroutines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				for _, h := range m.ListPendingHandoffs() {
					_ = h.Status
					_ = h.TaskID
				}
				// resolveHandoffPair is exercised indirectly, but call the
				// public state readers that walk the same tasks/handoffs.
				_ = m.GetOfficeState()
				_ = m.BuildMemberPayload(agentVex)
				_ = m.BuildMemberPayload(agentBolt)
			}
		}()
	}

	// Writers: a single serialized driver (guarded by ownerMu) that knows
	// which agent owns the task, plus many goroutines racing on accept/reject
	// for whichever handoff is currently pending. Serializing the
	// initiate/own-side bookkeeping keeps the scenario well-defined while the
	// accept/reject calls themselves stay genuinely concurrent (the
	// production code is built to let the loser of an accept/accept race bail
	// cleanly).
	var ownerMu sync.Mutex
	senderSess := "sess-vex"
	senderConf := "vex"
	receiverSess := "sess-bolt"
	receiverConf := "bolt"

	driveOne := func(i int) {
		ownerMu.Lock()
		defer ownerMu.Unlock()
		// Initiate from the current owner to the other agent.
		_, err := m.InitiateHandoff(senderSess, taskID, receiverConf, "ctx")
		if err != nil {
			// A prior iteration may have left it pending; try to resolve.
			return
		}
		// Resolve: alternate accept / reject. On accept the task moves to the
		// receiver, so swap the roles for the next iteration. On reject the
		// task stays put.
		if i%2 == 0 {
			if _, err := m.AcceptHandoff(receiverSess, taskID); err == nil {
				senderSess, receiverSess = receiverSess, senderSess
				senderConf, receiverConf = receiverConf, senderConf
			}
		} else {
			_, _ = m.RejectHandoff(receiverSess, taskID, "busy")
		}
	}

	for w := 0; w < writerGoroutines; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				driveOne(i)
				// Also fire lock-free accept/reject attempts that mostly miss
				// (no pending handoff) to stress resolveHandoffPair's scan.
				_, _ = m.AcceptHandoff(receiverSess, taskID)
				_, _ = m.RejectHandoff(receiverSess, taskID, "race")
			}
		}()
	}

	close(start)
	wg.Wait()
}

// TestConcurrentLoad_WhiteboardAndConfig exercises class (D): concurrent
// GetWhiteboard / GetConfig / SnapshotConfig / GetVoice reads against
// SetWhiteboardNote / SetVoice / UpdateOfficeSettings writes. GetWhiteboard
// copies notes under each agent's a.mu, GetConfig/SnapshotConfig copy under
// the owning lock, so this must be race-clean.
func TestConcurrentLoad_WhiteboardAndConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTestManager()

	sessions := []string{"sess-a", "sess-b", "sess-c"}
	for i, sid := range sessions {
		m.RegisterAgent(sid, fmt.Sprintf("cfg-%d", i), &TeamMemberConfig{
			ID:    fmt.Sprintf("cfg-%d", i),
			Name:  fmt.Sprintf("Agent%d", i),
			Role:  "worker",
			Tier:  "worker",
			Color: "#123456",
			Voice: "alloy",
		})
	}

	start := startBarrier()
	var wg sync.WaitGroup

	for r := 0; r < readerGoroutines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < loadIterations; i++ {
				wb := m.GetWhiteboard()
				for _, note := range wb {
					_ = note.Content
				}
				cfg := m.GetConfig()
				_ = cfg.IdleThreshold
				_ = cfg.BrainstormCooldown
				for _, a := range m.GetRoster() {
					snap := a.SnapshotConfig()
					_ = snap.Voice
					_ = snap.Name
					_ = a.GetVoice()
				}
			}
		}()
	}

	for w := 0; w < writerGoroutines; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			<-start
			voices := []string{"alloy", "echo", "fable", "onyx", "nova"}
			for i := 0; i < loadIterations; i++ {
				sid := sessions[i%len(sessions)]
				m.SetWhiteboardNote(sid, fmt.Sprintf("note w%d i%d", wid, i))
				if a := m.GetAgent(sid); a != nil {
					a.SetVoice(voices[i%len(voices)])
				}
				m.UpdateOfficeSettings(map[string]interface{}{
					"idleThresholdMs":      float64(30000 + (i%10)*1000),
					"brainstormCooldownMs": float64(60000 + (i%10)*1000),
				})
			}
		}(w)
	}

	close(start)
	wg.Wait()
}
