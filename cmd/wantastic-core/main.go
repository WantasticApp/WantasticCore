// Command wantastic-core is the single, all-in-one entry point for the
// merged Wantastic Core: it runs the gRPC core, the web portal, and (optionally)
// the WhatsApp adminbot in one process, wiring everything through in-process
// service calls — no internal gRPC over the network.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"WantasticCore/internal/admin"
	"WantasticCore/internal/adminbot"
	"WantasticCore/internal/app"
	"WantasticCore/internal/auth"
	"WantasticCore/internal/config"
	"WantasticCore/internal/copilot"
	core "WantasticCore/internal/core"
	"WantasticCore/internal/portalsrv"
	"WantasticCore/internal/setupweb"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// Sentinel errors returned by the in-app Copilot SetAPIKey callback.
// Surfaced to the browser so the UI can show actionable copy.
var (
	errCopilotUnavailable = errors.New("copilot service not initialized")
	errCopilotKeyRejected = errors.New("API key was rejected by the Anthropic client (empty after trim)")
)

// reconcileNginxAndFirewall re-runs the helper scripts that the setup
// wizard would normally invoke, but on every boot — so non-interactive
// env-driven installs (which skip the wizard's post-submit hook) still
// land in the production nginx site config + firewall state.
//
// All steps are best-effort: outside the all-in-one image the helper
// scripts don't exist and the function is a no-op.
func reconcileNginxAndFirewall(cfg *config.Config) {
	domain := strings.TrimSpace(cfg.Network.ServerEndpoint)
	if domain == "" {
		zlog.Warn().Msg("reconcile: no domain in config — skipping nginx + firewall provisioning")
		return
	}
	console := strings.TrimSpace(cfg.Endpoints.WireguardServer)
	if console == "" || console == domain {
		console = domain
	}

	// TLS mode: if LE was already issued for this domain (live/<domain>/
	// dir exists), use the production template. Otherwise the operator
	// either chose self-signed or LE hasn't run yet — fall back to
	// self-signed so nginx has a working cert pair.
	tlsMode := "self-signed"
	if _, err := os.Stat("/var/lib/wantastic/letsencrypt/live/" + domain); err == nil {
		tlsMode = "letsencrypt"
	}

	if _, err := os.Stat("/usr/local/bin/nginx-render.sh"); err == nil {
		cmd := exec.Command("/usr/local/bin/nginx-render.sh", domain, console, tlsMode)
		if out, err := cmd.CombinedOutput(); err != nil {
			zlog.Warn().Err(err).Bytes("output", out).Msg("reconcile: nginx-render.sh failed")
		} else {
			zlog.Info().Str("domain", domain).Str("tls_mode", tlsMode).Msg("reconcile: nginx site rendered")
		}
	}

	if _, err := exec.LookPath("nginx"); err == nil {
		_ = exec.Command("nginx", "-s", "reload").Run()
	}

	if os.Getenv("WANTASTIC_FIREWALL") != "0" {
		if _, err := os.Stat("/usr/local/bin/firewall-apply.sh"); err == nil {
			cmd := exec.Command("/usr/local/bin/firewall-apply.sh")
			if out, err := cmd.CombinedOutput(); err != nil {
				zlog.Warn().Err(err).Bytes("output", out).Msg("reconcile: firewall-apply.sh failed")
			}
		}
	}
}

