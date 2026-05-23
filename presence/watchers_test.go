package presence

import (
	"fmt"
	"sync"
	"testing"
)

func TestWatcherIndex_AddIsIdempotent(t *testing.T) {
	idx := newWatcherIndex()
	if err := idx.Add("alice", "bob"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := idx.Add("alice", "bob"); err != nil {
		t.Fatalf("re-Add: %v", err)
	}
	if got, want := idx.CountFor("alice"), 1; got != want {
		t.Fatalf("CountFor(alice) = %d, want %d", got, want)
	}
	if peers := idx.WatchersOf("bob"); len(peers) != 1 || peers[0] != "alice" {
		t.Fatalf("WatchersOf(bob) = %v, want [alice]", peers)
	}
}

func TestWatcherIndex_SubscriptionLimit(t *testing.T) {
	idx := newWatcherIndex()
	for i := 0; i < MaxSubscriptionsPerClient; i++ {
		if err := idx.Add("alice", fmt.Sprintf("u%d", i)); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	err := idx.Add("alice", "overflow")
	if err == nil {
		t.Fatal("expected ErrSubscriptionLimit, got nil")
	}
	if !IsSubscriptionLimit(err) {
		t.Fatalf("expected ErrSubscriptionLimit, got %v", err)
	}
	if got, want := idx.CountFor("alice"), MaxSubscriptionsPerClient; got != want {
		t.Fatalf("CountFor(alice) = %d, want %d", got, want)
	}
}

func TestWatcherIndex_RemoveCleansBothDirections(t *testing.T) {
	idx := newWatcherIndex()
	_ = idx.Add("alice", "bob")
	_ = idx.Add("alice", "carol")
	_ = idx.Add("dan", "bob")

	idx.RemoveWatcher("alice")

	if got := idx.CountFor("alice"); got != 0 {
		t.Fatalf("CountFor(alice) after remove = %d, want 0", got)
	}
	peers := idx.WatchersOf("bob")
	if len(peers) != 1 || peers[0] != "dan" {
		t.Fatalf("WatchersOf(bob) after remove = %v, want [dan]", peers)
	}
	if peers := idx.WatchersOf("carol"); len(peers) != 0 {
		t.Fatalf("WatchersOf(carol) after remove = %v, want []", peers)
	}
}

func TestWatcherIndex_ConcurrentAddRemove(t *testing.T) {
	idx := newWatcherIndex()
	const workers = 32
	const opsPerWorker = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()
			watcher := fmt.Sprintf("w%d", id)
			for i := 0; i < opsPerWorker; i++ {
				target := fmt.Sprintf("t%d", i%50)
				_ = idx.Add(watcher, target)
				_ = idx.WatchersOf(target)
			}
			idx.RemoveWatcher(watcher)
		}(w)
	}
	wg.Wait()

	for w := 0; w < workers; w++ {
		if got := idx.CountFor(fmt.Sprintf("w%d", w)); got != 0 {
			t.Fatalf("watcher w%d not cleaned up: %d entries remain", w, got)
		}
	}
	for i := 0; i < 50; i++ {
		if peers := idx.WatchersOf(fmt.Sprintf("t%d", i)); len(peers) != 0 {
			t.Fatalf("t%d still has watchers: %v", i, peers)
		}
	}
}

func TestWatcherIndex_ReAddAfterLimitFails(t *testing.T) {
	idx := newWatcherIndex()
	for i := 0; i < MaxSubscriptionsPerClient; i++ {
		_ = idx.Add("alice", fmt.Sprintf("u%d", i))
	}
	if err := idx.Add("alice", "u0"); err != nil {
		t.Fatalf("re-Add of existing pair at cap should succeed (idempotent), got %v", err)
	}
}
