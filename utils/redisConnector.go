package utils

import (
	"context"
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

func (r *RedisClient) initialize() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

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
