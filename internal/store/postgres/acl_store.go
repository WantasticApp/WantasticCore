package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/models"

	"github.com/go-pg/pg/v10"
)

// ACLStore implements the ACL storage using PostgreSQL.
type ACLStore struct {
	db *pg.DB
}

// NewACLStore creates a new ACL store.
func NewACLStore(db *pg.DB) *ACLStore {
	return &ACLStore{db: db}
}

// SaveRule saves an ACL rule to the database.
func (s *ACLStore) SaveRule(rule *models.ACLRule) error {
	pgRule := toPGACLRule(rule)
	if pgRule.CreatedAt.IsZero() {
		pgRule.CreatedAt = time.Now()
	}
	pgRule.UpdatedAt = time.Now()

	_, err := s.db.Model(pgRule).
		OnConflict("(id) DO UPDATE").
		Insert()
	if err != nil {
		return fmt.Errorf("failed to save acl rule: %w", err)
	}

	return nil
}

// GetRule retrieves an ACL rule by ID and account ID.
func (s *ACLStore) GetRule(accountID, ruleID string) (*models.ACLRule, error) {
	pgRule := &ACLRule{ID: ruleID}
	err := s.db.Model(pgRule).
		Where("account_id = ?", accountID).
		WherePK().
		Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, nil // Return nil if not found, let caller handle
		}
		return nil, fmt.Errorf("failed to get acl rule: %w", err)
	}

	return toModelACLRule(pgRule), nil
}

// DeleteRule deletes an ACL rule.
func (s *ACLStore) DeleteRule(accountID, ruleID string) error {
	pgRule := &ACLRule{ID: ruleID}
	result, err := s.db.Model(pgRule).
		Where("account_id = ?", accountID).
		WherePK().
		Delete()
	if err != nil {
		return fmt.Errorf("failed to delete acl rule: %w", err)
	}
	
	if result.RowsAffected() == 0 {
		return fmt.Errorf("acl rule not found")
	}

	return nil
}

// ListRules lists all ACL rules for an account.
func (s *ACLStore) ListRules(accountID string) ([]*models.ACLRule, error) {
	var pgRules []*ACLRule
	err := s.db.Model(&pgRules).
		Where("account_id = ?", accountID).
		Order("priority ASC"). // Usually order by priority for ACLs
		Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list acl rules: %w", err)
	}

	rules := make([]*models.ACLRule, len(pgRules))
	for i, pgRule := range pgRules {
		rules[i] = toModelACLRule(pgRule)
	}

	return rules, nil
}

// ListAllRules lists all ACL rules across all accounts.
func (s *ACLStore) ListAllRules() ([]*models.ACLRule, error) {
	var pgRules []*ACLRule
	err := s.db.Model(&pgRules).
		Order("priority ASC").
		Select()
	if err != nil {
		return nil, fmt.Errorf("failed to list all acl rules: %w", err)
	}

	rules := make([]*models.ACLRule, len(pgRules))
	for i, pgRule := range pgRules {
		rules[i] = toModelACLRule(pgRule)
	}

	return rules, nil
}

// Helper functions to convert between models

func toPGACLRule(m *models.ACLRule) *ACLRule {
	return &ACLRule{
		ID:            m.ID,
		AccountID:     m.AccountID,
		Name:          m.Name,
		Action:        m.Action,
		Protocol:      m.Protocol,
		SourceIPs:     m.SourceIPs,
		DestIPs:       m.DestIPs,
		DestPorts:     m.DestPorts,
		Priority:      m.Priority,
		Description:   m.Description,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		SourcePeerIDs: m.SourcePeerIDs,
		DestPeerIDs:   m.DestPeerIDs,
		Services:      m.Services,
	}
}

func toModelACLRule(pg *ACLRule) *models.ACLRule {
	return &models.ACLRule{
		ID:            pg.ID,
		AccountID:     pg.AccountID,
		Name:          pg.Name,
		Action:        pg.Action,
		Protocol:      pg.Protocol,
		SourceIPs:     pg.SourceIPs,
		DestIPs:       pg.DestIPs,
		DestPorts:     pg.DestPorts,
		Priority:      pg.Priority,
		Description:   pg.Description,
		CreatedAt:     pg.CreatedAt,
		UpdatedAt:     pg.UpdatedAt,
		SourcePeerIDs: pg.SourcePeerIDs,
		DestPeerIDs:   pg.DestPeerIDs,
		Services:      pg.Services,
		// Optimization fields are initialized to zero values and should be
		// populated by the business logic/service layer if needed.
	}
}
