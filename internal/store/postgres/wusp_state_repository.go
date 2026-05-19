package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
	"github.com/google/uuid"
)

type wuspDeviceStateRepository struct {
	db *pg.DB
}

// NewWUSPDeviceStateRepository creates a postgres-backed WUSP device state repository.
func NewWUSPDeviceStateRepository(db *pg.DB) store.WUSPDeviceStateRepository {
	return &wuspDeviceStateRepository{db: db}
}

func (r *wuspDeviceStateRepository) Upsert(state *store.WUSPDeviceStateData) error {
	if state.ID == "" {
		state.ID = uuid.New().String()
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}
	state.UpdatedAt = time.Now()

	var snapshot json.RawMessage
	if len(state.DeviceSnapshot) > 0 {
		snapshot = json.RawMessage(state.DeviceSnapshot)
	} else {
		snapshot = json.RawMessage("[]")
	}

	model := &WUSPDeviceState{
		ID:              state.ID,
		PeerID:          state.PeerID,
		AccountID:       state.AccountID,
		LastSyncAt:      state.LastSyncAt,
		SyncError:       state.SyncError,
		DeviceSnapshot:  snapshot,
		DeviceID:        state.DeviceID,
		Manufacturer:    state.Manufacturer,
		ProductClass:    state.ProductClass,
		SerialNumber:    state.SerialNumber,
		SoftwareVersion: state.SoftwareVersion,
		HardwareVersion: state.HardwareVersion,
		WUSPEnable:      state.WUSPEnable,
		WUSPStatus:      state.WUSPStatus,
		WUSPVersion:     state.WUSPVersion,
		CreatedAt:       state.CreatedAt,
		UpdatedAt:       state.UpdatedAt,
	}

	_, err := r.db.Model(model).
		OnConflict("(peer_id) DO UPDATE").
		Set(`
			account_id       = EXCLUDED.account_id,
			last_sync_at     = EXCLUDED.last_sync_at,
			sync_error       = EXCLUDED.sync_error,
			device_snapshot  = EXCLUDED.device_snapshot,
			device_id        = EXCLUDED.device_id,
			manufacturer     = EXCLUDED.manufacturer,
			product_class    = EXCLUDED.product_class,
			serial_number    = EXCLUDED.serial_number,
			software_version = EXCLUDED.software_version,
			hardware_version = EXCLUDED.hardware_version,
			wusp_enable      = EXCLUDED.wusp_enable,
			wusp_status      = EXCLUDED.wusp_status,
			wusp_version     = EXCLUDED.wusp_version,
			updated_at       = EXCLUDED.updated_at
		`).
		Insert()
	if err != nil {
		return fmt.Errorf("wusp_device_states upsert: %w", err)
	}
	return nil
}

func (r *wuspDeviceStateRepository) GetByPeer(peerID string) (*store.WUSPDeviceStateData, error) {
	model := &WUSPDeviceState{}
	err := r.db.Model(model).Where("peer_id = ?", peerID).Select()
	if err != nil {
		return nil, fmt.Errorf("wusp_device_states get peer %s: %w", peerID, err)
	}
	return toWUSPDeviceStateData(model), nil
}

func (r *wuspDeviceStateRepository) GetByAccount(accountID string) ([]*store.WUSPDeviceStateData, error) {
	var models []*WUSPDeviceState
	err := r.db.Model(&models).Where("account_id = ?", accountID).Select()
	if err != nil {
		return nil, fmt.Errorf("wusp_device_states list account %s: %w", accountID, err)
	}
	out := make([]*store.WUSPDeviceStateData, len(models))
	for i, m := range models {
		out[i] = toWUSPDeviceStateData(m)
	}
	return out, nil
}

func (r *wuspDeviceStateRepository) Delete(peerID string) error {
	_, err := r.db.Model(&WUSPDeviceState{}).Where("peer_id = ?", peerID).Delete()
	if err != nil {
		return fmt.Errorf("wusp_device_states delete peer %s: %w", peerID, err)
	}
	return nil
}

func toWUSPDeviceStateData(m *WUSPDeviceState) *store.WUSPDeviceStateData {
	var snapshot []byte
	if len(m.DeviceSnapshot) > 0 {
		snapshot = []byte(m.DeviceSnapshot)
	}
	return &store.WUSPDeviceStateData{
		ID:              m.ID,
		PeerID:          m.PeerID,
		AccountID:       m.AccountID,
		LastSyncAt:      m.LastSyncAt,
		SyncError:       m.SyncError,
		DeviceSnapshot:  snapshot,
		DeviceID:        m.DeviceID,
		Manufacturer:    m.Manufacturer,
		ProductClass:    m.ProductClass,
		SerialNumber:    m.SerialNumber,
		SoftwareVersion: m.SoftwareVersion,
		HardwareVersion: m.HardwareVersion,
		WUSPEnable:      m.WUSPEnable,
		WUSPStatus:      m.WUSPStatus,
		WUSPVersion:     m.WUSPVersion,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}
