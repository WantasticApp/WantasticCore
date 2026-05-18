// Package postgres registers PostgreSQL repository implementations.
package postgres

import (
	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

func init() {
	// Register PostgreSQL repository constructors with the store package
	store.RegisterRepositories(
		func(db *pg.DB) store.AccountRepository { return NewAccountRepository(db) },
		func(db *pg.DB) store.TenantRepository { return NewTenantRepository(db) },
		func(db *pg.DB) store.SessionRepository { return NewSessionRepository(db) },
		func(db *pg.DB) store.PeerRepository { return NewPeerRepository(db) },
		func(db *pg.DB) store.GroupRepository { return NewGroupRepository(db) },
		func(db *pg.DB) store.ACLRepository { return NewACLRepository(db) },
		func(db *pg.DB) store.IPAMRepository { return newIPAMRepository(db) },
	)
	store.RegisterWUSPDeviceStateRepository(
		func(db *pg.DB) store.WUSPDeviceStateRepository { return NewWUSPDeviceStateRepository(db) },
	)
	store.RegisterDeviceSnapshotRepository(
		func(db *pg.DB) store.DeviceSnapshotRepository { return NewDeviceSnapshotRepository(db) },
	)
}
