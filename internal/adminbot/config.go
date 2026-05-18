package adminbot

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corecfg "WantasticCore/internal/config"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath       = "/etc/bot.conf"
	defaultClaudeModel      = "claude-haiku-4-5"
	defaultReplySignatureOn = true
)

type Config struct {
	BotName  string         `yaml:"bot_name"`
	LogLevel string         `yaml:"log_level"`
	DB       DBConfig       `yaml:"database"`
	GRPC     GRPCConfig     `yaml:"grpc"`
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	Claude   ClaudeConfig   `yaml:"claude"`
}

type DBConfig struct {
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

type GRPCConfig struct {
	Address            string `yaml:"address"`
	UseTLS             bool   `yaml:"use_tls"`
	ServerName         string `yaml:"server_name"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	Timeout            string `yaml:"timeout"`
}

type WhatsAppConfig struct {
	StorePath             string   `yaml:"store_path"`
	AllowedGroups         []string `yaml:"allowed_groups"`
	ReplySignatureEnabled bool     `yaml:"reply_signature_enabled"`
	LoginTimeout          string   `yaml:"login_timeout"`
	DeviceName            string   `yaml:"device_name"`
	HistoryWindow         string   `yaml:"history_window"`
}

type ClaudeConfig struct {
	APIKey string `yaml:"api_key"`
}

func DefaultConfig() *Config {
	return &Config{
		BotName:  "adminbot",
		LogLevel: "info",
		DB: DBConfig{
			Host:         "127.0.0.1",
			Port:         5432,
			User:         "wantastic",
			Database:     "wantastic_dev",
			SSLMode:      "disable",
			PoolSize:     10,
			MinIdleConns: 2,
			MaxRetries:   3,
		},
		GRPC: GRPCConfig{
			Address: "127.0.0.1:50051",
			Timeout: "15s",
		},
		WhatsApp: WhatsAppConfig{
			StorePath:             "/var/lib/wantastic/adminbot/whatsapp.db",
			ReplySignatureEnabled: defaultReplySignatureOn,
			LoginTimeout:          "3m",
			DeviceName:            "Wantastic Admin Bot",
			HistoryWindow:         "30d",
		},
		Claude: ClaudeConfig{},
	}
}

func LoadConfig(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read adminbot config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse adminbot config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// FromUnified builds an adminbot.Config from the merged WantasticCore config.
// Used by the single all-in-one binary so the bot doesn't need its own YAML.
func FromUnified(c *corecfg.Config) *Config {
	out := DefaultConfig()
	if name := strings.TrimSpace(c.AdminBot.BotName); name != "" {
		out.BotName = name
	}
	if lvl := strings.TrimSpace(c.AdminBot.LogLevel); lvl != "" {
		out.LogLevel = lvl
	}
	out.DB = DBConfig{
		Host:                  c.Database.Host,
		Port:                  c.Database.Port,
		User:                  c.Database.User,
		Password:              c.Database.Password,
		Database:              c.Database.Database,
		SSLMode:               c.Database.SSLMode,
		PoolSize:              c.Database.PoolSize,
		MinIdleConns:          c.Database.MinIdleConns,
		MaxRetries:            c.Database.MaxRetries,
		RetryStatementTimeout: c.Database.RetryStatementTimeout,
	}
	// GRPC.Address is vestigial — the in-process bot doesn't dial gRPC, but
	// the field is still required by Validate(). Stuff a placeholder.
	out.GRPC = GRPCConfig{Address: "in-process", Timeout: "15s"}
	if sp := strings.TrimSpace(c.AdminBot.WhatsApp.StorePath); sp != "" {
		out.WhatsApp.StorePath = sp
	}
	if lt := strings.TrimSpace(c.AdminBot.WhatsApp.LoginTimeout); lt != "" {
		out.WhatsApp.LoginTimeout = lt
	}
	if dn := strings.TrimSpace(c.AdminBot.WhatsApp.DeviceName); dn != "" {
		out.WhatsApp.DeviceName = dn
	}
	if hw := strings.TrimSpace(c.AdminBot.WhatsApp.HistoryWindow); hw != "" {
		out.WhatsApp.HistoryWindow = hw
	}
	out.WhatsApp.AllowedGroups = append([]string(nil), c.AdminBot.WhatsApp.AllowedGroups...)
	out.WhatsApp.ReplySignatureEnabled = c.AdminBot.WhatsApp.ReplySignatureEnabled
	out.Claude.APIKey = c.AdminBot.Claude.APIKey
	return out
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.BotName) == "" {
		c.BotName = "adminbot"
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = "info"
	}
	if c.DB.Port == 0 {
		c.DB.Port = 5432
	}
	if c.DB.PoolSize == 0 {
		c.DB.PoolSize = 10
	}
	if c.DB.MinIdleConns == 0 {
		c.DB.MinIdleConns = 2
	}
	if c.DB.MaxRetries == 0 {
		c.DB.MaxRetries = 3
	}
	if strings.TrimSpace(c.DB.SSLMode) == "" {
		c.DB.SSLMode = "disable"
	}
	if strings.TrimSpace(c.DB.Host) == "" {
		return fmt.Errorf("database.host is required")
	}
	if strings.TrimSpace(c.DB.User) == "" {
		return fmt.Errorf("database.user is required")
	}
	if strings.TrimSpace(c.DB.Database) == "" {
		return fmt.Errorf("database.database is required")
	}

	if strings.TrimSpace(c.GRPC.Address) == "" {
		return fmt.Errorf("grpc.address is required")
	}
	if strings.TrimSpace(c.GRPC.Timeout) == "" {
		c.GRPC.Timeout = "15s"
	}
	if _, err := time.ParseDuration(c.GRPC.Timeout); err != nil {
		return fmt.Errorf("invalid grpc.timeout: %w", err)
	}

	if strings.TrimSpace(c.WhatsApp.StorePath) == "" {
		c.WhatsApp.StorePath = "/var/lib/wantastic/adminbot/whatsapp.db"
	}
	if strings.TrimSpace(c.WhatsApp.LoginTimeout) == "" {
		c.WhatsApp.LoginTimeout = "3m"
	}
	if _, err := time.ParseDuration(c.WhatsApp.LoginTimeout); err != nil {
		return fmt.Errorf("invalid whatsapp.login_timeout: %w", err)
	}
	if strings.TrimSpace(c.WhatsApp.HistoryWindow) == "" {
		c.WhatsApp.HistoryWindow = "30d"
	}
	if _, err := parseFlexibleDuration(c.WhatsApp.HistoryWindow); err != nil {
		return fmt.Errorf("invalid whatsapp.history_window: %w", err)
	}
	if strings.TrimSpace(c.WhatsApp.DeviceName) == "" {
		c.WhatsApp.DeviceName = "Wantastic Admin Bot"
	}

	return nil
}

func (c *Config) DatabaseConfig() corecfg.DatabaseConfig {
	return corecfg.DatabaseConfig{
		Host:                  c.DB.Host,
		Port:                  c.DB.Port,
		User:                  c.DB.User,
		Password:              c.DB.Password,
		Database:              c.DB.Database,
		SSLMode:               c.DB.SSLMode,
		PoolSize:              c.DB.PoolSize,
		MinIdleConns:          c.DB.MinIdleConns,
		MaxRetries:            c.DB.MaxRetries,
		RetryStatementTimeout: c.DB.RetryStatementTimeout,
	}
}

func (c *Config) DatabaseSQLDSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DB.User, c.DB.Password),
		Host:   fmt.Sprintf("%s:%d", c.DB.Host, c.DB.Port),
		Path:   c.DB.Database,
	}

	query := url.Values{}
	sslMode := strings.TrimSpace(c.DB.SSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	query.Set("sslmode", sslMode)
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *Config) WhatsAppStoreDSN() string {
	storePath := strings.TrimSpace(c.WhatsApp.StorePath)
	if storePath == "" {
		storePath = "/var/lib/wantastic/adminbot/whatsapp.db"
	}
	cleaned := filepath.Clean(storePath)
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)", filepath.ToSlash(cleaned))
}

func (c *Config) GRPCTimeout() time.Duration {
	d, err := time.ParseDuration(c.GRPC.Timeout)
	if err != nil {
		return 15 * time.Second
	}
	return d
}

func (c *Config) LoginTimeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.WhatsApp.LoginTimeout)
	if err != nil {
		return 3 * time.Minute
	}
	return d
}

func (c *Config) HistoryWindowDuration() time.Duration {
	d, err := parseFlexibleDuration(c.WhatsApp.HistoryWindow)
	if err != nil {
		return 30 * 24 * time.Hour
	}
	return d
}

func parseFlexibleDuration(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	switch {
	case strings.HasSuffix(trimmed, "d"):
		days, err := strconv.Atoi(strings.TrimSuffix(trimmed, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	case strings.HasSuffix(trimmed, "w"):
		weeks, err := strconv.Atoi(strings.TrimSuffix(trimmed, "w"))
		if err != nil {
			return 0, err
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	default:
		return time.ParseDuration(trimmed)
	}
}
