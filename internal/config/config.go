// Package config provides configuration loading from YAML files and environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Network   NetworkConfig   `yaml:"network"`
	Auth      AuthConfig      `yaml:"auth"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	SMTP      SMTPConfig      `yaml:"smtp"`
	Tenant    TenantConfig    `yaml:"tenant"`
	Endpoints EndpointsConfig `yaml:"endpoints"`
	Hooks     HooksConfig     `yaml:"hooks"`
	Auth0     Auth0Config     `yaml:"auth0"`
	AdminBot  AdminBotConfig  `yaml:"adminbot"`
}

var Sharedconfig *Config

// ServerConfig holds server-level configuration.
type ServerConfig struct {
	GRPCAddr      string `yaml:"grpc_addr"`
	WebSocketAddr string `yaml:"websocket_addr"`
	WebhookAddr   string `yaml:"webhook_addr"` // HTTP webhook server for Twilio callbacks
	MetricsAddr   string `yaml:"metrics_addr"`
	WinboxAddr    string `yaml:"winbox_addr"`
	AdvertiseAddr string `yaml:"advertise_addr"`
	Debug         bool   `yaml:"debug"`
}

// NetworkConfig holds network-related configuration.
type NetworkConfig struct {
	SubnetPools    []string `yaml:"subnet_pools"`    // Global subnet pool CIDR (e.g., "10.0.0.0/8")
	ServerEndpoint string   `yaml:"server_endpoint"` // Server IP/domain WITHOUT port (port is auto-assigned per tenant)
	SharedPort     int      `yaml:"shared_port"`     // UDP port for shared mode (default: 51820)
	MaxPeersTotal  int      `yaml:"max_peers_total"` // Maximum total peers across all tenants
}

// WebSSHConfig holds WebSSH-related configuration.
type WebSSHConfig struct {
	Domain string `yaml:"domain"` // WebSocket domain (e.g., "ws://localhost:8022" or "wss://ssh.example.com")
}

// AuthConfig holds authentication-related configuration.
type AuthConfig struct {
	Enable         bool       `yaml:"enable"`
	AllowedOrigins []string   `yaml:"allowed_origins"` // Allowed server origins for API key auth
	MTLS           MTLSConfig `yaml:"mtls"`            // mTLS configuration
}

// MTLSConfig holds mTLS certificate configuration.
type MTLSConfig struct {
	// automaticly enabled if auth enabled
	CertDir      string `yaml:"cert_dir"`
	ServerCert   string `yaml:"server_cert"`
	ServerKey    string `yaml:"server_key"`
	CACert       string `yaml:"ca_cert"`
	ClientCert   string `yaml:"client_cert"`
	ClientKey    string `yaml:"client_key"`
	AutoGenerate bool   `yaml:"auto_generate"`
}

// MetricsConfig holds metrics-related configuration.
type MetricsConfig struct {
	UpdateInterval string `yaml:"update_interval"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host                  string `yaml:"host"`
	Port                  int    `yaml:"port"`
	User                  string `yaml:"user"`
	Password              string `yaml:"password"`
	Database              string `yaml:"database"`
	SSLMode               string `yaml:"ssl_mode"`
	PoolSize              int    `yaml:"pool_size"`
	MinIdleConns          int    `yaml:"min_idle_conns"`
	MaxRetries            int    `yaml:"max_retries"`
	RetryStatementTimeout string `yaml:"retry_statement_timeout"`
}

// RedisConfig holds optional Redis settings.
type RedisConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Addr     string `yaml:"addr"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// SMTPConfig holds SMTP email configuration (used for admin notifications
// and password-reset emails — optional).
type SMTPConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	UseTLS   bool   `yaml:"use_tls"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	FromName string `yaml:"from_name"`
}

// TenantConfig holds tenant-related settings. After the admin-managed
// rewrite, this is intentionally minimal — registration / plans / billing
// were removed.
type TenantConfig struct {
	// SessionSigningKey signs tenant session cookies. Required.
	SessionSigningKey string `yaml:"session_signing_key"`
}

// AdminBotConfig holds optional WhatsApp adminbot settings. The bot only
// starts when AdminBot.Enabled is true.
type AdminBotConfig struct {
	Enabled       bool                 `yaml:"enabled"`
	BotName       string               `yaml:"bot_name"`
	LogLevel      string               `yaml:"log_level"`
	WhatsApp      AdminBotWhatsAppConfig `yaml:"whatsapp"`
	Claude        AdminBotClaudeConfig `yaml:"claude"`
	GRPCTimeoutMs int                  `yaml:"grpc_timeout_ms"`
}

