package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

type ipamRepository struct {
	db *pg.DB
}

func newIPAMRepository(db *pg.DB) store.IPAMRepository {
	return &ipamRepository{db: db}
}

func (r *ipamRepository) UpsertBlock(b *store.IPAMBlockData) error {
	model := &IPAMBlock{
		CIDR:      b.CIDR,
		TenantID:  b.TenantID,
		Allocated: b.Allocated,
		PoolIndex: b.PoolIndex,
		UpdatedAt: time.Now(),
	}

	// Explicitly set all columns to avoid DEFAULT values in INSERT
	// We use direct SQL to ensure pool_index=0 is respected and not treated as default/null
	_, err := r.db.Exec(`
		INSERT INTO ipam_blocks (cidr, tenant_id, allocated, pool_index, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (cidr) DO UPDATE
		SET tenant_id = EXCLUDED.tenant_id,
			allocated = EXCLUDED.allocated,
			pool_index = EXCLUDED.pool_index,
			updated_at = EXCLUDED.updated_at
	`, model.CIDR, model.TenantID, model.Allocated, model.PoolIndex, model.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert IPAM block: %w", err)
	}
	return nil
}

func (r *ipamRepository) ListBlocks() ([]*store.IPAMBlockData, error) {
	var models []IPAMBlock
	err := r.db.Model(&models).Order("cidr ASC").Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list IPAM blocks: %w", err)
	}

	result := make([]*store.IPAMBlockData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

func (r *ipamRepository) GetBlock(cidr string) (*store.IPAMBlockData, error) {
	model := &IPAMBlock{CIDR: cidr}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("IPAM block not found: %s", cidr)
		}
		return nil, fmt.Errorf("failed to get IPAM block: %w", err)
	}
	return r.toData(model), nil
}

func (r *ipamRepository) AllocateBlocks(tenantID string, poolIndex int, count int) ([]string, error) {
	var blocks []string

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Optimize: Use a single query with CTE to atomically select and update free blocks
		// ensuring no race conditions and a single DB round-trip.
		// Requires PostgreSQL 12+ (which supports CTEs in UPDATE) - or using subquery approach
		// Since go-pg limits complex CTEs, we use raw query for maximum performance and atomicity.

		query := `
			WITH free_blocks AS (
				SELECT cidr
				FROM ipam_blocks
				WHERE allocated = false AND pool_index = ?
				LIMIT ?
				FOR UPDATE SKIP LOCKED
			)
			UPDATE ipam_blocks
			SET allocated = true, tenant_id = ?, updated_at = ?
			FROM free_blocks
			WHERE ipam_blocks.cidr = free_blocks.cidr
			RETURNING ipam_blocks.cidr;
		`

		_, err := tx.Query(&blocks, query, poolIndex, count, tenantID, time.Now())
		if err != nil {
			return fmt.Errorf("failed to allocate blocks query: %w", err)
		}

		if len(blocks) < count {
			// Rollback is automatic on error return from transaction function
			return fmt.Errorf("not enough free blocks in pool %d (found %d, need %d)", poolIndex, len(blocks), count)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to allocate blocks: %w", err)
	}
	return blocks, nil
}

func (r *ipamRepository) ReleaseBlocks(tenantID string) error {
	_, err := r.db.Model((*IPAMBlock)(nil)).
		Set("allocated = ?", false).
		Set("tenant_id = ?", "").
		Set("updated_at = ?", time.Now()).
		Where("tenant_id = ?", tenantID).
		Update()

	if err != nil {
		return fmt.Errorf("failed to release blocks for tenant %s: %w", tenantID, err)
	}
	return nil
}

func (r *ipamRepository) RestoreState() ([]*store.IPAMBlockData, error) {
	return r.ListBlocks()
}

func (r *ipamRepository) toData(m *IPAMBlock) *store.IPAMBlockData {
	return &store.IPAMBlockData{
		CIDR:      m.CIDR,
		TenantID:  m.TenantID,
		Allocated: m.Allocated,
		PoolIndex: m.PoolIndex,
		UpdatedAt: m.UpdatedAt,
	}
}
