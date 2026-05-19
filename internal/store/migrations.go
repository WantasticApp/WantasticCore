package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/rs/zerolog/log"
)

// Migration represents a database migration.
type Migration struct {
	Version     int
	Description string
	Up          func(db *pg.DB) error
	Down        func(db *pg.DB) error
}

// SchemaMigration tracks applied migrations in the database.
type SchemaMigration struct {
	tableName   struct{}  `pg:"schema_migrations"`
	Version     int       `pg:"version,pk"`
	Description string    `pg:"description"`
	AppliedAt   time.Time `pg:"applied_at,default:now()"`
}

// migrations holds all registered migrations.
var migrations []Migration

// RegisterMigration adds a migration to the list.
func RegisterMigration(m Migration) {
	migrations = append(migrations, m)
}

// Migrate runs all pending migrations.
func (d *Database) Migrate() error {
	ctx := context.Background()

	// Create schema_migrations table if not exists
	_, err := d.pg.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	var current int
	_, err = d.pg.QueryOneContext(ctx, pg.Scan(&current),
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Apply pending migrations
	applied := 0
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}

		log.Debug().
			Int("version", m.Version).
			Str("description", m.Description).
			Msg(" Applying migration...")

		// Run migration in transaction
		err := d.pg.RunInTransaction(ctx, func(tx *pg.Tx) error {
			if err := m.Up(d.pg); err != nil {
				return fmt.Errorf("migration %d failed: %w", m.Version, err)
			}

			// Record migration
			_, err := tx.ExecContext(ctx, `
				INSERT INTO schema_migrations (version, description)
				VALUES (?, ?)
			`, m.Version, m.Description)
			return err
		})
		if err != nil {
			return err
		}

		applied++
		log.Debug().
			Int("version", m.Version).
			Msg(" Migration applied")
	}

	if applied > 0 {
		log.Debug().Int("count", applied).Msg("🎉 All migrations applied")
	} else {
		log.Debug().Msg(" Database schema is up to date")
	}

	return nil
}

