package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SetupAdmin is the first admin account collected by the setup wizard.
// The caller (main) bootstraps a tenant from it after the core starts.
type SetupAdmin struct {
	Email    string
	FullName string
	Password string
	MaxPeers int
}

// SetupResult bundles a freshly-built Config with the bootstrap admin info.
type SetupResult struct {
	Config *Config
	Admin  SetupAdmin
}

// RunSetupWizard runs an interactive first-run wizard on the given reader/writer,
// writes the resulting YAML to configPath, and returns the loaded Config plus
// the bootstrap admin credentials. Random secrets are generated for keys the
// user should not type by hand (session signing, hook tokens).
func RunSetupWizard(in io.Reader, out io.Writer, configPath string) (*SetupResult, error) {
	r := bufio.NewReader(in)
	cfg := Default()

	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "  WantasticCore — first-run setup")
	fmt.Fprintln(out, "─────────────────────────────────────────────────────────────")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "I'll create", configPath, "and a super-admin account.")
	fmt.Fprintln(out, "Press Enter to accept the [bracketed] default.")
	fmt.Fprintln(out)

	section(out, "PostgreSQL")
	cfg.Database.Host = askStr(r, out, "host", cfg.Database.Host)
	cfg.Database.Port = askInt(r, out, "port", cfg.Database.Port)
	cfg.Database.User = askStr(r, out, "user", cfg.Database.User)
	cfg.Database.Password = askSecret(r, out, "password")
	cfg.Database.Database = askStr(r, out, "database", cfg.Database.Database)
	cfg.Database.SSLMode = askStr(r, out, "ssl_mode", cfg.Database.SSLMode)

	section(out, "Server endpoint (peer-facing)")
	cfg.Network.ServerEndpoint = askStr(r, out, "public hostname or IP (no port)", "localhost")
	cfg.Network.SharedPort = askInt(r, out, "WireGuard UDP port", cfg.Network.SharedPort)

	section(out, "Redis (optional but recommended)")
	cfg.Redis.Enabled = askYesNo(r, out, "enable Redis?", true)
	if cfg.Redis.Enabled {
		cfg.Redis.Addr = askStr(r, out, "addr (host:port)", cfg.Redis.Addr)
		cfg.Redis.Password = askSecret(r, out, "password (blank if none)")
	}

	section(out, "SMTP (optional — for admin notifications)")
	cfg.SMTP.Enabled = askYesNo(r, out, "enable SMTP?", false)
	if cfg.SMTP.Enabled {
		cfg.SMTP.Host = askStr(r, out, "host", "smtp.example.com")
		cfg.SMTP.Port = askInt(r, out, "port", 587)
		cfg.SMTP.UseTLS = askYesNo(r, out, "use TLS?", true)
		cfg.SMTP.User = askStr(r, out, "username", "")
		cfg.SMTP.Password = askSecret(r, out, "password")
		cfg.SMTP.From = askStr(r, out, "from address", "noreply@example.com")
		cfg.SMTP.FromName = askStr(r, out, "from name", "Wantastic")
	}

	section(out, "AdminBot (WhatsApp tooling — optional)")
	cfg.AdminBot.Enabled = askYesNo(r, out, "enable adminbot?", false)
	if cfg.AdminBot.Enabled {
		cfg.AdminBot.WhatsApp.StorePath = askStr(r, out, "WhatsApp store path", cfg.AdminBot.WhatsApp.StorePath)
		cfg.AdminBot.Claude.APIKey = askSecret(r, out, "Anthropic API key")
	}

	section(out, "Super-admin account (you'll log into the portal with this)")
	admin := SetupAdmin{}
	admin.Email = askStr(r, out, "email", "")
	admin.FullName = askStr(r, out, "full name", strings.SplitN(admin.Email, "@", 2)[0])
	admin.Password = askSecret(r, out, "password")
	admin.MaxPeers = askInt(r, out, "max peers for the admin account", 30)

	// Auto-generated secrets.
	cfg.Tenant.SessionSigningKey = mustRandomHex(32)
	cfg.Hooks.SecretKey = mustRandomHex(32)
	cfg.Auth.MTLS.AutoGenerate = true

	if err := writeYAML(configPath, cfg); err != nil {
		return nil, fmt.Errorf("write %s: %w", configPath, err)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Saved config to %s. Continuing startup...\n\n", configPath)

	return &SetupResult{Config: cfg, Admin: admin}, nil
}

