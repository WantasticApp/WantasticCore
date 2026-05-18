package store

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

var (
	db   *Database
	once sync.Once
)

// Database is the singleton database manager providing access to all repositories.
type Database struct {
	pg    *pg.DB
	redis *redis.Client

	// Lazy-initialized repositories
	accounts        AccountRepository
	tenants         TenantRepository
	sessions        SessionRepository
	peers           PeerRepository
	groups          GroupRepository
	acl             ACLRepository
	ipam            IPAMRepository
	wuspDeviceState  WUSPDeviceStateRepository
	deviceSnapshots  DeviceSnapshotRepository

	mu sync.RWMutex
}

// Config holds database configuration.
type Config struct {
	// PostgreSQL
	Host         string
	Port         int
	User         string
	Password     string
	Database     string
	SSLMode      string
	PoolSize     int
	MinIdleConns int
	MaxRetries   int

	// Redis (optional)
	RedisEnabled  bool
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

// Initialize creates the singleton database manager.
// Must be called once at application startup.
func Initialize(cfg Config) error {
	var initErr error
	once.Do(func() {
		db, initErr = newDatabase(cfg)
	})
	return initErr
}

// DB returns the singleton database instance.
// Panics if Initialize() was not called.
func DB() *Database {
	if db == nil {
		panic("store: database not initialized - call Initialize() first")
	}
	return db
}

// IsInitialized returns true if the database has been initialized.
func IsInitialized() bool {
	return db != nil
}

func newDatabase(cfg Config) (*Database, error) {
	// Configure PostgreSQL connection
	opts := &pg.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		User:         cfg.User,
		Password:     cfg.Password,
		Database:     cfg.Database,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
	}

	if cfg.SSLMode != "" && cfg.SSLMode != "disable" {
		// Postgres `sslmode` semantics:
		//   require           — encrypt only, no cert verification
		//   verify-ca / verify-full — verify cert; needs ServerName
		// Go's TLS lib refuses to handshake without one of ServerName or
		// InsecureSkipVerify, so fill ServerName from the host for verify-*
		// modes and skip verification for "require"/anything else.
		tlsCfg := &tls.Config{}
		switch cfg.SSLMode {
		case "verify-ca", "verify-full":
			tlsCfg.ServerName = cfg.Host
		default:
			tlsCfg.InsecureSkipVerify = true
		}
		opts.TLSConfig = tlsCfg
	}

	pgDB := pg.Connect(opts)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := pgDB.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	log.Debug().
		Str("host", cfg.Host).
		Int("port", cfg.Port).
		Str("database", cfg.Database).
		Int("pool_size", cfg.PoolSize).
		Msg(" Connected to PostgreSQL")

	d := &Database{pg: pgDB}

	// Connect to Redis if enabled
	if cfg.RedisEnabled && cfg.RedisAddr != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Warn().Err(err).Msg(" Redis connection failed - caching disabled")
		} else {
			d.redis = redisClient
			log.Debug().Str("addr", cfg.RedisAddr).Msg(" Connected to Redis")
		}
	}

	return d, nil
}

// Close closes all database connections.
func (d *Database) Close() error {
	if d.redis != nil {
		if err := d.redis.Close(); err != nil {
			log.Warn().Err(err).Msg("Error closing Redis connection")
		}
	}
	return d.pg.Close()
}

// PG returns the underlying PostgreSQL connection for advanced operations.
func (d *Database) PG() *pg.DB {
	return d.pg
}

// Redis returns the Redis client (may be nil if disabled).
func (d *Database) Redis() *redis.Client {
	return d.redis
}

// HasRedis returns true if Redis is available.
func (d *Database) HasRedis() bool {
	return d.redis != nil
}

// =============================================================================
// Repository Accessors
// =============================================================================

// Accounts returns the account repository.
func (d *Database) Accounts() AccountRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.accounts == nil {
		d.accounts = newAccountRepository(d.pg)
	}
	return d.accounts
}

// Tenants returns the tenant repository.
func (d *Database) Tenants() TenantRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tenants == nil {
		d.tenants = newTenantRepository(d.pg)
	}
	return d.tenants
}

// Sessions returns the session repository (with Redis caching if available).
func (d *Database) Sessions() SessionRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sessions == nil {
		pgRepo := newSessionRepository(d.pg)
		if d.redis != nil {
			d.sessions = newCachedSessionRepository(pgRepo, d.redis)
		} else {
			d.sessions = pgRepo
		}
	}
	return d.sessions
}

