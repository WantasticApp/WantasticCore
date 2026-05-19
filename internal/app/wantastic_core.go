package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"WantasticCore/internal/auth"
	"WantasticCore/internal/config"
	"WantasticCore/internal/crypto"
	"WantasticCore/internal/email"
	"WantasticCore/internal/core"
	"WantasticCore/internal/server"
	"WantasticCore/internal/store"
	"WantasticCore/internal/store/adapter"
	_ "WantasticCore/internal/store/cache"
	"WantasticCore/internal/store/postgres"
	"WantasticCore/internal/store/registry"
	"WantasticCore/internal/tenant"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// WantasticCoreConfig holds configuration for the Wantastic Core
type WantasticCoreConfig struct {
	GRPCAddr        string
	MetricsAddr     string
	WsAddr          string
	WebhookAddr     string // HTTP webhook server address (e.g., ":8090")
	MetricsInterval time.Duration
	WinboxAddr      string
	ServerEndpoint  string
	EnableAuth      bool
	Debug           bool
	// Network configuration
	SubnetPools   []string
	SharedPort    int // Shared UDP port (shared mode)
	MaxTenants    int
	MaxPeersTotal int

	// API Key authentication
	AllowedOrigins []string

	// mTLS configuration
	EnableTLS  bool // Enable standard TLS (server-side only)
	EnableMTLS bool
	MTLSConfig *auth.MTLSConfig

	// Tenant configuration
	TenantEnabled bool // Enable tenant services

	// SMTP Email configuration
	SMTPEnabled  bool   // Enable SMTP email
	SMTPHost     string // SMTP server host
	SMTPPort     int    // SMTP server port
	SMTPUseTLS   bool   // Use TLS
	SMTPUser     string // SMTP username
	SMTPPassword string // SMTP password
	SMTPFrom     string // Sender email address
	SMTPFromName string // Sender display name

	// Hooks configuration for notification unsubscribe links
	HooksSecretKey string // Hex-encoded secret key for hook tokens
	HooksBaseURL   string // Base URL for hooks (e.g., "https://console.wantastic.app/hooks")

	// Full config reference for endpoint configuration
	FullConfig *config.Config // Complete config for endpoints

	// AfterStart fires once the gRPC server is registered and serving.
	// The merged binary uses it to launch the in-process portal and adminbot
	// against the Services bundle. The returned cleanup runs before gRPC
	// shutdown so embedded services can wind down cleanly.
	AfterStart func(grpcServer *core.GRPCServer) (cleanup func())
}

