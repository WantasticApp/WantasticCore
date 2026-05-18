// Package services — hub_manager.go: in-process service routing.
//
// InProcessRouter is a thin holder around the in-process *core.Services
// bundle. After the gRPC rip, there's no dial pool, no remote selection,
// no descriptor-based dispatch — just a typed reference to the services
// that the WebSocket dispatcher invokes directly. The type is retained
// because a handful of callers still take an *InProcessRouter for legacy
// signatures (TenantProxy construction, share-link routing, etc.).
//
// The old `InProcessClientConn` + `lookupServiceDesc` + `serverFor` were
// removed when every caller migrated to direct p.services.X dispatch.

package services

import (
	core "WantasticCore/internal/core"
)

// InProcessRouter wraps the in-process *core.Services bundle.
type InProcessRouter struct {
	services *core.Services
}

// NewInProcessRouter returns an InProcessRouter backed by an in-process
// *core.Services bundle.
func NewInProcessRouter(services *core.Services) *InProcessRouter {
	return &InProcessRouter{services: services}
}

// Services returns the in-process service bundle.
func (hm *InProcessRouter) Services() *core.Services {
	if hm == nil {
		return nil
	}
	return hm.services
}

// Close is a no-op for the in-process router. Kept so callers that
// `defer hm.Close()` keep compiling.
func (hm *InProcessRouter) Close() {}

// GetRouter returns self (helper used in some proxy paths).
func (hm *InProcessRouter) GetRouter() *InProcessRouter { return hm }
