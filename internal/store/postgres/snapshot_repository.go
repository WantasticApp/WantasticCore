package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"WantasticCore/internal/store"

	"github.com/go-pg/pg/v10"
	"github.com/google/uuid"
)

type deviceSnapshotRepository struct {
	db *pg.DB
}

// NewDeviceSnapshotRepository creates a postgres-backed device snapshot repository.
func NewDeviceSnapshotRepository(db *pg.DB) store.DeviceSnapshotRepository {
	return &deviceSnapshotRepository{db: db}
}

func (r *deviceSnapshotRepository) Create(snap *store.DeviceSnapshotData) error {
	if snap.ID == "" {
		snap.ID = uuid.New().String()
	}
	now := time.Now()
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = now
	}
	snap.UpdatedAt = now

	snapshot := json.RawMessage(snap.DeviceSnapshot)
	if len(snapshot) == 0 {
		snapshot = json.RawMessage("[]")
	}

	model := &DeviceSnapshot{
		ID:              snap.ID,
		AccountID:       snap.AccountID,
		Name:            snap.Name,
		Protocol:        snap.Protocol,
		Manufacturer:    snap.Manufacturer,
		ProductClass:    snap.ProductClass,
		SerialNumber:    snap.SerialNumber,
		SoftwareVersion: snap.SoftwareVersion,
		HardwareVersion: snap.HardwareVersion,
		DeviceSnapshot:  snapshot,
		BackupFile:      snap.BackupFile,
		BackupName:      snap.BackupName,
		BackupSize:      snap.BackupSize,
		UploadToken:     snap.UploadToken,
		CreatedAt:       snap.CreatedAt,
		UpdatedAt:       snap.UpdatedAt,
	}
	_, err := r.db.Model(model).Insert()
	if err != nil {
		return fmt.Errorf("device_snapshots create: %w", err)
	}
	return nil
}

func (r *deviceSnapshotRepository) Get(id, accountID string) (*store.DeviceSnapshotData, error) {
	model := &DeviceSnapshot{}
	err := r.db.Model(model).
		Where("id = ? AND account_id = ?", id, accountID).
		Select()
	if err != nil {
		return nil, fmt.Errorf("device_snapshots get %s: %w", id, err)
	}
	return toDeviceSnapshotData(model), nil
}

func (r *deviceSnapshotRepository) GetByUploadToken(token string) (*store.DeviceSnapshotData, error) {
	if token == "" {
		return nil, fmt.Errorf("empty upload token")
	}
	model := &DeviceSnapshot{}
	err := r.db.Model(model).
		Where("upload_token = ?", token).
		Select()
	if err != nil {
		return nil, fmt.Errorf("device_snapshots get by token: %w", err)
	}
	return toDeviceSnapshotData(model), nil
}

func (r *deviceSnapshotRepository) List(accountID string) ([]*store.DeviceSnapshotData, error) {
	var models []*DeviceSnapshot
	err := r.db.Model(&models).
		Where("account_id = ?", accountID).
		OrderExpr("created_at DESC").
		Select()
	if err != nil {
		return nil, fmt.Errorf("device_snapshots list account %s: %w", accountID, err)
	}
	out := make([]*store.DeviceSnapshotData, len(models))
	for i, m := range models {
		out[i] = toDeviceSnapshotData(m)
	}
	return out, nil
}

func (r *deviceSnapshotRepository) ListByProtocol(accountID, protocol string) ([]*store.DeviceSnapshotData, error) {
	var models []*DeviceSnapshot
	err := r.db.Model(&models).
		Where("account_id = ? AND protocol = ?", accountID, protocol).
		OrderExpr("created_at DESC").
		Select()
	if err != nil {
		return nil, fmt.Errorf("device_snapshots list account %s protocol %s: %w", accountID, protocol, err)
	}
	out := make([]*store.DeviceSnapshotData, len(models))
	for i, m := range models {
		out[i] = toDeviceSnapshotData(m)
	}
	return out, nil
}

func (r *deviceSnapshotRepository) Update(snap *store.DeviceSnapshotData) error {
	snap.UpdatedAt = time.Now()

	snapshot := json.RawMessage(snap.DeviceSnapshot)
	if len(snapshot) == 0 {
		snapshot = json.RawMessage("[]")
	}

	// Use a column-explicit UPDATE so empty/zero values are persisted (e.g.
	// rotating UploadToken to empty string, or storing the very first
	// BackupFile when DeviceSnapshot is still empty). UpdateNotZero would
	// otherwise silently drop those writes.
	result, err := r.db.Model(&DeviceSnapshot{}).
		Set("name = ?", snap.Name).
		Set("protocol = ?", snap.Protocol).
		Set("manufacturer = ?", snap.Manufacturer).
		Set("product_class = ?", snap.ProductClass).
		Set("serial_number = ?", snap.SerialNumber).
		Set("software_version = ?", snap.SoftwareVersion).
		Set("hardware_version = ?", snap.HardwareVersion).
		Set("device_snapshot = ?", snapshot).
		Set("backup_file = ?", snap.BackupFile).
		Set("backup_name = ?", snap.BackupName).
		Set("backup_size = ?", snap.BackupSize).
		Set("upload_token = ?", snap.UploadToken).
		Set("updated_at = ?", snap.UpdatedAt).
		Where("id = ? AND account_id = ?", snap.ID, snap.AccountID).
		Update()
	if err != nil {
		return fmt.Errorf("device_snapshots update %s: %w", snap.ID, err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("device_snapshots update %s: not found or unauthorized", snap.ID)
	}
	return nil
}

func (r *deviceSnapshotRepository) Delete(id, accountID string) error {
	result, err := r.db.Model(&DeviceSnapshot{}).
		Where("id = ? AND account_id = ?", id, accountID).
		Delete()
	if err != nil {
		return fmt.Errorf("device_snapshots delete %s: %w", id, err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("device_snapshots delete %s: not found or unauthorized", id)
	}
	return nil
}

func toDeviceSnapshotData(m *DeviceSnapshot) *store.DeviceSnapshotData {
	var snapshot []byte
	if len(m.DeviceSnapshot) > 0 {
		snapshot = []byte(m.DeviceSnapshot)
	}
	return &store.DeviceSnapshotData{
		ID:              m.ID,
		AccountID:       m.AccountID,
		Name:            m.Name,
		Protocol:        m.Protocol,
		Manufacturer:    m.Manufacturer,
		ProductClass:    m.ProductClass,
		SerialNumber:    m.SerialNumber,
		SoftwareVersion: m.SoftwareVersion,
		HardwareVersion: m.HardwareVersion,
		DeviceSnapshot:  snapshot,
		BackupFile:      m.BackupFile,
		BackupName:      m.BackupName,
		BackupSize:      m.BackupSize,
		UploadToken:     m.UploadToken,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}