// StartWantasticCore starts the Wantastic Core
func StartWantasticCore(cfg WantasticCoreConfig) *error {
	// Declare cron jobs at function level for shutdown handling
	var inactivityCron *tenant.InactivityCron
	var notificationManager *tenant.NotificationManager

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if cfg.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Determine effective endpoint for logging
	effectiveEndpoint := cfg.ServerEndpoint
	if effectiveEndpoint == "" {
		effectiveEndpoint = "localhost (auto-detect)"
	}

	log.Debug().
		Str("grpc_addr", cfg.GRPCAddr).
		Str("server_endpoint", effectiveEndpoint).
		Msg(" Starting Wantastic Core (PostgreSQL backend)")

	// Create server configuration
	serverCfg := server.DefaultConfig()
	// if cfg.FullConfig != nil {
	// 	serverCfg.DB = postgres.OptionsFromConfig(cfg.FullConfig.Database)
	// }

	// Use centralized endpoint configuration if available
	if cfg.FullConfig != nil && cfg.FullConfig.Endpoints.WireguardServer != "" {
		serverCfg.ServerEndpoint = cfg.FullConfig.Endpoints.WireguardServer
		log.Debug().
			Str("wireguard_server", serverCfg.ServerEndpoint).
			Msg(" Using centralized endpoints.wireguard_server for WireGuard peer configs")
	} else {
		// Debug logging to understand why endpoint config not used
		if cfg.FullConfig == nil {
			log.Warn().Msg(" FullConfig is nil - cannot use endpoints.wireguard_server")
		} else if cfg.FullConfig.Endpoints.WireguardServer == "" {
			log.Warn().
				Str("winbox_server", cfg.FullConfig.Endpoints.WinboxServer).
				Msg(" endpoints.wireguard_server is empty in config")
		}

		serverCfg.ServerEndpoint = cfg.ServerEndpoint
		log.Debug().
			Str("server_endpoint", serverCfg.ServerEndpoint).
			Msg(" Using network.server_endpoint for WireGuard peer configs (no endpoints.wireguard_server configured)")
	}

	if cfg.FullConfig != nil {
		serverCfg.AdvertiseAddr = cfg.FullConfig.Server.AdvertiseAddr
	}

	// Initialize unified store (PostgreSQL + Redis)
	if cfg.FullConfig == nil {
		log.Fatal().Msg("Full configuration is required (PostgreSQL + Redis)")
		return nil
	}

	storeCfg := store.Config{
		Host:          cfg.FullConfig.Database.Host,
		Port:          cfg.FullConfig.Database.Port,
		User:          cfg.FullConfig.Database.User,
		Password:      cfg.FullConfig.Database.Password,
		Database:      cfg.FullConfig.Database.Database,
		SSLMode:       cfg.FullConfig.Database.SSLMode,
		PoolSize:      cfg.FullConfig.Database.PoolSize,
		MinIdleConns:  cfg.FullConfig.Database.MinIdleConns,
		MaxRetries:    cfg.FullConfig.Database.MaxRetries,
		RedisEnabled:  cfg.FullConfig.Redis.Enabled,
		RedisAddr:     cfg.FullConfig.Redis.Addr,
		RedisPassword: cfg.FullConfig.Redis.Password,
		RedisDB:       cfg.FullConfig.Redis.DB,
	}

	if err := store.Initialize(storeCfg); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize unified database store")
		return &err
	}
	defer func() {
		if store.IsInitialized() {
			_ = store.DB().Close()
		}
	}()

	// Get Redis client from unified store
	redisClient := store.DB().Redis()
	if redisClient != nil {
		log.Debug().Msg(" Redis client initialized from unified store")
	}

	serverCfg.SubnetPools = cfg.SubnetPools
	serverCfg.SharedPort = cfg.SharedPort
	serverCfg.MaxTenants = cfg.MaxTenants
	serverCfg.MaxPeersTotal = cfg.MaxPeersTotal
	serverCfg.GRPCAddr = cfg.GRPCAddr

	// Apply database migrations (prod-ready schema management)
	if err := store.DB().Migrate(); err != nil {
		log.Fatal().Err(err).Msg("Failed to apply database migrations")
		return &err
	}

	pgStore := postgres.NewWithDB(store.DB().PG())

	// Initialize main server
	ctx := context.Background()

	// Create account store using adapter
	accRepo := store.DB().Accounts()
	accStore := adapter.NewAccountStore(accRepo)

	srv, err := server.NewServer(ctx, serverCfg, accStore)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create server")
	}
	defer srv.Close()

	// Restore WireGuard devices from database (critical for restart recovery)
	if err := srv.RestoreTenantsFromDatabase(); err != nil {
		log.Fatal().Err(err).Msg("Failed to restore tenants from database")
	}

	// Start distributed WebSSH kill listner
	go srv.StartWebSSHListener(ctx)

	// Configure gRPC server and WebSSH
	grpcConfig := &core.Config{
		EnableAuth:     cfg.EnableAuth,
		AllowedOrigins: cfg.AllowedOrigins,
		EnableTLS:      cfg.EnableTLS,
		EnableMTLS:     cfg.EnableMTLS,
		MTLSConfig:     cfg.MTLSConfig,
		WebSSHBaseURL:  "", // Terminal connections now handled via main tenant WebSocket
		WinboxAddr:     cfg.WinboxAddr,
		AppConfig:      cfg.FullConfig, // Pass full config for endpoints
		RedisClient:    redisClient,    // Shared Redis client
	}

	// Initialize tenant services if enabled
	log.Debug().
		Bool("tenant_enabled", cfg.TenantEnabled).
		Msg(" Tenant config check")

	if cfg.TenantEnabled {
		log.Debug().Msg("Initializing tenant services with PostgreSQL")

		// Create Tenant Store (implements Registry)
		tenantRegistry := postgres.NewTenantStore(pgStore.DB())

		// Wrap with Redis cache if enabled
		if redisClient != nil {
			log.Debug().Msg(" Enabling Redis session cache")
			grpcConfig.TenantRegistry = registry.NewCachedRegistry(tenantRegistry, redisClient)
		} else {
			grpcConfig.TenantRegistry = tenantRegistry
		}

		log.Debug().Msg(" PostgreSQL store initialized")

		// Start periodic session cleanup goroutine (every hour)
		go func(registry tenant.Registry) {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()

			// Run once at startup
			if count, err := registry.CleanupExpiredSessions(); err != nil {
				log.Warn().Err(err).Msg("Initial session cleanup failed")
			} else if count > 0 {
				log.Debug().Int("count", count).Msg("🧹 Initial session cleanup completed")
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if count, err := registry.CleanupExpiredSessions(); err != nil {
						log.Warn().Err(err).Msg("Periodic session cleanup failed")
					} else if count > 0 {
						log.Debug().Int("count", count).Msg("🧹 Periodic session cleanup completed")
					}
				}
			}
		}(tenantRegistry)

		// Billing/Stripe integration removed — Phase 2 refactor.
		// Twilio/SMS verification removed — Phase 3 refactor.

		// Initialize SMTP email client if enabled
		if cfg.SMTPEnabled && cfg.SMTPHost != "" {
			smtpClient := email.NewSMTPService(email.SMTPConfig{
				Host:     cfg.SMTPHost,
				Port:     cfg.SMTPPort,
				User:     cfg.SMTPUser,
				UseTLS:   cfg.SMTPUseTLS,
				Password: cfg.SMTPPassword,
				From:     cfg.SMTPFrom,
				FromName: cfg.SMTPFromName,
			})
			grpcConfig.SMTPClient = smtpClient
			log.Debug().Msg(" SMTP email client initialized")
		} else if cfg.SMTPEnabled {
			// SMTP enabled but no host - use dev mode
			smtpClient := email.NewSMTPService(email.SMTPConfig{
				From:     cfg.SMTPFrom,
				FromName: cfg.SMTPFromName,
			})
			grpcConfig.SMTPClient = smtpClient
			log.Warn().Msg(" SMTP email client in dev mode (no host - codes logged locally)")
		}

		// Create TenantRegistrationServiceServer (billing + sms/twilio parameters removed in Phases 2/3).
		tenantRegService := core.NewTenantRegistrationServiceServer(
			srv,
			grpcConfig.TenantRegistry,
			grpcConfig.SMTPClient,
		)
		grpcConfig.TenantRegistrationService = tenantRegService
		log.Debug().Msg(" TenantRegistrationService created")

		// Resolve the shared secret used by every Cipher (enrollment, hook,
		// session). Done up front so the enrollment cipher is available even
		// when SMTP is off — enrollment tokens are independent of email and
		// were silently dropped before, returning "current state" errors to
		// users trying to generate them.
		var secretKey []byte
		if cfg.HooksSecretKey != "" {
			decoded, err := hex.DecodeString(cfg.HooksSecretKey)
			if err != nil {
				log.Warn().Err(err).Msg(" Invalid hooks secret key (must be hex) - automated features requiring encryption disabled")
			} else {
				secretKey = decoded
			}
		} else {
			log.Warn().Msg(" Hooks secret key not configured - using fallback development key")
			secretKey = []byte("wantastic-fallback-secret-for-dev-only")
		}
		if len(secretKey) > 0 {
			if enrollmentCipher, err := crypto.NewEnrollmentTokenCipher(secretKey); err != nil {
				log.Warn().Err(err).Msg(" Failed to create enrollment cipher - automated device enrollment disabled")
			} else {
				grpcConfig.EnrollmentCipher = enrollmentCipher
				log.Debug().Msg(" Enrollment token cipher configured")
			}
		}

		// Email-driven plumbing: inactivity cron, notification workers, and
		// the unsubscribe-link hook cipher only make sense when SMTP is on.
		if grpcConfig.SMTPClient != nil {
			inactivityCron = tenant.NewInactivityCron(
				grpcConfig.TenantRegistry,
				// grpcConfig.TenantStores,
				grpcConfig.SMTPClient,
				srv, // srv implements AccountCleaner.DeleteAccount
				tenant.DefaultInactivityConfig(),
				redisClient,
			)
			inactivityCron.Start()
			log.Debug().Msg(" Inactivity cron started (30-day reminder, 45-day follow-up reminder for free accounts)")

			// Initialize notification manager for per-tenant offline alerts
			notificationManager = tenant.NewNotificationManager(
				grpcConfig.TenantRegistry,
				srv.GetPeerStore(),
				grpcConfig.SMTPClient,
				tenant.DefaultNotificationConfig(),
				redisClient,
			)
			grpcConfig.NotificationManager = notificationManager

			// Restore notification workers from database (start workers for tenants with enabled alerts)
			if err := notificationManager.RestoreFromDatabase(); err != nil {
				log.Warn().Err(err).Msg(" Failed to restore notification workers from database")
			} else {
				log.Debug().Msg(" Notification manager initialized and workers restored")
			}

			// Initialize Hook Cipher (needs both key and BaseURL)
			if len(secretKey) > 0 && cfg.HooksBaseURL != "" {
				hookCipher, err := crypto.NewNotificationHookCipher(secretKey)
				if err != nil {
					log.Warn().Err(err).Msg(" Failed to create hook cipher - unsubscribe links disabled")
				} else {
					notificationManager.SetHookCipher(hookCipher, cfg.HooksBaseURL)
					log.Debug().
						Str("base_url", cfg.HooksBaseURL).
						Msg(" Notification hook cipher configured (unsubscribe links enabled)")
				}
			} else if cfg.HooksBaseURL == "" {
				log.Warn().Msg(" Hooks BaseURL not configured - unsubscribe links disabled")
			}
		} else {
			log.Warn().Msg(" Inactivity cron disabled (email service not configured)")
		}

		// Stripe Listener / webhook integration removed (Phase 2).
		_ = tenantRegService

	} else {
		log.Warn().Msg("  Tenant services disabled (missing TenantDBPath or TenantEncryptKey)")
	}

	// Build the in-process service registry (no gRPC listener — services
	// are invoked directly from the portal's WebSocket dispatcher and the
	// adminbot via the registry's accessors).
	grpcServer, err := core.NewGRPCServer(srv, "", grpcConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to build service registry")
		return &err
	}

	// Embedded services hook (portal, adminbot) — runs in the same process.
	var embeddedCleanup func()
	if cfg.AfterStart != nil {
		embeddedCleanup = cfg.AfterStart(grpcServer)
	}

	// Start Winbox multiplexer in goroutine
	go func() {
		winboxAddr := cfg.WinboxAddr // Standard Winbox port
		log.Debug().Str("winbox_addr", winboxAddr).Msg("Starting Winbox multiplexer")
		if err := srv.StartWinboxMultiplexer(winboxAddr); err != nil {
			log.Error().Err(err).Msg("Winbox multiplexer failed to start")
		}
	}()

	log.Debug().
		Str("grpc_addr", cfg.GRPCAddr).
		Str("metrics_addr", cfg.MetricsAddr).
		Bool("auth_enabled", cfg.EnableAuth).
		Msg(" gRPC server started successfully")

	// Health probe for load balancers
	if cfg.MetricsAddr != "" {
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, "ok")
			})
			if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
				log.Error().Err(err).Str("addr", cfg.MetricsAddr).Msg("health server failed")
			}
		}()
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Debug().Msg("Shutting down...")

	// Create shutdown timeout context
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Tear down embedded portal/adminbot before gRPC so they see clean shutdown.
	if embeddedCleanup != nil {
		log.Debug().Msg("Stopping embedded services (portal/adminbot)")
		embeddedCleanup()
	}

	// Stop inactivity cron if running
	if inactivityCron != nil {
		log.Debug().Msg("Stopping inactivity cron")
		inactivityCron.Stop()
	}

	// Stop notification manager if running
	if notificationManager != nil {
		log.Debug().Msg("Stopping notification manager")
		notificationManager.Stop()
	}

	// Stop gRPC server gracefully
	log.Debug().Msg("Stopping gRPC server")
	done := make(chan struct{})
	go func() {
		defer close(done)
		grpcServer.Stop() // Use our wrapper's Stop method
	}()

	select {
	case <-done:
		log.Debug().Msg("gRPC server stopped gracefully")
	case <-time.After(10 * time.Second):
		log.Warn().Msg("gRPC server stop timeout - force exit")
	}

	// Close server with timeout
	log.Debug().Msg("Closing server")
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		srv.Close()
	}()

	select {
	case <-serverDone:
		log.Debug().Msg("Server closed successfully")
	case <-shutdownCtx.Done():
		log.Error().Msg("Server close timeout - exiting anyway")
	}

	log.Debug().Msg("Goodbye!")
	return nil
}
