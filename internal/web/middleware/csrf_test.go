package middleware

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCSRFStore_ValidTokenAccepted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewCSRFStore(ctx)

	token := store.Generate()
	if !store.Validate(token) {
		t.Error("Validate rejected a freshly generated token")
	}
	// Multi-use: same token accepted again.
	if !store.Validate(token) {
		t.Error("Validate rejected the token on second use (should be multi-use)")
	}
}

func TestCSRFStore_UnknownTokenRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewCSRFStore(ctx)

	if store.Validate("not-a-real-token") {
		t.Error("Validate accepted an unknown token")
	}
	if store.Validate("") {
		t.Error("Validate accepted an empty token")
	}
}

func TestCSRFStore_ExpiredTokenRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewCSRFStore(ctx)

	token := store.Generate()
	// Manually backdate the expiry so the token appears expired.
	h := csrfTokenHash(token)
	store.mu.Lock()
	store.tokens[h] = time.Now().Add(-2 * time.Hour)
	store.mu.Unlock()

	if store.Validate(token) {
		t.Error("Validate accepted an expired token")
	}
}

func TestCSRFStore_ConcurrentValidateRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewCSRFStore(ctx)

	token := store.Generate()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if !store.Validate(token) {
				t.Errorf("Validate returned false for valid token under concurrent load")
			}
		}()
	}
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			_ = store.Generate()
		}()
	}
	wg.Wait()
}
