package postgres

import (
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
)

type aclRepository struct {
	db *pg.DB
}

func NewACLRepository(db *pg.DB) store.ACLRepository {
	return &aclRepository{db: db}
}

func (r *aclRepository) SaveRule(rule *store.ACLRuleData) error {
	model := &ACLRule{
		ID:            rule.ID,
		AccountID:     rule.AccountID,
		Name:          rule.Name,
		Action:        rule.Action,
		Protocol:      rule.Protocol,
		SourceIPs:     rule.SourceIPs,
		DestIPs:       rule.DestIPs,
		DestPorts:     rule.DestPorts,
		Priority:      rule.Priority,
		Description:   rule.Description,
		CreatedAt:     rule.CreatedAt,
		UpdatedAt:     rule.UpdatedAt,
		SourcePeerIDs: rule.SourcePeerIDs,
		DestPeerIDs:   rule.DestPeerIDs,
		Services:      rule.Services,
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

func (r *aclRepository) GetRule(ruleID string) (*store.ACLRuleData, error) {
	model := &ACLRule{ID: ruleID}
	err := r.db.Model(model).WherePK().Select()
	if err != nil {
		if err == pg.ErrNoRows {
			return nil, fmt.Errorf("acl rule not found: %s", ruleID)
		}
		return nil, err
	}
	return r.toData(model), nil
}

func (r *aclRepository) ListByAccount(accountID string) ([]*store.ACLRuleData, error) {
	var models []ACLRule
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		Order("priority ASC").
		Select()
	if err != nil {
		return nil, err
	}
	result := make([]*store.ACLRuleData, len(models))
	for i := range models {
		result[i] = r.toData(&models[i])
	}
	return result, nil
}

func (r *aclRepository) DeleteRule(ruleID string) error {
	result, err := r.db.Model((*ACLRule)(nil)).
		Where("id = ?", ruleID).
		Delete()
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("acl rule not found")
	}
	return nil
}

func (r *aclRepository) toData(m *ACLRule) *store.ACLRuleData {
	return &store.ACLRuleData{
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