// Peers returns the peer repository (with Redis caching if available).
func (d *Database) Peers() PeerRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.peers == nil {
		pgRepo := newPeerRepository(d.pg)
		if d.redis != nil {
			d.peers = newCachedPeerRepository(pgRepo, d.redis)
		} else {
			d.peers = pgRepo
		}
	}
	return d.peers
}

// Groups returns the group repository.
func (d *Database) Groups() GroupRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.groups == nil {
		d.groups = newGroupRepository(d.pg)
	}
	return d.groups
}

// ACL returns the ACL repository.
func (d *Database) ACL() ACLRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.acl == nil {
		d.acl = newACLRepository(d.pg)
	}
	return d.acl
}

// IPAM returns the IPAM repository.
func (d *Database) IPAM() IPAMRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ipam == nil {
		d.ipam = newIPAMRepository(d.pg)
	}
	return d.ipam
}

// WUSPDeviceStates returns the WUSP device state repository.
// Returns nil if RegisterWUSPDeviceStateRepository has not been called
// (i.e. the postgres subpackage has not been imported).
func (d *Database) WUSPDeviceStates() WUSPDeviceStateRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.wuspDeviceState == nil && newWUSPDeviceStateRepository != nil {
		d.wuspDeviceState = newWUSPDeviceStateRepository(d.pg)
	}
	return d.wuspDeviceState
}

// =============================================================================
// Placeholder constructors (implemented in postgres/ subpackage)
// =============================================================================

// These will be implemented in internal/store/postgres/
var (
	newAccountRepository             func(*pg.DB) AccountRepository
	newTenantRepository              func(*pg.DB) TenantRepository
	newSessionRepository             func(*pg.DB) SessionRepository
	newPeerRepository                func(*pg.DB) PeerRepository
	newGroupRepository               func(*pg.DB) GroupRepository
	newACLRepository                 func(*pg.DB) ACLRepository
	newIPAMRepository                func(*pg.DB) IPAMRepository
	newWUSPDeviceStateRepository func(*pg.DB) WUSPDeviceStateRepository
	newDeviceSnapshotRepository  func(*pg.DB) DeviceSnapshotRepository
	newCachedSessionRepository   func(SessionRepository, *redis.Client) SessionRepository
	newCachedPeerRepository      func(PeerRepository, *redis.Client) PeerRepository
)

// RegisterRepositories is called by the postgres package to register implementations.
func RegisterRepositories(
	accounts func(*pg.DB) AccountRepository,
	tenants func(*pg.DB) TenantRepository,
	sessions func(*pg.DB) SessionRepository,
	peers func(*pg.DB) PeerRepository,
	groups func(*pg.DB) GroupRepository,
	acl func(*pg.DB) ACLRepository,
	ipam func(*pg.DB) IPAMRepository,
) {
	newAccountRepository = accounts
	newTenantRepository = tenants
	newSessionRepository = sessions
	newPeerRepository = peers
	newGroupRepository = groups
	newACLRepository = acl
	newIPAMRepository = ipam
}

// WUSPDeviceStates returns the WUSP live-state repository.
// Alias kept for callers that used the old name (wusp_service, server).

// DeviceSnapshots returns the named device snapshot repository.
// Returns nil if RegisterDeviceSnapshotRepository has not been called.
func (d *Database) DeviceSnapshots() DeviceSnapshotRepository {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.deviceSnapshots == nil && newDeviceSnapshotRepository != nil {
		d.deviceSnapshots = newDeviceSnapshotRepository(d.pg)
	}
	return d.deviceSnapshots
}

// RegisterWUSPDeviceStateRepository registers the WUSP device state repository
// implementation. Called from the postgres package init().
func RegisterWUSPDeviceStateRepository(factory func(*pg.DB) WUSPDeviceStateRepository) {
	newWUSPDeviceStateRepository = factory
}

// RegisterDeviceSnapshotRepository registers the device snapshot repository
// implementation. Called from the postgres package init().
func RegisterDeviceSnapshotRepository(factory func(*pg.DB) DeviceSnapshotRepository) {
	newDeviceSnapshotRepository = factory
}

// RegisterCacheDecorators is called by the cache package to register implementations.
func RegisterCacheDecorators(
	sessions func(SessionRepository, *redis.Client) SessionRepository,
	peers func(PeerRepository, *redis.Client) PeerRepository,
) {
	newCachedSessionRepository = sessions
	newCachedPeerRepository = peers
}
