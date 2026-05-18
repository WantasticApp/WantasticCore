package cache

import (
	"context"
	"encoding/json"
	"time"

	"WantasticCore/internal/store"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	peerPrefix  = "peer:"
	peerListTTL = 5 * time.Minute
)

// cachedPeerRepository wraps a PeerRepository with Redis caching.
type cachedPeerRepository struct {
	store.PeerRepository
	redis *redis.Client
}

// NewCachedPeerRepository creates a new Redis-cached peer repository.
func NewCachedPeerRepository(repo store.PeerRepository, redis *redis.Client) store.PeerRepository {
	return &cachedPeerRepository{
		PeerRepository: repo,
		redis:          redis,
	}
}

func (r *cachedPeerRepository) Save(peer *store.PeerData) error {
	if err := r.PeerRepository.Save(peer); err != nil {
		return err
	}
	// Invalidate list cache for this account
	r.invalidateList(peer.AccountID)
	return nil
}

func (r *cachedPeerRepository) Get(accountID, peerID string) (*store.PeerData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := peerPrefix + accountID + ":" + peerID
	data, err := r.redis.Get(ctx, key).Bytes()
	if err == nil {
		var peer store.PeerData
		if json.Unmarshal(data, &peer) == nil {
			return &peer, nil
		}
	}

	// Fallback to database
	peer, err := r.PeerRepository.Get(accountID, peerID)
	if err != nil {
		return nil, err
	}

	// Cache it
	if peer != nil {
		r.cachePeer(peer)
	}

	return peer, nil
}

func (r *cachedPeerRepository) List(accountID string) ([]*store.PeerData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := peerPrefix + "list:" + accountID
	data, err := r.redis.Get(ctx, key).Bytes()
	if err == nil {
		var peers []*store.PeerData
		if json.Unmarshal(data, &peers) == nil {
			return peers, nil
		}
	}

	// Fallback to database
	peers, err := r.PeerRepository.List(accountID)
	if err != nil {
		return nil, err
	}

	// Cache it
	cacheData, err := json.Marshal(peers)
	if err == nil {
		r.redis.Set(ctx, key, cacheData, peerListTTL)
	} else {
		log.Error().Err(err).Msg("Failed to marshal peer list for cache")
	}

	return peers, nil
}

// Count always bypasses the Redis cache and hits the database directly.
// This is intentional: it is used for authoritative peer-limit enforcement
// where a stale cached count could allow limit bypass.
func (r *cachedPeerRepository) Count(accountID string) (int, error) {
	return r.PeerRepository.Count(accountID)
}

func (r *cachedPeerRepository) Delete(accountID, peerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r.redis.Del(ctx, peerPrefix+accountID+":"+peerID)
	r.invalidateList(accountID)

	return r.PeerRepository.Delete(accountID, peerID)
}

func (r *cachedPeerRepository) DeleteByAccount(accountID string) error {
	r.invalidateList(accountID)
	return r.PeerRepository.DeleteByAccount(accountID)
}

func (r *cachedPeerRepository) cachePeer(peer *store.PeerData) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := json.Marshal(peer)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal peer for cache")
		return
	}

	key := peerPrefix + peer.AccountID + ":" + peer.ID
	if err := r.redis.Set(ctx, key, data, peerListTTL).Err(); err != nil {
		log.Error().Err(err).Msg("Failed to cache peer")
	}
}

func (r *cachedPeerRepository) invalidateList(accountID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Delete list cache
	r.redis.Del(ctx, peerPrefix+"list:"+accountID)
}
