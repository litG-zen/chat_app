package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-redis/redis/v8"
)

var (
	redisClient *RedisClient
	redisOnce   sync.Once
	REDIS_URL   = "redis://localhost:6379" //os.Getenv("REDIS_URL")
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

// FlushMessagesForUser fetches all undelivered messages for a user and deletes the list
func FlushMessagesForUser(userID string) ([]RedisMessage, error) {

	rdb, err := GetRedisInstance()

	key := "undelivered:" + userID
	msgsJson, err := rdb.client.LRange(rdb.ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var msgs []RedisMessage
	for _, m := range msgsJson {
		var msg RedisMessage
		if err := json.Unmarshal([]byte(m), &msg); err == nil {
			msgs = append(msgs, msg)
		}
	}

	// Delete the messages after fetching
	err = rdb.client.Del(rdb.ctx, key).Err()
	if err != nil {
		return nil, err
	}
	return msgs, nil
}