func main() {
	configFile := flag.String("config", "config.yaml", "Path to YAML configuration file (created interactively on first run if missing)")
	grpcAddr := flag.String("grpc-addr", "", "gRPC server address")
	metricsAddr := flag.String("metrics-addr", "", "Prometheus metrics address")
	serverEndpoint := flag.String("server-endpoint", "", "Server endpoint for peer configs (auto-detect if empty)")
	enableAuth := flag.Bool("enable-auth", false, "Enable authentication")
	debug := flag.Bool("debug", false, "Enable debug logging")
	subnetPools := flag.String("subnet-pools", "", "Global subnet pool CIDR (e.g., '10.0.0.0/8')")
	maxPeers := flag.Int("max-peers", 0, "Maximum total peers")
	websocketAddr := flag.String("ws-addr", "", "WebSocket server address for WebSSH")
	autoCertGen := flag.Bool("auto-cert-gen", true, "Enable mTLS certificate auto-generation")
	certDir := flag.String("cert-dir", "", "Directory containing mTLS certificates")
	winboxAddr := flag.String("winbox-addr", "", "Address for Winbox (e.g., ':8291')")
	sharedPort := flag.Int("shared-port", 0, "Shared port for WireGuard (e.g., 51820)")

	portalHTTPAddr := flag.String("portal-http", ":8001", "HTTP listen address for the embedded portal")
	portalRedisAddr := flag.String("portal-redis", "localhost:6379", "Redis address for the embedded portal")
	portalHooksSecret := flag.String("portal-hooks-secret", "", "HMAC secret for hook URL tokens (random if empty)")
	portalAuth0PublicDomain := flag.String("auth0-public-domain", "", "Auth0 public domain (or env WANTASTIC_AUTH0_PUBLIC_DOMAIN)")
	portalAuth0OAuthDomain := flag.String("auth0-oauth-domain", "", "Auth0 OAuth domain (or env WANTASTIC_AUTH0_OAUTH_DOMAIN)")
	portalAuth0ClientID := flag.String("auth0-client-id", "", "Auth0 client ID (or env WANTASTIC_AUTH0_CLIENT_ID)")
	portalSessionSecret := flag.String("session-secret", "", "Portal session secret (or env WANTASTIC_SESSION_SECRET)")

	flag.Parse()

	setup, err := loadOrRunSetup(*configFile)
	if err != nil {
		log.Fatalf("Config setup failed: %v", err)
	}
	cfg := setup.Config
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	config.Sharedconfig = cfg

	// Reconcile nginx + firewall every boot, regardless of how the config
	// was produced (web wizard, CLI wizard, env-driven NONINTERACTIVE, or
	// an already-existing config.yaml). Idempotent: the helpers no-op when
	// nothing has changed. Lets a fresh container land in the production
	// nginx site config (instead of the bootstrap one that proxies to the
	// long-gone wizard on :8443).
	reconcileNginxAndFirewall(cfg)

	// Command-line overrides win over file.
	if *grpcAddr != "" {
		cfg.Server.GRPCAddr = *grpcAddr
	}
	if *metricsAddr != "" {
		cfg.Server.MetricsAddr = *metricsAddr
	}
	if *websocketAddr != "" {
		cfg.Server.WebSocketAddr = *websocketAddr
	}
	if *serverEndpoint != "" {
		cfg.Network.ServerEndpoint = *serverEndpoint
	}
	if *debug {
		cfg.Server.Debug = true
	}
	if *subnetPools != "" {
		cfg.Network.SubnetPools = strings.Split(*subnetPools, ",")
	}
	if *maxPeers > 0 {
		cfg.Network.MaxPeersTotal = *maxPeers
	}
	if *winboxAddr != "" {
		cfg.Server.WinboxAddr = *winboxAddr
	}
	if *sharedPort > 0 {
		cfg.Network.SharedPort = *sharedPort
	}
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "auto-cert-gen" {
			cfg.Auth.MTLS.AutoGenerate = *autoCertGen
		}
		if f.Name == "cert-dir" {
			cfg.Auth.MTLS.CertDir = *certDir
		}
	})

	metricsInterval, err := cfg.Metrics.GetUpdateInterval()
	if err != nil {
		log.Fatalf("Invalid metrics.update_interval: %v", err)
	}

	var mtlsConfig *auth.MTLSConfig
	var enableMTLS, enableTLS bool
	if cfg.Auth.Enable {
		enableTLS = true
		mtlsConfig = &auth.MTLSConfig{
			CertDir:        cfg.Auth.MTLS.CertDir,
			ServerCertFile: cfg.Auth.MTLS.ServerCert,
			ServerKeyFile:  cfg.Auth.MTLS.ServerKey,
			CACertFile:     cfg.Auth.MTLS.CACert,
			ClientCertFile: cfg.Auth.MTLS.ClientCert,
			ClientKeyFile:  cfg.Auth.MTLS.ClientKey,
			AutoGenerate:   cfg.Auth.MTLS.AutoGenerate,
		}
	}

	// Embedded services: ctx is cancelled before gRPC shutdown so portal/adminbot
	// stop cleanly. WaitGroup lets us join the goroutines during shutdown.
	embeddedCtx, embeddedCancel := context.WithCancel(context.Background())
	var embeddedWG sync.WaitGroup

	appCfg := app.WantasticCoreConfig{
		GRPCAddr:        cfg.Server.GRPCAddr,
		MetricsAddr:     cfg.Server.MetricsAddr,
		WsAddr:          cfg.Server.WebSocketAddr,
		WebhookAddr:     cfg.Server.WebhookAddr,
		MetricsInterval: metricsInterval,
		ServerEndpoint:  cfg.Network.ServerEndpoint,
		EnableAuth:      cfg.Auth.Enable || *enableAuth,
		Debug:           cfg.Server.Debug,
		SubnetPools:     cfg.Network.SubnetPools,
		SharedPort:      cfg.Network.SharedPort,
		MaxPeersTotal:   cfg.Network.MaxPeersTotal,
		AllowedOrigins:  cfg.Auth.AllowedOrigins,
		EnableTLS:       enableTLS,
		EnableMTLS:      enableMTLS,
		MTLSConfig:      mtlsConfig,
		WinboxAddr:      cfg.Server.WinboxAddr,

		TenantEnabled: cfg.Tenant.SessionSigningKey != "",

		SMTPEnabled:  cfg.SMTP.Enabled,
		SMTPHost:     cfg.SMTP.Host,
		SMTPPort:     cfg.SMTP.Port,
		SMTPUseTLS:   cfg.SMTP.UseTLS,
		SMTPUser:     cfg.SMTP.User,
		SMTPPassword: cfg.SMTP.Password,
		SMTPFrom:     cfg.SMTP.From,
		SMTPFromName: cfg.SMTP.FromName,

		HooksSecretKey: cfg.Hooks.SecretKey,
		HooksBaseURL:   cfg.Hooks.BaseURL,

		FullConfig: cfg,

		AfterStart: func(grpcServer *core.GRPCServer) func() {
			services := grpcServer.ServiceBundle()
			srv := grpcServer.OverlayServer()
			registry := grpcServer.TenantRegistry()

			// One admin service instance shared by the bootstrap step and the portal.
			var adminSvc *admin.Service
			if srv != nil && registry != nil {
				adminSvc = admin.New(srv, registry)
			}

			// Copilot is always wired so the in-app GetStatus / SetAPIKey
			// endpoints work even before an admin enters a key. When the
			// AdminBot.Claude.APIKey is set up-front, seed the LLM now;
			// otherwise the service starts disabled and the admin enables
			// it from the Copilot setup screen.
			copilotSvc, err := copilot.New(copilot.Config{}, nil, adminSvc, services)
			if err != nil {
				zlog.Error().Err(err).Msg("copilot: init failed; disabled")
			} else if key := strings.TrimSpace(cfg.AdminBot.Claude.APIKey); key != "" {
				llm := copilot.NewClaudeLLM(key)
				if llm.Enabled() {
					copilotSvc.SetLLM(llm)
				}
			}
			// Persistence callback used by the in-app "Set API key" form.
			// Updates the live LLM AND writes the key to config.yaml so it
			// survives a process restart. Errors propagate to the UI.
			onSetCopilotAPIKey := func(apiKey string) error {
				if copilotSvc == nil {
					return errCopilotUnavailable
				}
				llm := copilot.NewClaudeLLM(apiKey)
				if !llm.Enabled() {
					return errCopilotKeyRejected
				}
				copilotSvc.SetLLM(llm)
				cfg.AdminBot.Claude.APIKey = apiKey
				if err := config.Save(*configFile, cfg); err != nil {
					zlog.Error().Err(err).Str("path", *configFile).Msg("copilot: API key saved in memory but failed to persist to config.yaml")
					return err
				}
				zlog.Info().Msg("copilot: API key updated via in-app form; persisted to config.yaml")
				return nil
			}

			// Bootstrap admin from the setup wizard, if one was collected.
			if adminSvc != nil && setup.Admin.Email != "" {
				if err := adminSvc.BootstrapAdmin(setup.Admin.Email, setup.Admin.FullName, setup.Admin.Password, setup.Admin.MaxPeers); err != nil {
					zlog.Error().Err(err).Msg("admin bootstrap failed (you may need to create the admin manually)")
				} else {
					zlog.Info().Str("email", setup.Admin.Email).Msg("Bootstrap admin ensured")
				}
			}

			// Portal — always on.
			portalArgs := portalsrv.Args{
				HTTPAddr:           *portalHTTPAddr,
				CertDir:            cfg.Auth.MTLS.CertDir,
				MTLS:               cfg.Auth.MTLS.AutoGenerate,
				HooksSecret:        firstNonEmpty(*portalHooksSecret, cfg.Hooks.SecretKey),
				RedisAddr:          *portalRedisAddr,
				AutoCertGen:        cfg.Auth.MTLS.AutoGenerate,
				Auth0PublicDomain:  *portalAuth0PublicDomain,
				Auth0OAuthDomain:   *portalAuth0OAuthDomain,
				Auth0ClientID:      *portalAuth0ClientID,
				SessionSecret:      *portalSessionSecret,
				Services:           services,
				Admin:              adminSvc,
				Copilot:            copilotSvc,
				OnSetCopilotAPIKey: onSetCopilotAPIKey,
			}
			embeddedWG.Add(1)
			go func() {
				defer embeddedWG.Done()
				if err := portalsrv.Start(embeddedCtx, portalArgs); err != nil && embeddedCtx.Err() == nil {
					zlog.Error().Err(err).Msg("embedded portal exited with error")
				}
			}()

			// AdminBot — opt-in via config (`adminbot.enabled: true`).
			if cfg.AdminBot.Enabled {
				botCfg := adminbot.FromUnified(cfg)
				if err := botCfg.Validate(); err != nil {
					zlog.Error().Err(err).Msg("adminbot disabled: invalid config")
				} else {
					level, perr := zerolog.ParseLevel(botCfg.LogLevel)
					if perr != nil {
						level = zerolog.InfoLevel
					}
					botLogger := zlog.Logger.With().Str("service", botCfg.BotName).Logger().Level(level)
					bot, err := adminbot.NewBot(embeddedCtx, botCfg, services, botLogger)
					if err != nil {
						zlog.Error().Err(err).Msg("adminbot disabled: NewBot failed")
					} else {
						embeddedWG.Add(1)
						go func() {
							defer embeddedWG.Done()
							defer bot.Close()
							if err := bot.Start(embeddedCtx); err != nil && embeddedCtx.Err() == nil {
								zlog.Error().Err(err).Msg("adminbot exited with error")
							}
						}()
					}
				}
			}

			return func() {
				if copilotSvc != nil {
					copilotSvc.Close()
				}
				embeddedCancel()
				embeddedWG.Wait()
			}
		},
	}

	app.StartWantasticCore(appCfg)
	_ = os.Stderr
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// loadOrRunSetup is the configuration entry point. Resolution order:
//
//  1. If configPath exists, load and return it as-is.
//  2. If WANTASTIC_WEB_SETUP=1, OR stdin is not a TTY and the required
//     non-interactive env vars are absent, boot the web setup wizard
//     (binds :8443, serves a self-signed HTTPS page). After the operator
//     submits, the wizard writes configPath and we exit cleanly so the
//     supervisor restarts in normal mode.
//  3. Otherwise fall back to the existing CLI wizard / env-driven path
//     (config.LoadOrSetup), which preserves the legacy bootstrap behavior.
func loadOrRunSetup(configPath string) (*config.SetupResult, error) {
	if _, err := os.Stat(configPath); err == nil {
		cfg, err := config.Load(configPath)
		if err != nil {
			return nil, err
		}
		return &config.SetupResult{Config: cfg}, nil
	}

	// NONINTERACTIVE takes precedence so CI / docker-compose with bootstrap
	// env vars never hangs on a wizard that nobody's clicking through.
	if os.Getenv("WANTASTIC_SETUP_NONINTERACTIVE") == "1" {
		return config.LoadOrSetup(configPath)
	}

	webSetup := os.Getenv("WANTASTIC_WEB_SETUP") == "1"
	if !webSetup {
		// Use the legacy resolution (CLI wizard for TTY, env-driven otherwise).
		return config.LoadOrSetup(configPath)
	}

	addr := os.Getenv("WANTASTIC_WEB_SETUP_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	zlog.Info().Str("addr", addr).Msg("no config.yaml — starting web setup wizard")
	res, err := setupweb.Run(addr, configPath)
	if err != nil {
		return nil, err
	}
	zlog.Info().
		Str("console", res.ConsoleHost).
		Str("config", configPath).
		Msg("web setup complete; restarting with the new config")
	// Exit cleanly so the supervisor (Docker / systemd) brings the
	// process back up — this time it'll find the config we just wrote
	// and start in normal mode.
	os.Exit(0)
	return nil, nil // unreachable, satisfies the compiler
}
