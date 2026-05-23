package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/go-redis/redis/v8"
)

// PresenceCallback is invoked on every PresenceEvent received from Redis,
// already enriched with the list of locally-connected watchers interested in
// ev.UserID. If watchers is empty the callback should be a no-op.
type PresenceCallback func(ev PresenceEvent, watchers []string)

// TypingCallback is invoked on every TypingEvent received from Redis. The
// callback should fan out to whichever recipients in ev.To are connected to
// this node.
type TypingCallback func(ev TypingEvent)

// Manager is the public surface for the presence subsystem. One instance per
// server process. Safe for concurrent use.
type Manager struct {
	store    *store
	watchers *watcherIndex

	startOnce sync.Once
	startErr  error

	cbMu       sync.RWMutex
	onPresence PresenceCallback
	onTyping   TypingCallback

	// pubsub handles are retained for clean shutdown.
	presenceSub *redis.PubSub
	typingSub   *redis.PubSub
}

func NewManager(rdb *redis.Client, nodeID string) *Manager {
	return &Manager{
		store:    newStore(rdb, nodeID),
		watchers: newWatcherIndex(),
	}
}

// Start launches the two subscriber goroutines. Subsequent calls are no-ops.
// The goroutines exit when ctx is canceled.
func (m *Manager) Start(ctx context.Context) error {
	m.startOnce.Do(func() {
		m.presenceSub = m.store.rdb.Subscribe(ctx, PresenceChannel)
		m.typingSub = m.store.rdb.Subscribe(ctx, TypingChannel)

		// Block on first receive to surface immediate connection errors.
		if _, err := m.presenceSub.Receive(ctx); err != nil {
			m.startErr = fmt.Errorf("subscribe presence channel: %w", err)
			return
		}
		if _, err := m.typingSub.Receive(ctx); err != nil {
			m.startErr = fmt.Errorf("subscribe typing channel: %w", err)
			return
		}

		go m.consumePresence(ctx)
		go m.consumeTyping(ctx)
	})
	return m.startErr
}

func (m *Manager) consumePresence(ctx context.Context) {
	ch := m.presenceSub.Channel()
	for {
		select {
		case <-ctx.Done():
			_ = m.presenceSub.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev PresenceEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				log.Printf("presence: malformed presence event: %v", err)
				continue
			}
			watchers := m.watchers.WatchersOf(ev.UserID)
			if len(watchers) == 0 {
				continue
			}
			m.cbMu.RLock()
			cb := m.onPresence
			m.cbMu.RUnlock()
			if cb != nil {
				cb(ev, watchers)
			}
		}
	}
}

func (m *Manager) consumeTyping(ctx context.Context) {
	ch := m.typingSub.Channel()
	for {
		select {
		case <-ctx.Done():
			_ = m.typingSub.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev TypingEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				log.Printf("presence: malformed typing event: %v", err)
				continue
			}
			m.cbMu.RLock()
			cb := m.onTyping
			m.cbMu.RUnlock()
			if cb != nil {
				cb(ev)
			}
		}
	}
}

func (m *Manager) OnPresenceEvent(fn PresenceCallback) {
	m.cbMu.Lock()
	m.onPresence = fn
	m.cbMu.Unlock()
}

func (m *Manager) OnTypingEvent(fn TypingCallback) {
	m.cbMu.Lock()
	m.onTyping = fn
	m.cbMu.Unlock()
}

func (m *Manager) MarkOnline(ctx context.Context, userID string) error {
	return m.store.MarkOnline(ctx, userID)
}

func (m *Manager) MarkOffline(ctx context.Context, userID string) error {
	return m.store.MarkOffline(ctx, userID)
}

func (m *Manager) RefreshTTL(ctx context.Context, userID string) error {
	return m.store.RefreshTTL(ctx, userID)
}

func (m *Manager) IsOnline(ctx context.Context, userID string) (bool, error) {
	return m.store.IsOnline(ctx, userID)
}

func (m *Manager) PublishTyping(ctx context.Context, ev TypingEvent) error {
	return m.store.PublishTyping(ctx, ev)
}

// AddWatcher registers watcher's interest in watched. Returns
// ErrSubscriptionLimit if the watcher exceeds MaxSubscriptionsPerClient.
func (m *Manager) AddWatcher(watcher, watched string) error {
	return m.watchers.Add(watcher, watched)
}

// RemoveAllWatchers drops every interest entry the watcher holds.
func (m *Manager) RemoveAllWatchers(watcher string) {
	m.watchers.RemoveWatcher(watcher)
}

// CountFor reports how many users the watcher is currently subscribed to.
func (m *Manager) CountFor(watcher string) int {
	return m.watchers.CountFor(watcher)
}

// IsSubscriptionLimit reports whether err originated from the per-client
// subscription cap. Useful for the server to map to a gRPC status code.
func IsSubscriptionLimit(err error) bool {
	return errors.Is(err, ErrSubscriptionLimit)
}
