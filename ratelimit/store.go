package ratelimit

import (
	"errors"
	"sync"
	"time"
)

const (
	// WindowDuration is the fixed 1-minute window per user.
	WindowDuration = time.Minute
	// MaxRequests is the maximum number of accepted requests per window per user.
	MaxRequests = 5
)

// Sentinel errors returned by Store methods.
var ErrRateLimited = errors.New("rate limit exceeded")

// userState holds rate-limit counters for a single user_id.
//
// mu is an RWMutex so snapshot (read-only) can run concurrently with other
// snapshots. tryAccept always needs a write lock because it may mutate state.
type userState struct {
	mu            sync.RWMutex
	windowStart   time.Time
	acceptedCount int
	rejectedTotal int // cumulative across all windows; never resets
}

// tryAccept checks the window and either accepts or rejects the incoming request.
// Returns true if the request is within the limit.
func (u *userState) tryAccept(now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	if now.Sub(u.windowStart) >= WindowDuration {
		u.windowStart = now
		u.acceptedCount = 0
	}

	if u.acceptedCount < MaxRequests {
		u.acceptedCount++
		return true
	}

	u.rejectedTotal++
	return false
}

// snapshot returns a point-in-time copy of the user's stats.
// Uses RLock because it only reads.
func (u *userState) snapshot(now time.Time) UserStats {
	u.mu.RLock()
	defer u.mu.RUnlock()

	accepted := u.acceptedCount
	if now.Sub(u.windowStart) >= WindowDuration {
		accepted = 0
	}

	return UserStats{
		AcceptedInWindow: accepted,
		RejectedTotal:    u.rejectedTotal,
	}
}

// UserStats is the exported, JSON-serialisable view of a user's counters.
type UserStats struct {
	AcceptedInWindow int `json:"accepted_in_current_window"`
	RejectedTotal    int `json:"rejected_total"`
}

// Store is the top-level in-memory rate-limit store.
//
// Concurrency model:
//   - mu (RWMutex) guards the users map itself (reads: RLock, writes: Lock).
//   - Each userState has its own RWMutex, so requests for different user_ids
//     never contend with each other.
//   - Entries are never deleted, so there is no ABA / use-after-free risk on
//     the pointer returned by getOrCreate.
type Store struct {
	mu    sync.RWMutex
	users map[string]*userState
}

// NewStore returns an initialised, empty Store.
func NewStore() *Store {
	return &Store{users: make(map[string]*userState)}
}

// getOrCreate returns the userState for id, creating it if absent.
// Uses double-checked locking to avoid taking a write lock on the hot path.
func (s *Store) getOrCreate(id string) *userState {
	s.mu.RLock()
	u, ok := s.users[id]
	s.mu.RUnlock()
	if ok {
		return u
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok = s.users[id]; ok { // another goroutine may have beaten us
		return u
	}
	u = &userState{windowStart: time.Now()}
	s.users[id] = u
	return u
}

// TryAccept attempts to accept a request for userID.
func (s *Store) TryAccept(userID string) bool {
	return s.getOrCreate(userID).tryAccept(time.Now())
}

// AllStats returns a snapshot of every user's statistics.
// Holds the store RLock for the entire iteration to avoid per-entry lock churn.
func (s *Store) AllStats() map[string]UserStats {
	now := time.Now()

	s.mu.RLock()
	result := make(map[string]UserStats, len(s.users))
	for id, u := range s.users {
		result[id] = u.snapshot(now)
	}
	s.mu.RUnlock()

	return result
}