// LoadOrSetup loads the config from path; if it doesn't exist, either the
// interactive wizard runs against stdin/stdout, or — when stdin is non-TTY
// (containers, CI) — the env-driven path runs from WANTASTIC_SETUP_* variables.
// If the file already exists, SetupResult.Admin is empty and SetupResult.Config
// is the loaded config.
func LoadOrSetup(path string) (*SetupResult, error) {
	if _, err := os.Stat(path); err == nil {
		cfg, err := Load(path)
		if err != nil {
			return nil, err
		}
		return &SetupResult{Config: cfg}, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat config: %w", err)
	}

	if os.Getenv("WANTASTIC_SETUP_NONINTERACTIVE") == "1" || !isInteractive(os.Stdin) {
		return RunNonInteractiveSetup(path)
	}
	return RunSetupWizard(os.Stdin, os.Stderr, path)
}

// RunNonInteractiveSetup builds the config and bootstrap admin entirely from
// environment variables. Designed for Docker / CI / orchestrated deployments
// where there's no terminal for the wizard.
//
// Required env vars (no defaults — fail closed):
//   WANTASTIC_BOOTSTRAP_ADMIN_EMAIL      Super-admin email
//   WANTASTIC_BOOTSTRAP_ADMIN_PASSWORD   Super-admin password
//   WANTASTIC_DB_PASSWORD                PostgreSQL password
//
// Optional env vars (sensible defaults applied if absent):
//   WANTASTIC_BOOTSTRAP_ADMIN_NAME       (default: derived from email local-part)
//   WANTASTIC_BOOTSTRAP_ADMIN_MAX_PEERS  (default: 30)
//   WANTASTIC_DB_HOST                    (default: localhost)
//   WANTASTIC_DB_PORT                    (default: 5432)
//   WANTASTIC_DB_USER                    (default: wantastic)
//   WANTASTIC_DB_NAME                    (default: wantastic)
//   WANTASTIC_REDIS_ADDR                 (default: localhost:6379)
//   WANTASTIC_SERVER_ENDPOINT            (default: localhost)
//   WANTASTIC_WIREGUARD_PORT             (default: 51820)
//   WANTASTIC_SMTP_HOST / _PORT / _USER / _PASSWORD / _FROM
//     If WANTASTIC_SMTP_HOST is set, SMTP is enabled. Otherwise the email
//     subsystem falls back to local sendmail / disk-spool (see internal/email).
//   WANTASTIC_ADMINBOT_ENABLED=1         Enable the WhatsApp adminbot
//   WANTASTIC_ADMINBOT_CLAUDE_API_KEY    Required if adminbot is enabled
//
// Auto-generated:
//   tenant.session_signing_key, hooks.secret_key — 32-byte random hex.
func RunNonInteractiveSetup(configPath string) (*SetupResult, error) {
	adminEmail := strings.TrimSpace(os.Getenv("WANTASTIC_BOOTSTRAP_ADMIN_EMAIL"))
	adminPass := os.Getenv("WANTASTIC_BOOTSTRAP_ADMIN_PASSWORD")
	dbPass := os.Getenv("WANTASTIC_DB_PASSWORD")
	if adminEmail == "" || adminPass == "" || dbPass == "" {
		return nil, fmt.Errorf("non-interactive setup requires WANTASTIC_BOOTSTRAP_ADMIN_EMAIL, WANTASTIC_BOOTSTRAP_ADMIN_PASSWORD, WANTASTIC_DB_PASSWORD")
	}

	cfg := Default()

	cfg.Database.Host = envOr("WANTASTIC_DB_HOST", cfg.Database.Host)
	cfg.Database.Port = envOrInt("WANTASTIC_DB_PORT", cfg.Database.Port)
	cfg.Database.User = envOr("WANTASTIC_DB_USER", cfg.Database.User)
	cfg.Database.Password = dbPass
	cfg.Database.Database = envOr("WANTASTIC_DB_NAME", cfg.Database.Database)

	cfg.Redis.Enabled = true
	cfg.Redis.Addr = envOr("WANTASTIC_REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.Password = os.Getenv("WANTASTIC_REDIS_PASSWORD")

	cfg.Network.ServerEndpoint = envOr("WANTASTIC_SERVER_ENDPOINT", "localhost")
	cfg.Network.SharedPort = envOrInt("WANTASTIC_WIREGUARD_PORT", cfg.Network.SharedPort)

	if host := strings.TrimSpace(os.Getenv("WANTASTIC_SMTP_HOST")); host != "" {
		cfg.SMTP.Enabled = true
		cfg.SMTP.Host = host
		cfg.SMTP.Port = envOrInt("WANTASTIC_SMTP_PORT", 587)
		cfg.SMTP.UseTLS = true
		cfg.SMTP.User = os.Getenv("WANTASTIC_SMTP_USER")
		cfg.SMTP.Password = os.Getenv("WANTASTIC_SMTP_PASSWORD")
		cfg.SMTP.From = envOr("WANTASTIC_SMTP_FROM", "noreply@"+host)
		cfg.SMTP.FromName = envOr("WANTASTIC_SMTP_FROM_NAME", "Wantastic")
	}

	if os.Getenv("WANTASTIC_ADMINBOT_ENABLED") == "1" {
		cfg.AdminBot.Enabled = true
		cfg.AdminBot.Claude.APIKey = os.Getenv("WANTASTIC_ADMINBOT_CLAUDE_API_KEY")
		if cfg.AdminBot.Claude.APIKey == "" {
			return nil, fmt.Errorf("WANTASTIC_ADMINBOT_ENABLED=1 requires WANTASTIC_ADMINBOT_CLAUDE_API_KEY")
		}
		cfg.AdminBot.WhatsApp.StorePath = envOr("WANTASTIC_ADMINBOT_STORE_PATH", cfg.AdminBot.WhatsApp.StorePath)
	}

	cfg.Tenant.SessionSigningKey = mustRandomHex(32)
	cfg.Hooks.SecretKey = mustRandomHex(32)
	cfg.Auth.MTLS.AutoGenerate = true

	if err := writeYAML(configPath, cfg); err != nil {
		return nil, fmt.Errorf("write %s: %w", configPath, err)
	}

	admin := SetupAdmin{
		Email:    adminEmail,
		FullName: envOr("WANTASTIC_BOOTSTRAP_ADMIN_NAME", strings.SplitN(adminEmail, "@", 2)[0]),
		Password: adminPass,
		MaxPeers: envOrInt("WANTASTIC_BOOTSTRAP_ADMIN_MAX_PEERS", 30),
	}
	return &SetupResult{Config: cfg, Admin: admin}, nil
}

func envOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envOrInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func writeYAML(path string, cfg *Config) error {
	return WriteYAML(path, cfg)
}

// WriteYAML marshals cfg to YAML and writes it to path with mode 0600.
// Exported so the web-based setup wizard (internal/setupweb) can persist
// the operator's choices without reaching into the package internals.
func WriteYAML(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func section(out io.Writer, name string) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "▸ %s\n", name)
}

func askStr(r *bufio.Reader, out io.Writer, label, def string) string {
	if def != "" {
		fmt.Fprintf(out, "  %s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "  %s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func askSecret(r *bufio.Reader, out io.Writer, label string) string {
	// Plain stdin read; terminal echo control isn't worth a dep for a one-shot wizard.
	fmt.Fprintf(out, "  %s: ", label)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func askInt(r *bufio.Reader, out io.Writer, label string, def int) int {
	for {
		raw := askStr(r, out, label, strconv.Itoa(def))
		n, err := strconv.Atoi(raw)
		if err == nil {
			return n
		}
		fmt.Fprintf(out, "  '%s' is not a number, try again.\n", raw)
	}
}

func askYesNo(r *bufio.Reader, out io.Writer, label string, def bool) bool {
	suffix := "y/N"
	if def {
		suffix = "Y/n"
	}
	for {
		fmt.Fprintf(out, "  %s [%s]: ", label, suffix)
		line, _ := r.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		switch line {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read: %v", err))
	}
	return hex.EncodeToString(b)
}

func isInteractive(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
