// Package grpc holds the service-implementation registry that powers
// in-process dispatch. The package name is a historical artifact — there
// is NO gRPC runtime here anymore, no TCP listener, no proto serialization
// at runtime. The "Server" types in this package are plain Go structs
// whose methods are invoked directly by the portal's WebSocket bridge.
//
// The proto-generated types in api/proto are still used as the request /
// response data structures; replacing them with handwritten Go structs is
// a separate follow-up.
package core

import (
	"context"
	"time"

	"WantasticCore/internal/config"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/email"
	"WantasticCore/internal/server"
	"WantasticCore/internal/tenant"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// GRPCServer is the in-process registry of service implementations.
// Despite the name, it does NOT serve gRPC: the type is kept so callers
// (portal, adminbot, admin bootstrap) can resolve any service handler by
// reaching into this struct.
type GRPCServer struct {
	// Underlying overlay + registry — exposed for in-process callers (admin bootstrap).
	overlayServer  *server.Server
	tenantRegistry tenant.Registry

	// Service implementations
	accountSvc  *AccountServiceServer
	websshSvc   *WebSSHServiceServer
	webproxySvc *WebProxyServiceServer
	peerSvc     *PeerServiceServer
	wuspSvc     *WUSPService
	routerOSSvc *RouterOSService

	// Tenant services
	tenantRegSvc     *TenantRegistrationServiceServer
	tenantPortalSvc  *TenantPortalServiceServer
	tenantBillingSvc *TenantBillingServiceServer
	tenantDataSvc    *TenantDataServiceServer
	wantasticSvc     *WantasticServiceServer
}

// OverlayServer returns the underlying *server.Server for in-process callers.
func (s *GRPCServer) OverlayServer() *server.Server { return s.overlayServer }

// TenantRegistry returns the tenant.Registry handed to this server at construction.
func (s *GRPCServer) TenantRegistry() tenant.Registry { return s.tenantRegistry }

// Config holds service-registry construction parameters. The legacy
// gRPC-specific knobs (EnableAuth, EnableTLS, EnableMTLS, MTLSConfig,
// AllowedOrigins) are retained as fields for source-compat with existing
// callers, but they're now no-ops: nothing serves gRPC.
type Config struct {
	EnableAuth     bool          // kept for caller compat; unused
	EnableTLS      bool          // kept for caller compat; unused
	EnableMTLS     bool          // kept for caller compat; unused
	MTLSConfig     any           // kept for caller compat; unused
	WebSSHBaseURL  string        // Base URL for WebSSH access
	AllowedOrigins []string      // kept for caller compat; unused
	AppConfig      *config.Config

	WinboxAddr     string
	TenantRegistry tenant.Registry

	SMTPClient *email.SMTPService

	// Notification manager for per-tenant offline alerts
	NotificationManager *tenant.NotificationManager

	// Redis client for distributed state (e.g., device flows)
	RedisClient *redis.Client

	// Cipher for enrollment tokens (kept for portal device-enroll compatibility)
	EnrollmentCipher *crypto.EnrollmentTokenCipher

	// Pre-created services (optional - for webhook integration)
	TenantRegistrationService *TenantRegistrationServiceServer
}

// NewGRPCServer constructs the in-process service registry. Despite the
// name (kept for caller compatibility), it does NOT bind a TCP port and
// does NOT serve gRPC. The `addr` argument is accepted for source compat
// and ignored.
func NewGRPCServer(srv *server.Server, _ string, cfg *Config) (*GRPCServer, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	// Create service implementations
	accountSvc := NewAccountServiceServer(srv)
	websshSvc := NewWebSSHServiceServer(srv, cfg.WebSSHBaseURL)
	webproxySvc := NewWebProxyServiceServer(srv, srv.GetWebProxyHandler(), cfg.TenantRegistry)
	peerSvc := NewPeerServiceServer(srv)
	wuspSvc := NewWUSPService(srv.WUSPCtrl())
	routerOSSvc := NewRouterOSService(srv)
	if cfg.RedisClient != nil {
		wuspSvc.SetRedis(&redisAdapter{cfg.RedisClient})
	}

	// Create tenant service implementations
	var tenantRegSvc *TenantRegistrationServiceServer
	var tenantPortalSvc *TenantPortalServiceServer
	var tenantBillingSvc *TenantBillingServiceServer
	var tenantDataSvc *TenantDataServiceServer
	var wantasticSvc *WantasticServiceServer

	if cfg.TenantRegistry != nil {
		if cfg.TenantRegistrationService != nil {
			tenantRegSvc = cfg.TenantRegistrationService
		} else {
			tenantRegSvc = NewTenantRegistrationServiceServer(srv, cfg.TenantRegistry, cfg.SMTPClient)
		}
		tenantPortalSvc = NewTenantPortalServiceServer(
			srv,
			cfg.TenantRegistry,
			cfg.SMTPClient,
			cfg.AppConfig,
			cfg.NotificationManager,
			cfg.EnrollmentCipher,
		)
		tenantBillingSvc = NewTenantBillingServiceServer(srv, cfg.TenantRegistry, cfg.SMTPClient)
		tenantDataSvc = NewTenantDataServiceServer(srv, cfg.TenantRegistry)
		wantasticSvc = NewWantasticServiceServer(srv, cfg.TenantRegistry)
		log.Debug().Msg("Tenant services initialized (in-process; no gRPC listener)")
	} else {
		log.Warn().Msg("Tenant services disabled (no TenantRegistry configured)")
	}

	return &GRPCServer{
		overlayServer:    srv,
		tenantRegistry:   cfg.TenantRegistry,
		accountSvc:       accountSvc,
		websshSvc:        websshSvc,
		webproxySvc:      webproxySvc,
		peerSvc:          peerSvc,
		wuspSvc:          wuspSvc,
		routerOSSvc:      routerOSSvc,
		tenantRegSvc:     tenantRegSvc,
		tenantPortalSvc:  tenantPortalSvc,
		tenantBillingSvc: tenantBillingSvc,
		tenantDataSvc:    tenantDataSvc,
		wantasticSvc:     wantasticSvc,
	}, nil
}

// Serve was the gRPC TCP listener loop. Now a blocking no-op so existing
// goroutine call sites (`go grpcServer.Serve()`) keep their shape without
// spinning hot. The goroutine returns when caller cancels its context;
// since this no-op never returns, callers should treat it as fire-and-forget.
func (s *GRPCServer) Serve() error {
	log.Debug().Msg("gRPC listener disabled; service registry running in-process only")
	select {} // park forever
}

// Stop is a no-op now that there's no gRPC server to gracefully shut down.
func (s *GRPCServer) Stop() {
	log.Debug().Msg("gRPC stop: no-op (no listener)")
}

// redisAdapter wraps *redis.Client to implement the RedisClient interface
// used by WUSPService for backup token management.
type redisAdapter struct {
	c *redis.Client
}

func (r *redisAdapter) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	return r.c.Set(ctx, key, value, exp).Err()
}

func (r *redisAdapter) Get(ctx context.Context, key string) (string, error) {
	return r.c.Get(ctx, key).Result()
}

func (r *redisAdapter) Del(ctx context.Context, keys ...string) error {
	return r.c.Del(ctx, keys...).Err()
}
