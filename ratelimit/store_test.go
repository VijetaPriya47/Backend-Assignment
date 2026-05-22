package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestTryAccept_BasicWindow(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxRequests; i++ {
		if !s.TryAccept("alice") {
			t.Fatalf("request %d should have been accepted", i+1)
		}
	}
	if s.TryAccept("alice") {
		t.Fatal("6th request should have been rejected")
	}
}

func TestTryAccept_DifferentUsersAreIndependent(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxRequests; i++ {
		s.TryAccept("alice")
	}
	if !s.TryAccept("bob") {
		t.Fatal("bob should not be affected by alice's rate limit")
	}
}

func TestTryAccept_WindowReset(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxRequests; i++ {
		s.TryAccept("alice")
	}
	if s.TryAccept("alice") {
		t.Fatal("should be rate-limited within window")
	}

	// Use a very short window for this sub-test to avoid time.Sleep.
	// We directly manipulate the unexported windowStart under the user's own
	// lock — acceptable in a white-box test within the same package.
	s.mu.Lock()
	s.users["alice"].windowStart = time.Now().Add(-WindowDuration - time.Second)
	s.mu.Unlock()

	if !s.TryAccept("alice") {
		t.Fatal("should be accepted after window expiry")
	}
}

func TestTryAccept_Concurrent_ExactlyMaxAccepted(t *testing.T) {
	s := NewStore()

	const goroutines = 100
	var accepted int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.TryAccept("concurrent-user") {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if accepted != MaxRequests {
		t.Fatalf("expected exactly %d accepted under concurrency, got %d", MaxRequests, accepted)
	}
}

func TestTryAccept_RejectedCounterIsCumulative(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxRequests+3; i++ {
		s.TryAccept("alice")
	}
	stats := s.AllStats()
	if stats["alice"].RejectedTotal != 3 {
		t.Errorf("expected 3 cumulative rejections, got %d", stats["alice"].RejectedTotal)
	}
}

func TestAllStats_ReturnsAllUsers(t *testing.T) {
	s := NewStore()
	s.TryAccept("alice")
	s.TryAccept("alice")
	s.TryAccept("bob")

	stats := s.AllStats()
	if stats["alice"].AcceptedInWindow != 2 {
		t.Errorf("alice: expected 2 accepted, got %d", stats["alice"].AcceptedInWindow)
	}
	if stats["bob"].AcceptedInWindow != 1 {
		t.Errorf("bob: expected 1 accepted, got %d", stats["bob"].AcceptedInWindow)
	}
}

func TestAllStats_ExpiredWindowShowsZero(t *testing.T) {
	s := NewStore()
	s.TryAccept("alice")

	s.mu.Lock()
	s.users["alice"].windowStart = time.Now().Add(-WindowDuration - time.Second)
	s.mu.Unlock()

	stats := s.AllStats()
	if stats["alice"].AcceptedInWindow != 0 {
		t.Errorf("expected 0 accepted in expired window, got %d", stats["alice"].AcceptedInWindow)
	}
}
