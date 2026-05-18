package postgres

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"WantasticCore/internal/config"

	"github.com/go-pg/pg/v10"
)

// Store implements the PostgreSQL storage.
type Store struct {
	db *pg.DB
}

// New creates a new PostgreSQL store.
func New(cfg config.DatabaseConfig) (*Store, error) {
	opt := OptionsFromConfig(cfg)

	db := pg.Connect(opt)

	// Check connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	return &Store{db: db}, nil
}

// NewWithDB creates a new PostgreSQL store using an existing connection.
func NewWithDB(db *pg.DB) *Store {
	return &Store{db: db}
}

func OptionsFromConfig(cfg config.DatabaseConfig) *pg.Options {
	opt := &pg.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		User:         cfg.User,
		Password:     cfg.Password,
		Database:     cfg.Database,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
	}

	if cfg.SSLMode != "disable" {
		opt.TLSConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	return opt
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying pg.DB instance.
func (s *Store) DB() *pg.DB {
	return s.db
}
