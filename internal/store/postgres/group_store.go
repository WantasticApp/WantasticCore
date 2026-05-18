package postgres

import (
	"WantasticCore/internal/models"
	"encoding/json"

	"github.com/go-pg/pg/v10"
)

// GroupStore implements the storage for peer groups and links.
type GroupStore struct {
	db *pg.DB
}

// NewGroupStore creates a new PostgreSQL-backed group store.
func NewGroupStore(db *pg.DB) *GroupStore {
	return &GroupStore{db: db}
}

// Helpers for converting between postgres models and domain models

func toModelsPeerGroup(pg *PeerGroup) *models.PeerGroup {
	if pg == nil {
		return nil
	}
	return &models.PeerGroup{
		ID:          pg.ID,
		AccountID:   pg.AccountID,
		Name:        pg.Name,
		Description: pg.Description,
		Protocols:   pg.Protocols,
		CreatedAt:   pg.CreatedAt,
		UpdatedAt:   pg.UpdatedAt,
	}
}

func fromModelsPeerGroup(m *models.PeerGroup) *PeerGroup {
	if m == nil {
		return nil
	}
	return &PeerGroup{
		ID:          m.ID,
		AccountID:   m.AccountID,
		Name:        m.Name,
		Description: m.Description,
		Protocols:   m.Protocols,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func toModelsGroupLink(pg *GroupLink) *models.GroupLink {
	if pg == nil {
		return nil
	}

	var portRanges []models.PortRange
	if pg.PortRanges != nil {
		data, err := json.Marshal(pg.PortRanges)
		if err == nil {
			_ = json.Unmarshal(data, &portRanges)
		}
	}

	return &models.GroupLink{
		ID:         pg.ID,
		AccountID:  pg.AccountID,
		SrcGroupID: pg.SrcGroupID,
		DstGroupID: pg.DstGroupID,
		Action:     pg.Action,
		Protocols:  pg.Protocols,
		PortRanges: portRanges,
		CreatedAt:  pg.CreatedAt,
		UpdatedAt:  pg.UpdatedAt,
	}
}

func fromModelsGroupLink(m *models.GroupLink) *GroupLink {
	if m == nil {
		return nil
	}
	return &GroupLink{
		ID:         m.ID,
		AccountID:  m.AccountID,
		SrcGroupID: m.SrcGroupID,
		DstGroupID: m.DstGroupID,
		Action:     m.Action,
		Protocols:  m.Protocols,
		PortRanges: m.PortRanges, // go-pg handles any as jsonb
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

// SavePeerGroup saves a peer group to the database (Upsert).
func (s *GroupStore) SavePeerGroup(group *models.PeerGroup) error {
	pgGroup := fromModelsPeerGroup(group)
	_, err := s.db.Model(pgGroup).OnConflict("(id) DO UPDATE").Insert()
	return err
}

// GetPeerGroup retrieves a peer group by ID.
func (s *GroupStore) GetPeerGroup(groupID string) (*models.PeerGroup, error) {
	pgGroup := new(PeerGroup)
	err := s.db.Model(pgGroup).Where("id = ?", groupID).Select()
	if err != nil {
		return nil, err
	}
	return toModelsPeerGroup(pgGroup), nil
}

// ListPeerGroups lists all peer groups for an account.
func (s *GroupStore) ListPeerGroups(accountID string) ([]*models.PeerGroup, error) {
	var pgGroups []*PeerGroup
	err := s.db.Model(&pgGroups).Where("account_id = ?", accountID).Select()
	if err != nil {
		return nil, err
	}

	groups := make([]*models.PeerGroup, len(pgGroups))
	for i, pg := range pgGroups {
		groups[i] = toModelsPeerGroup(pg)
	}
	return groups, nil
}

// DeletePeerGroup deletes a peer group by ID.
func (s *GroupStore) DeletePeerGroup(groupID string) error {
	_, err := s.db.Model((*PeerGroup)(nil)).Where("id = ?", groupID).Delete()
	return err
}

// ListAllPeerGroups lists all peer groups across all accounts.
func (s *GroupStore) ListAllPeerGroups() ([]*models.PeerGroup, error) {
	var pgGroups []*PeerGroup
	err := s.db.Model(&pgGroups).Select()
	if err != nil {
		return nil, err
	}

	groups := make([]*models.PeerGroup, len(pgGroups))
	for i, pg := range pgGroups {
		groups[i] = toModelsPeerGroup(pg)
	}
	return groups, nil
}

// SaveGroupLink saves a group link to the database (Upsert).
func (s *GroupStore) SaveGroupLink(link *models.GroupLink) error {
	pgLink := fromModelsGroupLink(link)
	_, err := s.db.Model(pgLink).OnConflict("(id) DO UPDATE").Insert()
	return err
}

// GetGroupLink retrieves a group link by ID.
func (s *GroupStore) GetGroupLink(linkID string) (*models.GroupLink, error) {
	pgLink := new(GroupLink)
	err := s.db.Model(pgLink).Where("id = ?", linkID).Select()
	if err != nil {
		return nil, err
	}
	return toModelsGroupLink(pgLink), nil
}

// ListGroupLinks lists all group links for an account.
func (s *GroupStore) ListGroupLinks(accountID string) ([]*models.GroupLink, error) {
	var pgLinks []*GroupLink
	err := s.db.Model(&pgLinks).Where("account_id = ?", accountID).Select()
	if err != nil {
		return nil, err
	}

	links := make([]*models.GroupLink, len(pgLinks))
	for i, pg := range pgLinks {
		links[i] = toModelsGroupLink(pg)
	}
	return links, nil
}

// DeleteGroupLink deletes a group link by ID.
func (s *GroupStore) DeleteGroupLink(linkID string) error {
	_, err := s.db.Model((*GroupLink)(nil)).Where("id = ?", linkID).Delete()
	return err
}

// ListAllGroupLinks lists all group links across all accounts.
func (s *GroupStore) ListAllGroupLinks() ([]*models.GroupLink, error) {
	var pgLinks []*GroupLink
	err := s.db.Model(&pgLinks).Select()
	if err != nil {
		return nil, err
	}

	links := make([]*models.GroupLink, len(pgLinks))
	for i, pg := range pgLinks {
		links[i] = toModelsGroupLink(pg)
	}
	return links, nil
}