// CurrentVersion returns the current schema version.
func (d *Database) CurrentVersion() (int, error) {
	var version int
	_, err := d.pg.QueryOne(pg.Scan(&version),
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	return version, err
}

// RollbackMigration rolls back the last migration.
func (d *Database) RollbackMigration() error {
	ctx := context.Background()

	current, err := d.CurrentVersion()
	if err != nil {
		return err
	}
	if current == 0 {
		return fmt.Errorf("no migrations to rollback")
	}

	// Find the migration to rollback
	var target *Migration
	for i := range migrations {
		if migrations[i].Version == current {
			target = &migrations[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("migration %d not found in registry", current)
	}
	if target.Down == nil {
		return fmt.Errorf("migration %d has no Down function", current)
	}

	log.Debug().
		Int("version", target.Version).
		Str("description", target.Description).
		Msg(" Rolling back migration...")

	err = d.pg.RunInTransaction(ctx, func(tx *pg.Tx) error {
		if err := target.Down(d.pg); err != nil {
			return fmt.Errorf("rollback %d failed: %w", target.Version, err)
		}

		_, err := tx.ExecContext(ctx, `
			DELETE FROM schema_migrations WHERE version = ?
		`, target.Version)
		return err
	})
	if err != nil {
		return err
	}

	log.Debug().Int("version", target.Version).Msg(" Migration rolled back")
	return nil
}


// =============================================================================
// Initial Schema
// =============================================================================
//
// Single, consolidated migration that defines every table the application
// uses. Future schema changes register additional migrations with higher
// version numbers; this one is the canonical starting point for fresh
// installs and is never re-ordered.

func init() {
	RegisterMigration(Migration{
		Version:     1,
		Description: "Initial schema",
		Up: func(db *pg.DB) error {
			_, err := db.Exec(`
				-- Accounts — WireGuard network allocations, one per tenant.
				CREATE TABLE IF NOT EXISTS accounts (
					id                  TEXT PRIMARY KEY,
					name                TEXT NOT NULL,
					networks            TEXT[] DEFAULT '{}',
					server_ips          TEXT[] DEFAULT '{}',
					block_count         INT DEFAULT 1,
					private_key         TEXT NOT NULL,
					max_peers           INT NOT NULL DEFAULT 30,
					peer_limit_override INT NOT NULL DEFAULT 0,
					created_at          TIMESTAMPTZ DEFAULT NOW(),
					updated_at          TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_accounts_name ON accounts(name);

				-- Tenants — portal users. Super-admins have is_admin=true.
				CREATE TABLE IF NOT EXISTS tenants (
					id                          TEXT PRIMARY KEY,
					email                       TEXT UNIQUE NOT NULL,
					full_name                   TEXT NOT NULL,
					password_hash               TEXT NOT NULL,
					totp_secret                 TEXT,
					totp_enabled                BOOLEAN DEFAULT FALSE,
					last_login                  TIMESTAMPTZ,
					twofa_method                TEXT DEFAULT '',
					twofa_whatsapp_enabled      BOOLEAN DEFAULT FALSE,
					twofa_pending_code          TEXT,
					twofa_code_expiry           TIMESTAMPTZ,
					twofa_code_attempts         INT DEFAULT 0,
					overlay_account_id          TEXT REFERENCES accounts(id) ON DELETE SET NULL,
					networks                    TEXT[] DEFAULT '{}',
					status                      TEXT DEFAULT 'active',
					is_admin                    BOOLEAN NOT NULL DEFAULT FALSE,
					preferred_language          TEXT DEFAULT 'en',
					inactivity_warning_sent_at  TIMESTAMPTZ,
					auth0_sub                   TEXT NOT NULL DEFAULT '',
					created_at                  TIMESTAMPTZ DEFAULT NOW(),
					updated_at                  TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_tenants_email           ON tenants(email);
				CREATE INDEX IF NOT EXISTS idx_tenants_overlay_account ON tenants(overlay_account_id);

				-- Tenant sessions — active portal logins.
				CREATE TABLE IF NOT EXISTS tenant_sessions (
					session_id      TEXT PRIMARY KEY,
					tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
					email           TEXT,
					full_name       TEXT,
					session_token   TEXT,
					created_at      TIMESTAMPTZ DEFAULT NOW(),
					expires_at      TIMESTAMPTZ NOT NULL,
					last_activity   TIMESTAMPTZ DEFAULT NOW(),
					ip_address      TEXT,
					user_agent      TEXT,
					remember_me     BOOLEAN DEFAULT FALSE,
					device_hash     TEXT,
					trusted_device  BOOLEAN DEFAULT FALSE
				);
				CREATE INDEX IF NOT EXISTS idx_sessions_tenant  ON tenant_sessions(tenant_id);
				CREATE INDEX IF NOT EXISTS idx_sessions_expires ON tenant_sessions(expires_at);
				CREATE INDEX IF NOT EXISTS idx_sessions_device  ON tenant_sessions(tenant_id, device_hash);

				-- Peers — WireGuard devices.
				CREATE TABLE IF NOT EXISTS peers (
					id                              TEXT PRIMARY KEY,
					account_id                      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
					name                            TEXT NOT NULL,
					assigned_ip                     TEXT NOT NULL,
					allowed_ips                     TEXT[] DEFAULT '{}',
					private_key                     TEXT NOT NULL,
					created_at                      TIMESTAMPTZ DEFAULT NOW(),
					updated_at                      TIMESTAMPTZ DEFAULT NOW(),
					is_online                       BOOLEAN DEFAULT FALSE,
					last_handshake_time             TIMESTAMPTZ,
					last_seen_at                    TIMESTAMPTZ,
					endpoint                        TEXT,
					uptime_history                  BYTEA,
					rx_bytes                        BIGINT DEFAULT 0,
					tx_bytes                        BIGINT DEFAULT 0,
					webssh_consumer_active          BOOLEAN DEFAULT FALSE,
					webssh_consumer_port            INT,
					webssh_link_active              BOOLEAN DEFAULT FALSE,
					webssh_link_expiry              TIMESTAMPTZ,
					has_winbox                      BOOLEAN DEFAULT FALSE,
					encrypted_routeros_username     BYTEA,
					encrypted_routeros_password     BYTEA,
					routeros_credential_source      TEXT NOT NULL DEFAULT '',
					routeros_api_verified           BOOLEAN NOT NULL DEFAULT FALSE,
					routeros_api_last_validated     TIMESTAMPTZ,
					routeros_api_error              TEXT NOT NULL DEFAULT '',
					routeros_api_port               INT NOT NULL DEFAULT 0,
					routeros_api_tls                BOOLEAN NOT NULL DEFAULT FALSE,
					last_port_scan                  TIMESTAMPTZ,
					cached_port_scan_json           JSONB,
					scanned_ssh_port                INT,
					scanned_winbox_port             INT,
					last_port_scan_time             TIMESTAMPTZ,
					notification_enabled            BOOLEAN DEFAULT FALSE,
					first_seen_online               TIMESTAMPTZ,
					last_online_at                  TIMESTAMPTZ,
					failed_handshakes               INT DEFAULT 0,
					last_notification_sent_at       TIMESTAMPTZ,
					offline_notification_state      TEXT,
					scan_progress                   INT DEFAULT 0,
					tags                            TEXT[] DEFAULT '{}',
					notes                           TEXT NOT NULL DEFAULT '',
					client_type                     TEXT NOT NULL DEFAULT '',
					is_wantasticd                   BOOLEAN NOT NULL DEFAULT FALSE,
					agent_model                     TEXT NOT NULL DEFAULT '',
					agent_version                   TEXT NOT NULL DEFAULT ''
				);
				CREATE INDEX IF NOT EXISTS idx_peers_account        ON peers(account_id);
				CREATE INDEX IF NOT EXISTS idx_peers_ip             ON peers(assigned_ip);
				CREATE INDEX IF NOT EXISTS idx_peers_is_wantasticd  ON peers(is_wantasticd) WHERE is_wantasticd = TRUE;
				CREATE INDEX IF NOT EXISTS idx_peers_agent_model    ON peers(agent_model)   WHERE agent_model != '';

				-- Winbox sessions — saved MikroTik proxy targets.
				CREATE TABLE IF NOT EXISTS winbox_sessions (
					id                          TEXT PRIMARY KEY,
					peer_id                     TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
					account_id                  TEXT NOT NULL,
					name                        TEXT NOT NULL,
					router_ip                   TEXT,
					port                        INT NOT NULL DEFAULT 8291,
					access_token                TEXT UNIQUE,
					password_token              TEXT,
					encrypted_username          BYTEA,
					encrypted_password          BYTEA,
					auth_method                 TEXT,
					allowed_client_ips          TEXT[] DEFAULT '{}',
					credentials_valid           BOOLEAN DEFAULT FALSE,
					last_validated              TIMESTAMPTZ,
					validation_error            TEXT,
					routeros_api_verified       BOOLEAN NOT NULL DEFAULT FALSE,
					routeros_api_last_validated TIMESTAMPTZ,
					routeros_api_error          TEXT NOT NULL DEFAULT '',
					routeros_api_port           INT NOT NULL DEFAULT 0,
					routeros_api_tls            BOOLEAN NOT NULL DEFAULT FALSE,
					last_connected              TIMESTAMPTZ,
					created_at                  TIMESTAMPTZ DEFAULT NOW(),
					updated_at                  TIMESTAMPTZ DEFAULT NOW(),
					enabled                     BOOLEAN DEFAULT TRUE
				);
				CREATE INDEX IF NOT EXISTS idx_winbox_peer  ON winbox_sessions(peer_id);
				CREATE INDEX IF NOT EXISTS idx_winbox_token ON winbox_sessions(access_token);

				-- WebSSH sessions — saved browser-SSH targets.
				CREATE TABLE IF NOT EXISTS webssh_sessions (
					id                                TEXT PRIMARY KEY,
					peer_id                           TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
					peer_ip                           TEXT,
					account_id                        TEXT NOT NULL,
					name                              TEXT NOT NULL,
					port                              INT DEFAULT 22,
					encrypted_username                BYTEA,
					encrypted_password                BYTEA,
					encrypted_private_key             BYTEA,
					encrypted_private_key_passphrase  BYTEA,
					terminal_rows                     INT DEFAULT 24,
					terminal_cols                     INT DEFAULT 80,
					user_agent                        TEXT NOT NULL DEFAULT '',
					last_connected                    TIMESTAMPTZ,
					created_at                        TIMESTAMPTZ DEFAULT NOW(),
					updated_at                        TIMESTAMPTZ DEFAULT NOW(),
					enabled                           BOOLEAN DEFAULT TRUE,
					history                           BYTEA,
					host_key                          BYTEA,
					host_key_fingerprint              TEXT NOT NULL DEFAULT '',
					host_key_algorithm                TEXT NOT NULL DEFAULT '',
					compatibility_mode                TEXT NOT NULL DEFAULT ''
				);
				CREATE INDEX IF NOT EXISTS idx_webssh_peer                ON webssh_sessions(peer_id);
				CREATE INDEX IF NOT EXISTS idx_webssh_account_created     ON webssh_sessions(account_id, created_at DESC);
				CREATE INDEX IF NOT EXISTS idx_webssh_account_peer_created ON webssh_sessions(account_id, peer_id, created_at DESC);

				-- SSH activity log — one row per shell session.
				CREATE TABLE IF NOT EXISTS ssh_activities (
					id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
					peer_id     TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
					account_id  TEXT NOT NULL,
					session_id  TEXT NOT NULL,
					user_agent  TEXT,
					client_ip   TEXT,
					timestamp   TIMESTAMPTZ DEFAULT NOW(),
					end_time    TIMESTAMPTZ,
					username    TEXT,
					commands    JSONB DEFAULT '[]',
					bytes_sent  BIGINT DEFAULT 0,
					bytes_recv  BIGINT DEFAULT 0,
					duration_ms BIGINT DEFAULT 0
				);
				CREATE INDEX IF NOT EXISTS idx_ssh_activity_peer ON ssh_activities(peer_id);
				CREATE INDEX IF NOT EXISTS idx_ssh_activity_time ON ssh_activities(timestamp DESC);
				CREATE UNIQUE INDEX IF NOT EXISTS idx_ssh_activity_account_session_unique
					ON ssh_activities(account_id, session_id);

				-- Winbox activity log.
				CREATE TABLE IF NOT EXISTS winbox_activities (
					id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
					peer_id      TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
					account_id   TEXT NOT NULL,
					session_name TEXT NOT NULL,
					username     TEXT,
					client_ip    TEXT,
					timestamp    TIMESTAMPTZ DEFAULT NOW(),
					end_time     TIMESTAMPTZ,
					duration_ms  BIGINT DEFAULT 0,
					romon_mode   BOOLEAN DEFAULT FALSE
				);
				CREATE INDEX IF NOT EXISTS idx_winbox_activity_peer ON winbox_activities(peer_id);
				CREATE INDEX IF NOT EXISTS idx_winbox_activity_time ON winbox_activities(timestamp DESC);

				-- Peer groups + memberships + group→group links — ACL building blocks.
				CREATE TABLE IF NOT EXISTS peer_groups (
					id          TEXT PRIMARY KEY,
					account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
					name        TEXT NOT NULL,
					description TEXT,
					protocols   SMALLINT[] DEFAULT '{}',
					created_at  TIMESTAMPTZ DEFAULT NOW(),
					updated_at  TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_groups_account ON peer_groups(account_id);

				CREATE TABLE IF NOT EXISTS peer_group_members (
					group_id TEXT NOT NULL REFERENCES peer_groups(id) ON DELETE CASCADE,
					peer_id  TEXT NOT NULL REFERENCES peers(id)       ON DELETE CASCADE,
					PRIMARY KEY (group_id, peer_id)
				);
				CREATE INDEX IF NOT EXISTS idx_members_peer ON peer_group_members(peer_id);

				CREATE TABLE IF NOT EXISTS group_links (
					id            TEXT PRIMARY KEY,
					account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
					src_group_id  TEXT NOT NULL REFERENCES peer_groups(id) ON DELETE CASCADE,
					dst_group_id  TEXT NOT NULL REFERENCES peer_groups(id) ON DELETE CASCADE,
					action        TEXT DEFAULT 'allow',
					protocols     SMALLINT[] DEFAULT '{}',
					port_ranges   JSONB DEFAULT '[]',
					created_at    TIMESTAMPTZ DEFAULT NOW(),
					updated_at    TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_links_account ON group_links(account_id);

				-- ACL rules (flat firewall rules; coexist with group_links).
				CREATE TABLE IF NOT EXISTS acl_rules (
					id              TEXT PRIMARY KEY,
					account_id      TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
					name            TEXT NOT NULL,
					action          TEXT DEFAULT 'allow',
					protocol        TEXT DEFAULT 'all',
					source_ips      TEXT[] DEFAULT '{}',
					dest_ips        TEXT[] DEFAULT '{}',
					dest_ports      INT[] DEFAULT '{}',
					priority        INT DEFAULT 0,
					description     TEXT,
					source_peer_ids TEXT[] DEFAULT '{}',
					dest_peer_ids   TEXT[] DEFAULT '{}',
					services        TEXT[] DEFAULT '{}',
					created_at      TIMESTAMPTZ DEFAULT NOW(),
					updated_at      TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_acl_account  ON acl_rules(account_id);
				CREATE INDEX IF NOT EXISTS idx_acl_priority ON acl_rules(account_id, priority);

				-- Peer migrations — owner-to-owner device transfer state.
				CREATE TABLE IF NOT EXISTS peer_migrations (
					id                     TEXT PRIMARY KEY,
					source_tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
					source_tenant_email    TEXT NOT NULL,
					source_tenant_name     TEXT,
					target_email           TEXT NOT NULL,
					target_tenant_id       TEXT REFERENCES tenants(id) ON DELETE SET NULL,
					target_tenant_name     TEXT,
					peers                  JSONB DEFAULT '[]',
					invite_token           TEXT UNIQUE,
					status                 TEXT DEFAULT 'pending',
					failure_reason         TEXT,
					encrypted_ssh_username BYTEA,
					encrypted_ssh_password BYTEA,
					created_at             TIMESTAMPTZ DEFAULT NOW(),
					expires_at             TIMESTAMPTZ,
					accepted_at            TIMESTAMPTZ,
					completed_at           TIMESTAMPTZ,
					logs                   TEXT[] DEFAULT '{}'
				);
				CREATE INDEX IF NOT EXISTS idx_migrations_source ON peer_migrations(source_tenant_id);
				CREATE INDEX IF NOT EXISTS idx_migrations_target ON peer_migrations(target_email);
				CREATE INDEX IF NOT EXISTS idx_migrations_token  ON peer_migrations(invite_token);

				-- Peer handshakes — append-only history.
				CREATE TABLE IF NOT EXISTS peer_handshakes (
					id         BIGSERIAL PRIMARY KEY,
					peer_id    TEXT NOT NULL,
					account_id TEXT NOT NULL,
					timestamp  TIMESTAMPTZ DEFAULT NOW(),
					endpoint   TEXT
				);
				CREATE INDEX IF NOT EXISTS idx_handshakes_peer_time    ON peer_handshakes(peer_id, timestamp);
				CREATE INDEX IF NOT EXISTS idx_handshakes_account_time ON peer_handshakes(account_id, timestamp);

				-- Enrollment tokens — pre-shared device-enrollment secrets.
				CREATE TABLE IF NOT EXISTS enrollment_tokens (
					id          TEXT PRIMARY KEY,
					tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
					name        TEXT NOT NULL,
					token       TEXT UNIQUE NOT NULL,
					max_uses    INT DEFAULT 0,
					usage_count INT DEFAULT 0,
					expires_at  TIMESTAMPTZ,
					created_at  TIMESTAMPTZ DEFAULT NOW(),
					created_by  TEXT
				);
				CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_tenant     ON enrollment_tokens(tenant_id);
				CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_token      ON enrollment_tokens(token);
				CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_expiry     ON enrollment_tokens(expires_at);
				CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_created_at ON enrollment_tokens(created_at);

				-- IPAM — persistent global /27 pool tracking.
				CREATE TABLE IF NOT EXISTS ipam_blocks (
					cidr       TEXT PRIMARY KEY,
					tenant_id  TEXT DEFAULT '',
					allocated  BOOLEAN DEFAULT FALSE,
					pool_index INT NOT NULL,
					updated_at TIMESTAMPTZ DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_ipam_blocks_tenant    ON ipam_blocks(tenant_id);
				CREATE INDEX IF NOT EXISTS idx_ipam_blocks_allocated ON ipam_blocks(allocated);
				CREATE INDEX IF NOT EXISTS idx_ipam_blocks_pool      ON ipam_blocks(pool_index);

				-- WUSP device state — last-known parameter snapshot per peer.
				CREATE TABLE IF NOT EXISTS wusp_device_states (
					id               TEXT PRIMARY KEY,
					peer_id          TEXT NOT NULL REFERENCES peers(id) ON DELETE CASCADE,
					account_id       TEXT NOT NULL,
					last_sync_at     TIMESTAMPTZ,
					sync_error       TEXT NOT NULL DEFAULT '',
					device_snapshot  JSONB NOT NULL DEFAULT '[]',
					device_id        TEXT NOT NULL DEFAULT '',
					manufacturer     TEXT NOT NULL DEFAULT '',
					product_class    TEXT NOT NULL DEFAULT '',
					serial_number    TEXT NOT NULL DEFAULT '',
					software_version TEXT NOT NULL DEFAULT '',
					hardware_version TEXT NOT NULL DEFAULT '',
					wusp_enable      BOOLEAN NOT NULL DEFAULT FALSE,
					wusp_status      TEXT NOT NULL DEFAULT '',
					wusp_version     TEXT NOT NULL DEFAULT '',
					created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
				);
				CREATE UNIQUE INDEX IF NOT EXISTS idx_wusp_device_states_peer    ON wusp_device_states(peer_id);
				CREATE INDEX        IF NOT EXISTS idx_wusp_device_states_account ON wusp_device_states(account_id);

				-- Named device snapshots — tenant-scoped configuration backups.
				CREATE TABLE IF NOT EXISTS device_snapshots (
					id               TEXT PRIMARY KEY,
					account_id       TEXT NOT NULL,
					name             TEXT NOT NULL DEFAULT '',
					protocol         TEXT NOT NULL DEFAULT 'wusp',
					manufacturer     TEXT NOT NULL DEFAULT '',
					product_class    TEXT NOT NULL DEFAULT '',
					serial_number    TEXT NOT NULL DEFAULT '',
					software_version TEXT NOT NULL DEFAULT '',
					hardware_version TEXT NOT NULL DEFAULT '',
					device_snapshot  JSONB NOT NULL DEFAULT '[]',
					backup_file      TEXT NOT NULL DEFAULT '',
					backup_name      TEXT NOT NULL DEFAULT '',
					backup_size      INTEGER NOT NULL DEFAULT 0,
					upload_token     TEXT NOT NULL DEFAULT '',
					created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_device_snapshots_account      ON device_snapshots(account_id);
				CREATE INDEX IF NOT EXISTS idx_device_snapshots_protocol     ON device_snapshots(account_id, protocol);
				CREATE INDEX IF NOT EXISTS idx_device_snapshots_upload_token ON device_snapshots(upload_token) WHERE upload_token != '';

				-- Export records — device transfer state machine.
				CREATE TABLE IF NOT EXISTS export_records (
					id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
					token               TEXT UNIQUE NOT NULL,
					token_jti           TEXT UNIQUE NOT NULL,
					source_account_id   TEXT NOT NULL,
					source_tenant_id    TEXT NOT NULL,
					target_account_id   TEXT NOT NULL,
					target_tenant_id    TEXT,
					target_email        TEXT NOT NULL,
					peer_id             TEXT NOT NULL,
					device_name         TEXT,
					client_type         VARCHAR(20) DEFAULT 'wantasticd',
					state               VARCHAR(30) NOT NULL DEFAULT 'pending',
					new_peer_id         TEXT,
					new_assigned_ip     TEXT,
					expires_at          TIMESTAMPTZ NOT NULL,
					created_at          TIMESTAMPTZ DEFAULT NOW(),
					claimed_at          TIMESTAMPTZ,
					device_notified_at  TIMESTAMPTZ,
					device_confirmed_at TIMESTAMPTZ,
					completed_at        TIMESTAMPTZ,
					failed_at           TIMESTAMPTZ,
					failure_reason      TEXT,
					retry_count         INT DEFAULT 0,

					CONSTRAINT valid_state CHECK (state IN (
						'pending', 'claimed', 'device_notified', 'device_confirmed',
						'device_failed', 'source_peer_removed', 'target_peer_created',
						'completed', 'failed', 'expired'
					))
				);
				CREATE INDEX IF NOT EXISTS idx_export_records_token          ON export_records(token);
				CREATE INDEX IF NOT EXISTS idx_export_records_state          ON export_records(state);
				CREATE INDEX IF NOT EXISTS idx_export_records_source_account ON export_records(source_account_id);
				CREATE INDEX IF NOT EXISTS idx_export_records_target_account ON export_records(target_account_id);
				CREATE INDEX IF NOT EXISTS idx_export_records_expires_at     ON export_records(expires_at);
				CREATE INDEX IF NOT EXISTS idx_export_records_created_at     ON export_records(created_at);
			`)
			return err
		},
		Down: func(db *pg.DB) error {
			_, err := db.Exec(`
				DROP TABLE IF EXISTS export_records      CASCADE;
				DROP TABLE IF EXISTS device_snapshots    CASCADE;
				DROP TABLE IF EXISTS wusp_device_states  CASCADE;
				DROP TABLE IF EXISTS ipam_blocks         CASCADE;
				DROP TABLE IF EXISTS enrollment_tokens   CASCADE;
				DROP TABLE IF EXISTS peer_handshakes     CASCADE;
				DROP TABLE IF EXISTS peer_migrations     CASCADE;
				DROP TABLE IF EXISTS acl_rules           CASCADE;
				DROP TABLE IF EXISTS group_links         CASCADE;
				DROP TABLE IF EXISTS peer_group_members  CASCADE;
				DROP TABLE IF EXISTS peer_groups         CASCADE;
				DROP TABLE IF EXISTS winbox_activities   CASCADE;
				DROP TABLE IF EXISTS ssh_activities      CASCADE;
				DROP TABLE IF EXISTS webssh_sessions     CASCADE;
				DROP TABLE IF EXISTS winbox_sessions     CASCADE;
				DROP TABLE IF EXISTS peers               CASCADE;
				DROP TABLE IF EXISTS tenant_sessions     CASCADE;
				DROP TABLE IF EXISTS tenants             CASCADE;
				DROP TABLE IF EXISTS accounts            CASCADE;
			`)
			return err
		},
	})
}
