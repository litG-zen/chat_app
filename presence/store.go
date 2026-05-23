package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	PresenceKeyPrefix = "presence:"
	PresenceChannel   = "presence:events"
	TypingChannel     = "typing:events"

	// TTL on the presence key; refreshed by heartbeat. If a server crashes
	// mid-stream, the key expires within this window and the user flips to
	// offline. Refresh interval must be < TTL/2 to tolerate one missed tick.
	PresenceTTL     = 30 * time.Second
	RefreshInterval = 10 * time.Second
)

type store struct {
	rdb    *redis.Client
	nodeID string
}

func newStore(rdb *redis.Client, nodeID string) *store {
	return &store{rdb: rdb, nodeID: nodeID}
}

func presenceKey(userID string) string {
	return PresenceKeyPrefix + userID
}

func (s *store) MarkOnline(ctx context.Context, userID string) error {
	if err := s.rdb.Set(ctx, presenceKey(userID), s.nodeID, PresenceTTL).Err(); err != nil {
		return fmt.Errorf("set presence key for %s: %w", userID, err)
	}
	return s.publishPresence(ctx, PresenceEvent{
		UserID:    userID,
		State:     StateOnline,
		Timestamp: nowMillis(),
		NodeID:    s.nodeID,
	})
}

func (s *store) MarkOffline(ctx context.Context, userID string) error {
	if err := s.rdb.Del(ctx, presenceKey(userID)).Err(); err != nil {
		return fmt.Errorf("del presence key for %s: %w", userID, err)
	}
	return s.publishPresence(ctx, PresenceEvent{
		UserID:    userID,
		State:     StateOffline,
		Timestamp: nowMillis(),
		NodeID:    s.nodeID,
	})
}

func (s *store) RefreshTTL(ctx context.Context, userID string) error {
	ok, err := s.rdb.Expire(ctx, presenceKey(userID), PresenceTTL).Result()
	if err != nil {
		return fmt.Errorf("expire presence key for %s: %w", userID, err)
	}
	if !ok {
		// Key vanished — likely TTL elapsed because the heartbeat fell behind.
		// Restore it so we don't flap the user offline.
		if err := s.rdb.Set(ctx, presenceKey(userID), s.nodeID, PresenceTTL).Err(); err != nil {
			return fmt.Errorf("restore presence key for %s: %w", userID, err)
		}
	}
	return nil
}

func (s *store) IsOnline(ctx context.Context, userID string) (bool, error) {
	n, err := s.rdb.Exists(ctx, presenceKey(userID)).Result()
	if err != nil {
		return false, fmt.Errorf("exists presence key for %s: %w", userID, err)
	}
	return n > 0, nil
}

func (s *store) PublishTyping(ctx context.Context, ev TypingEvent) error {
	ev.NodeID = s.nodeID
	if ev.Timestamp == 0 {
		ev.Timestamp = nowMillis()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal typing event: %w", err)
	}
	if err := s.rdb.Publish(ctx, TypingChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish typing event: %w", err)
	}
	return nil
}

func (s *store) publishPresence(ctx context.Context, ev PresenceEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal presence event: %w", err)
	}
	if err := s.rdb.Publish(ctx, PresenceChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish presence event: %w", err)
	}
	return nil
}

func nowMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
