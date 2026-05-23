package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	redisPingTimeout  = 5 * time.Second
	redisWriteTimeout = 5 * time.Second
	redisReadTimeout  = 10 * time.Second
)

var (
	redisClient *RedisClient
	redisOnce   sync.Once
	REDIS_URL   = os.Getenv("REDIS_URL")
)

type RedisClient struct {
	client *redis.Client
	ctx    context.Context
	mutex  sync.RWMutex
}

type RedisMessage struct {
	Sender    string `json:"sender"`
	Receiver  string `json:"receiver"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

func (r *RedisClient) initialize() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	fmt.Printf("REDIS_URL :%v", REDIS_URL)
	if REDIS_URL == "" {
		return fmt.Errorf("REDIS URL is not defined")
	}

	opt, err := redis.ParseURL(REDIS_URL)
	if err != nil {
		return fmt.Errorf("Failed to parse REDIS URL : %v", REDIS_URL)

	}

	r.client = redis.NewClient(opt)

	pingCtx, cancel := context.WithTimeout(r.ctx, redisPingTimeout)
	defer cancel()
	if _, err := r.client.Ping(pingCtx).Result(); err != nil {
		return fmt.Errorf("failed to connect to redis instance: %w", err)
	}

	return nil
}

func NewRedisClient() (*RedisClient, error) {
	var initErr error
	redisOnce.Do(func() {
		redisClient = &RedisClient{ctx: context.Background()}
		initErr = redisClient.initialize()
	})

	if initErr != nil {
		return nil, initErr
	}
	return redisClient, nil
}

func GetRedisInstance() (*RedisClient, error) {
	if redisClient == nil {
		return NewRedisClient()
	}
	return redisClient, nil
}

// Client exposes the underlying go-redis client for callers that need pub/sub
// or other primitives not surfaced by this wrapper. Returns nil if the
// connection has not yet been initialised.
func (r *RedisClient) Client() *redis.Client {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.client
}

// AddMessageForUser adds a serialized message to the recipient's Redis list.
func AddMessageForUser(msg RedisMessage) error {
	rdb, err := GetRedisInstance()
	if err != nil {
		return fmt.Errorf("redis connection issue: %w", err)
	}
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	ctx, cancel := context.WithTimeout(rdb.ctx, redisWriteTimeout)
	defer cancel()
	key := "undelivered:" + msg.Receiver
	if err := rdb.client.RPush(ctx, key, jsonMsg).Err(); err != nil {
		return fmt.Errorf("rpush %s: %w", key, err)
	}
	return nil
}

// FlushMessagesForUser fetches all undelivered messages for a user and deletes the list.
// Returns the slice of messages (may be empty) or an error.
func FlushMessagesForUser(userID string) ([]RedisMessage, error) {
	rdb, err := GetRedisInstance()
	if err != nil {
		return nil, fmt.Errorf("failed to get redis instance: %w", err)
	}
	if rdb == nil || rdb.client == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(rdb.ctx, redisReadTimeout)
	defer cancel()

	key := "undelivered:" + userID

	// LRange + Del executed in a MULTI/EXEC transaction so messages can't be
	// pushed between the read and the delete (any concurrent RPush after EXEC
	// stays in the new list and is delivered next time).
	var lrangeCmd *redis.StringSliceCmd
	if _, err := rdb.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		lrangeCmd = p.LRange(ctx, key, 0, -1)
		p.Del(ctx, key)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("flush pipeline for %s: %w", key, err)
	}

	msgsJson, err := lrangeCmd.Result()
	if err != nil {
		return nil, fmt.Errorf("lrange %s: %w", key, err)
	}

	msgs := make([]RedisMessage, 0, len(msgsJson))
	for _, m := range msgsJson {
		var msg RedisMessage
		if err := json.Unmarshal([]byte(m), &msg); err != nil {
			// skip malformed messages but continue
			continue
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}