// AdminBotWhatsAppConfig holds WhatsApp pairing/session settings for the bot.
type AdminBotWhatsAppConfig struct {
	StorePath             string   `yaml:"store_path"`
	AllowedGroups         []string `yaml:"allowed_groups"`
	ReplySignatureEnabled bool     `yaml:"reply_signature_enabled"`
	LoginTimeout          string   `yaml:"login_timeout"`
	DeviceName            string   `yaml:"device_name"`
	HistoryWindow         string   `yaml:"history_window"`
}

// AdminBotClaudeConfig holds Anthropic API access for the bot's tool loop.
type AdminBotClaudeConfig struct {
	APIKey string `yaml:"api_key"`
}

// EndpointsConfig holds service endpoint configuration for frontend.
type EndpointsConfig struct {
	WinboxServer    string `yaml:"winbox_server"`    // Winbox proxy server domain (e.g., "winbox.wantastic.app")
	WireguardServer string `yaml:"wireguard_server"` // WireGuard server endpoint (e.g., "wg.wantastic.app")
}

// Auth0Config holds Auth0 OAuth 2.0 / OIDC configuration.
// Used for the Device Authorization Grant flow (RFC 8628 — Tailscale-style).
type Auth0Config struct {
	Enabled  bool   `yaml:"enabled"`   // Enable Auth0 device flow
	Domain   string `yaml:"domain"`    // Auth0 domain, e.g. "dev-xxxx.us.auth0.com"
	ClientID string `yaml:"client_id"` // Auth0 application Client ID (native app)
	Audience string `yaml:"audience"`  // Auth0 API audience (may be empty)
}

// HooksConfig holds notification hook configuration.
type HooksConfig struct {
	BaseURL     string `yaml:"base_url"`     // Base URL for hook endpoints (e.g., "https://console.wantastic.app/hooks")
	SecretKey   string `yaml:"secret_key"`   // Secret key for encrypting hook tokens (min 16 bytes hex)
	TokenExpiry string `yaml:"token_expiry"` // Token expiry duration (e.g., "168h" = 7 days)
}

// GetTokenExpiry parses and returns the hook token expiry duration.
func (c *HooksConfig) GetTokenExpiry() (time.Duration, error) {
	if c.TokenExpiry == "" {
		return 7 * 24 * time.Hour, nil // Default: 7 days
	}
	d, err := time.ParseDuration(c.TokenExpiry)
	if err != nil {
		return 0, fmt.Errorf("invalid token_expiry format '%s': %w (example: '168h', '7d')", c.TokenExpiry, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("token_expiry must be positive, got: %v", d)
	}
	return d, nil
}

// Save writes the configuration back to a YAML file. The write is atomic:
// data lands in <path>.tmp and is renamed over <path> only after a clean
// fsync, so a crash mid-write never leaves the operator with a truncated
// config.yaml. Used by runtime settings flows (e.g. the in-app Copilot
// API-key form) that need to persist changes across restarts.
func Save(configPath string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := configPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, configPath, err)
	}
	return nil
}

