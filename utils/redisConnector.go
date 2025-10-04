package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/go-redis/redis/v8"
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

	// Test connection
	_, err = r.client.Ping(r.ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to redis instance %v", err)
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

// AddMessageForUser adds a serialized message to the recipient's Redis list
func AddMessageForUser(msg RedisMessage) error {
	rdb, err := GetRedisInstance()
	if err != nil {
		return fmt.Errorf("Redis connection issue, please check %v", err)
	}
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := "undelivered:" + msg.Receiver
	return rdb.client.RPush(rdb.ctx, key, jsonMsg).Err()
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

	key := "undelivered:" + userID

	// fetch all messages
	msgsJson, err := rdb.client.LRange(rdb.ctx, key, 0, -1).Result()
	if err != nil {
		// If key does not exist, LRange returns nil slice and nil error, so we typically won't hit this,
		// but handle any other redis errors here.
		return nil, fmt.Errorf("failed to lrange key %s: %w", key, err)
	}

	var msgs []RedisMessage
	for _, m := range msgsJson {
		var msg RedisMessage
		if err := json.Unmarshal([]byte(m), &msg); err != nil {
			// skip malformed messages but continue
			continue
		}
		msgs = append(msgs, msg)
	}

	// delete the list atomically after reading
	if err := rdb.client.Del(rdb.ctx, key).Err(); err != nil {
		// we read the messages successfully — return them but also surface the delete error
		return msgs, fmt.Errorf("fetched %d messages but failed to delete key %s: %w", len(msgs), key, err)
	}

	return msgs, nil
}
