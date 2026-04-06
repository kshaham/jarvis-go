package office

import "testing"

func TestManager_AcquireLock(t *testing.T) {
	m := newTestManager()
	lock, err := m.AcquireLock("/src/session.ts", "sess-1", "Vex")
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	if lock.Owner != "sess-1" {
		t.Errorf("expected owner sess-1, got %s", lock.Owner)
	}
}

func TestManager_AcquireLock_Conflict(t *testing.T) {
	m := newTestManager()
	_, _ = m.AcquireLock("/src/session.ts", "sess-1", "Vex")

	_, err := m.AcquireLock("/src/session.ts", "sess-2", "Bolt")
	if err == nil {
		t.Error("expected conflict error")
	}
}

func TestManager_AcquireLock_SameOwner(t *testing.T) {
	m := newTestManager()
	_, _ = m.AcquireLock("/src/session.ts", "sess-1", "Vex")

	lock, err := m.AcquireLock("/src/session.ts", "sess-1", "Vex")
	if err != nil {
		t.Errorf("same owner should succeed: %v", err)
	}
	if lock == nil {
		t.Error("expected lock")
	}
}

func TestManager_ReleaseLock(t *testing.T) {
	m := newTestManager()
	_, _ = m.AcquireLock("/src/session.ts", "sess-1", "Vex")

	if !m.ReleaseLock("/src/session.ts", "sess-1") {
		t.Error("expected successful release")
	}
	if m.ReleaseLock("/src/session.ts", "sess-1") {
		t.Error("double release should fail")
	}
}

func TestManager_ReleaseLock_WrongOwner(t *testing.T) {
	m := newTestManager()
	_, _ = m.AcquireLock("/src/session.ts", "sess-1", "Vex")

	if m.ReleaseLock("/src/session.ts", "sess-2") {
		t.Error("wrong owner should not release")
	}
}

func TestManager_GetLocks(t *testing.T) {
	m := newTestManager()
	_, _ = m.AcquireLock("/a.ts", "s1", "Vex")
	_, _ = m.AcquireLock("/b.ts", "s2", "Bolt")

	locks := m.GetLocks()
	if len(locks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(locks))
	}
}

func TestNormalizePath(t *testing.T) {
	if normalizePath("  /src/file.ts/  ") != "/src/file.ts" {
		t.Error("expected trimmed path")
	}
}
