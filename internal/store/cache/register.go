// Package cache registers Redis cache decorator implementations.
package cache

import (
	"WantasticCore/internal/store"

	"github.com/redis/go-redis/v9"
)

func init() {
	// Register cache decorator constructors with the store package
	store.RegisterCacheDecorators(
		func(repo store.SessionRepository, redis *redis.Client) store.SessionRepository {
			return NewCachedSessionRepository(repo, redis)
		},
		func(repo store.PeerRepository, redis *redis.Client) store.PeerRepository {
			return NewCachedPeerRepository(repo, redis)
		},
	)
}
