package collections

import "testing"

func TestLRUMap_SetGet(t *testing.T) {
	m := NewLRUMap[string, int](3)
	m.Set("a", 1)
	m.Set("b", 2)

	v, ok := m.Get("a")
	if !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v; want 1, true", v, ok)
	}
}

func TestLRUMap_Eviction(t *testing.T) {
	m := NewLRUMap[string, int](2)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3) // evicts "a"

	if _, ok := m.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if v, ok := m.Get("b"); !ok || v != 2 {
		t.Error("expected 'b' to still exist")
	}
	if v, ok := m.Get("c"); !ok || v != 3 {
		t.Error("expected 'c' to exist")
	}
}

func TestLRUMap_AccessPreservesOrder(t *testing.T) {
	m := NewLRUMap[string, int](2)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Get("a")    // Touch "a", making "b" the oldest
	m.Set("c", 3) // Should evict "b", not "a"

	if _, ok := m.Get("a"); !ok {
		t.Error("expected 'a' preserved after access")
	}
	if _, ok := m.Get("b"); ok {
		t.Error("expected 'b' evicted")
	}
}

func TestLRUMap_Delete(t *testing.T) {
	m := NewLRUMap[string, int](3)
	m.Set("a", 1)
	m.Delete("a")
	if m.Len() != 0 {
		t.Errorf("expected len 0, got %d", m.Len())
	}
}

func TestLRUMap_Update(t *testing.T) {
	m := NewLRUMap[string, int](3)
	m.Set("a", 1)
	m.Set("a", 2)
	v, _ := m.Get("a")
	if v != 2 {
		t.Errorf("expected 2 after update, got %d", v)
	}
	if m.Len() != 1 {
		t.Errorf("expected len 1, got %d", m.Len())
	}
}

func TestLRUMap_Clear(t *testing.T) {
	m := NewLRUMap[string, int](3)
	m.Set("a", 1)
	m.Set("b", 2)
	m.Clear()
	if m.Len() != 0 {
		t.Errorf("expected len 0 after clear, got %d", m.Len())
	}
}
