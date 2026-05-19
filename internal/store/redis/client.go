package redis

import (
	"context"
	"fmt"
	"time"

	"WantasticCore/internal/config"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// NewClient creates a new Redis client.
func NewClient(cfg config.RedisConfig) (*redis.Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	opts := &redis.Options{
		Addr:     cfg.Addr,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	}

	client := redis.NewClient(opts)

	// Ping to check connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	log.Debug().Str("addr", cfg.Addr).Int("db", cfg.DB).Msg(" Connected to Redis")
	return client, nil
}
