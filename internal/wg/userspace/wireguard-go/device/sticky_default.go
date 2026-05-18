//go:build !linux

package device

import (
	"WantasticCore/internal/wg/userspace/wireguard-go/conn"
	"WantasticCore/internal/wg/userspace/wireguard-go/rwcancel"
)

func (device *Device) startRouteListener(_ conn.Bind) (*rwcancel.RWCancel, error) {
	return nil, nil
}
