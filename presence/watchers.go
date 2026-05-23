package presence

import (
	"errors"
	"sync"
)

// MaxSubscriptionsPerClient caps how many users a single client can watch.
// Prevents a malicious / buggy client from exploding server memory.
const MaxSubscriptionsPerClient = 1000

var ErrSubscriptionLimit = errors.New("subscription limit reached")

// watcherIndex maintains a bidirectional mapping between watchers and the users
// they observe. Both directions are kept consistent under a single mutex.
//
//	watched -> set of watchers  : used when an event for `watched` arrives and
//	                              we need to find local clients to deliver to.
//	watcher -> set of watched   : used on watcher disconnect to clean up O(N).
type watcherIndex struct {
	mu          sync.RWMutex
	watchersOf  map[string]map[string]struct{} // watched -> watchers
	watchedBy   map[string]map[string]struct{} // watcher -> watched
}

func newWatcherIndex() *watcherIndex {
	return &watcherIndex{
		watchersOf: make(map[string]map[string]struct{}),
		watchedBy:  make(map[string]map[string]struct{}),
	}
}

// Add registers watcher's interest in watched. Returns ErrSubscriptionLimit
// if watcher already observes MaxSubscriptionsPerClient distinct users.
// Idempotent: re-adding an existing pair is a no-op and does not count.
func (w *watcherIndex) Add(watcher, watched string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if existing, ok := w.watchedBy[watcher]; ok {
		if _, already := existing[watched]; already {
			return nil
		}
		if len(existing) >= MaxSubscriptionsPerClient {
			return ErrSubscriptionLimit
		}
	}

	if w.watchersOf[watched] == nil {
		w.watchersOf[watched] = make(map[string]struct{})
	}
	w.watchersOf[watched][watcher] = struct{}{}

	if w.watchedBy[watcher] == nil {
		w.watchedBy[watcher] = make(map[string]struct{})
	}
	w.watchedBy[watcher][watched] = struct{}{}

	return nil
}

// RemoveWatcher drops watcher from every watched set it appears in. Called on
// stream close so we don't leak interest entries.
func (w *watcherIndex) RemoveWatcher(watcher string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	watched, ok := w.watchedBy[watcher]
	if !ok {
		return
	}
	for target := range watched {
		if peers, exists := w.watchersOf[target]; exists {
			delete(peers, watcher)
			if len(peers) == 0 {
				delete(w.watchersOf, target)
			}
		}
	}
	delete(w.watchedBy, watcher)
}

// WatchersOf returns a snapshot of watcher IDs interested in `watched`.
// Returned slice is safe to use after the mutex is released.
func (w *watcherIndex) WatchersOf(watched string) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	peers, ok := w.watchersOf[watched]
	if !ok || len(peers) == 0 {
		return nil
	}
	out := make([]string, 0, len(peers))
	for id := range peers {
		out = append(out, id)
	}
	return out
}

// CountFor returns how many users `watcher` is currently subscribed to.
// Useful for tests; not on the hot path.
func (w *watcherIndex) CountFor(watcher string) int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.watchedBy[watcher])
}
