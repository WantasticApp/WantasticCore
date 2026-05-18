package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

type groupRepository struct {
	db *pg.DB
}

func NewGroupRepository(db *pg.DB) store.GroupRepository {
	return &groupRepository{db: db}
}

func (r *groupRepository) SaveGroup(group *store.GroupData) error {
	model := &PeerGroup{
		ID:          group.ID,
		AccountID:   group.AccountID,
		Name:        group.Name,
		Description: group.Description,
		Protocols:   group.Protocols,
		CreatedAt:   group.CreatedAt,
		UpdatedAt:   group.UpdatedAt,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now()
	}
	_, err := r.db.Model(model).OnConflict("(id) DO UPDATE").Insert()
	return err
}

func (r *groupRepository) GetGroup(groupID string) (*store.GroupData, error) {
	model := &PeerGroup{ID: groupID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("group not found: %s", groupID)
		}
		return nil, err
	}
	return r.toGroupData(model), nil
}

func (r *groupRepository) ListByAccount(accountID string) ([]*store.GroupData, error) {
	var models []PeerGroup
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Order("name ASC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.GroupData, len(models))
	for i := range models {
		result[i] = r.toGroupData(&models[i])
	}
	return result, nil
}

// ListAll retrieves all groups across all accounts.
func (r *groupRepository) ListAll() ([]*store.GroupData, error) {
	var models []PeerGroup
	if err := r.db.Model(&models).Select(); err != nil {
		return nil, err
	}
	result := make([]*store.GroupData, len(models))
	for i := range models {
		result[i] = r.toGroupData(&models[i])
	}
	return result, nil
}

func (r *groupRepository) DeleteGroup(groupID string) error {
	result, err := r.db.Model((*PeerGroup)(nil)).
		Where("id = ?", groupID).
		Delete()
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("group not found")
	}
	return nil
}

// Membership

func (r *groupRepository) AddPeerToGroup(groupID, peerID string) error {
	_, err := r.db.Exec(`
		INSERT INTO peer_group_members (group_id, peer_id)
		VALUES (?, ?)
		ON CONFLICT DO NOTHING
	`, groupID, peerID)
	return err
}

func (r *groupRepository) RemovePeerFromGroup(groupID, peerID string) error {
	_, err := r.db.Exec(`
		DELETE FROM peer_group_members
		WHERE group_id = ? AND peer_id = ?
	`, groupID, peerID)
	return err
}

func (r *groupRepository) GetPeerGroups(peerID string) ([]string, error) {
	var groupIDs []string
	_, err := r.db.Query(&groupIDs, `
		SELECT group_id FROM peer_group_members WHERE peer_id = ?
	`, peerID)
	return groupIDs, err
}

func (r *groupRepository) GetGroupPeers(groupID string) ([]string, error) {
	var peerIDs []string
	_, err := r.db.Query(&peerIDs, `
		SELECT peer_id FROM peer_group_members WHERE group_id = ?
	`, groupID)
	return peerIDs, err
}

// Links

func (r *groupRepository) SaveLink(link *store.GroupLinkData) error {
	// DTO PortRanges to Postgres JSON/compatible struct
	// Existing model has PortRanges any
	// We can pass slice of PortRange structs, pg converts to jsonb

	model := &GroupLink{
		ID:         link.ID,
		AccountID:  link.AccountID,
		SrcGroupID: link.SrcGroupID,
		DstGroupID: link.DstGroupID,
		Action:     link.Action,
		Protocols:  link.Protocols,
		PortRanges: link.PortRanges, // Assuming this marshals correctly
		CreatedAt:  link.CreatedAt,
		UpdatedAt:  link.UpdatedAt,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now()
	}
	_, err := r.db.Model(model).OnConflict("(id) DO UPDATE").Insert()
	return err
}

func (r *groupRepository) GetLink(linkID string) (*store.GroupLinkData, error) {
	model := &GroupLink{ID: linkID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("link not found")
		}
		return nil, err
	}
	return r.toLinkData(model), nil
}

func (r *groupRepository) ListLinksByAccount(accountID string) ([]*store.GroupLinkData, error) {
	var models []GroupLink
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.GroupLinkData, len(models))
	for i := range models {
		result[i] = r.toLinkData(&models[i])
	}
	return result, nil
}

// ListAllLinks retrieves all group links across all accounts.
func (r *groupRepository) ListAllLinks() ([]*store.GroupLinkData, error) {
	var models []GroupLink
	if err := r.db.Model(&models).Select(); err != nil {
		return nil, err
	}
	result := make([]*store.GroupLinkData, len(models))
	for i := range models {
		result[i] = r.toLinkData(&models[i])
	}
	return result, nil
}

func (r *groupRepository) DeleteLink(linkID string) error {
	result, err := r.db.Model((*GroupLink)(nil)).
		Where("id = ?", linkID).
		Delete()
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("link not found")
	}
	return nil
}

// Helpers

func (r *groupRepository) toGroupData(m *PeerGroup) *store.GroupData {
	return &store.GroupData{
		ID:          m.ID,
		AccountID:   m.AccountID,
		Name:        m.Name,
		Description: m.Description,
		Protocols:   m.Protocols,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func (r *groupRepository) toLinkData(m *GroupLink) *store.GroupLinkData {
	var ranges []store.PortRange
	if m.PortRanges != nil {
		data, _ := json.Marshal(m.PortRanges)
		json.Unmarshal(data, &ranges)
	}

	return &store.GroupLinkData{
		ID:         m.ID,
		AccountID:  m.AccountID,
		SrcGroupID: m.SrcGroupID,
		DstGroupID: m.DstGroupID,
		Action:     m.Action,
		Protocols:  m.Protocols,
		PortRanges: ranges,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}