// Load loads configuration from a YAML file.
func Load(configPath string) (*Config, error) {
	// Read the file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Load defaults
	cfg := Default()

	// Parse YAML into defaults (merging)
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Environment variable overrides
	if host := os.Getenv("WANTASTIC_DB_HOST"); host != "" {
		cfg.Database.Host = host
	}
	if pass := os.Getenv("WANTASTIC_DB_PASSWORD"); pass != "" {
		cfg.Database.Password = pass
	}
	if user := os.Getenv("WANTASTIC_DB_USER"); user != "" {
		cfg.Database.User = user
	}
	if dbName := os.Getenv("WANTASTIC_DB_NAME"); dbName != "" {
		cfg.Database.Database = dbName
	}
	if port := os.Getenv("WANTASTIC_DB_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Database.Port = p
		}
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 5432
	}
	if addr := os.Getenv("WANTASTIC_REDIS_HOST"); addr != "" {
		// Redis host form linode is usually just the hostname
		if !strings.Contains(addr, ":") {
			cfg.Redis.Addr = addr + ":6379"
		} else {
			cfg.Redis.Addr = addr
		}
	}
	if pass := os.Getenv("WANTASTIC_REDIS_PASSWORD"); pass != "" {
		cfg.Redis.Password = pass
	}
	if ep := os.Getenv("WANTASTIC_SERVER_ENDPOINT"); ep != "" {
		cfg.Network.ServerEndpoint = ep
	}
	if gaddr := os.Getenv("WANTASTIC_GRPC_ADDR"); gaddr != "" {
		cfg.Server.GRPCAddr = gaddr
	}
	if aaddr := os.Getenv("WANTASTIC_ADVERTISE_ADDR"); aaddr != "" {
		cfg.Server.AdvertiseAddr = aaddr
	}
	if ws := os.Getenv("WANTASTIC_ENDPOINTS_WINBOX_SERVER"); ws != "" {
		cfg.Endpoints.WinboxServer = ws
	}
	if wgs := os.Getenv("WANTASTIC_ENDPOINTS_WIREGUARD_SERVER"); wgs != "" {
		cfg.Endpoints.WireguardServer = wgs
	}
	if authEnable := os.Getenv("WANTASTIC_AUTH_ENABLE"); authEnable != "" {
		if b, err := strconv.ParseBool(authEnable); err == nil {
			cfg.Auth.Enable = b
		}
	}
	if mtlsAutoGen := os.Getenv("WANTASTIC_MTLS_AUTO_GENERATE"); mtlsAutoGen != "" {
		if b, err := strconv.ParseBool(mtlsAutoGen); err == nil {
			cfg.Auth.MTLS.AutoGenerate = b
		}
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	// Validate subnet pool CIDR
	if len(c.Network.SubnetPools) == 0 {
		return fmt.Errorf("network.subnet_pools is required")
	}

	// Validate server_endpoint does not contain a port
	// In multi-tenant mode, ports are automatically assigned per tenant
	if c.Network.ServerEndpoint != "" && strings.Contains(c.Network.ServerEndpoint, ":") {
		return fmt.Errorf("network.server_endpoint should not include a port (e.g., use '192.168.1.10' not '192.168.1.10:51820'). Ports are automatically assigned per tenant")
	}

	// Auto-calculate MaxPeersTotal if not set (based on /27 blocks)
	// Each /27 block provides 29 usable IPs (32 - 3 reserved)
	if c.Network.MaxPeersTotal <= 0 {
		// Estimate based on subnet pools
		// For simplicity, assume reasonable defaults based on common pool sizes
		// 10.0.0.0/8 = ~536,870 blocks × 29 IPs = ~15.5M peers
		// 172.16.0.0/12 = ~16,384 blocks × 29 IPs = ~475K peers
		c.Network.MaxPeersTotal = 1000000 // Default 1M peer capacity
	}

	return nil
}

// GetUpdateInterval parses and returns the metrics update interval.
func (c *MetricsConfig) GetUpdateInterval() (time.Duration, error) {
	if c.UpdateInterval == "" {
		return 30 * time.Second, nil // Default: 30 seconds
	}
	d, err := time.ParseDuration(c.UpdateInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid update_interval format '%s': %w (example: '30s', '1m')", c.UpdateInterval, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("update_interval must be positive, got: %v", d)
	}
	return d, nil
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCAddr:      ":50051",
			MetricsAddr:   ":9091",
			WebSocketAddr: ":8081",
			WebhookAddr:   ":8090",
			WinboxAddr:    ":8292",
			Debug:         false,
		},
		Network: NetworkConfig{
			SubnetPools:    []string{"10.0.0.0/8", "172.16.0.0/12"},
			ServerEndpoint: "localhost",
			SharedPort:     51820,
			MaxPeersTotal:  160000,
		},
		Database: DatabaseConfig{
			Host:                  "localhost",
			Port:                  5432,
			User:                  "wantastic",
			Password:              "mysecretpassword",
			Database:              "wantastic_dev",
			SSLMode:               "disable",
			PoolSize:              10,
			MinIdleConns:          2,
			MaxRetries:            3,
			RetryStatementTimeout: "5s",
		},
		Redis: RedisConfig{
			Enabled:  true,
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		Auth: AuthConfig{
			Enable:         false,
			AllowedOrigins: []string{},
		},
		Metrics: MetricsConfig{
			UpdateInterval: "30s",
		},
		SMTP: SMTPConfig{
			Enabled: false,
		},
		Tenant: TenantConfig{
			SessionSigningKey: "",
		},
		AdminBot: AdminBotConfig{
			Enabled:  false,
			BotName:  "wantastic-adminbot",
			LogLevel: "info",
			WhatsApp: AdminBotWhatsAppConfig{
				StorePath:    "/var/lib/wantastic/adminbot/whatsapp.db",
				LoginTimeout: "5m",
				DeviceName:   "WantasticBot",
				HistoryWindow: "24h",
			},
		},
		// Endpoints intentionally left empty so they fall back to
		// Network.ServerEndpoint at runtime. Filled in by the setup
		// wizard (or by hand in config.yaml) from the operator's domain.
		Endpoints: EndpointsConfig{},
		Hooks: HooksConfig{
			BaseURL:     "https://console.wantastic.app/hooks",
			SecretKey:   "",
			TokenExpiry: "168h",
		},
		Auth0: Auth0Config{
			Enabled:  false,
			Domain:   "",
			ClientID: "",
			Audience: "",
		},
	}
}
