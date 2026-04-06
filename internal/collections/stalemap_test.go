package collections

import (
	"testing"
	"time"
)

func TestStaleMap_SetGet(t *testing.T) {
	m := NewStaleExpirationMap[string, int](1*time.Second, 500*time.Millisecond)
	defer m.Stop()

	m.Set("a", 1)
	v, ok := m.Get("a")
	if !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v; want 1, true", v, ok)
	}
}

func TestStaleMap_Expiration(t *testing.T) {
	m := NewStaleExpirationMap[string, int](100*time.Millisecond, 50*time.Millisecond)
	defer m.Stop()

	m.Set("a", 1)
	time.Sleep(200 * time.Millisecond)

	if _, ok := m.Get("a"); ok {
		t.Error("expected 'a' to be expired")
	}
}

func TestStaleMap_Delete(t *testing.T) {
	m := NewStaleExpirationMap[string, int](1*time.Second, 500*time.Millisecond)
	defer m.Stop()

	m.Set("a", 1)
	m.Delete("a")
	if _, ok := m.Get("a"); ok {
		t.Error("expected 'a' deleted")
	}
}

func TestStaleMap_Len(t *testing.T) {
	m := NewStaleExpirationMap[string, int](1*time.Second, 500*time.Millisecond)
	defer m.Stop()

	m.Set("a", 1)
	m.Set("b", 2)
	if m.Len() != 2 {
		t.Errorf("expected len 2, got %d", m.Len())
	}
}
