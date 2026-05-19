// Package setupweb serves a single-page HTTPS setup wizard on first boot
// when the operator hasn't provided a config.yaml and there's no terminal
// for the CLI wizard. Designed for the "curl install.sh | sh" and
// "docker run" install paths.
//
// Flow:
//   1. Operator boots wantastic-core with no config.yaml and either
//      WANTASTIC_WEB_SETUP=1 or no TTY on stdin.
//   2. main.go detects this and calls setupweb.Run(addr, configPath).
//   3. setupweb generates a self-signed cert (if none exists), binds the
//      address, and serves a form at GET / (with form fields prefilled
//      from any WANTASTIC_* env vars).
//   4. Operator submits POST /submit with the form fields.
//   5. setupweb validates, builds a config.Config + SetupAdmin, writes
//      the config to configPath, renders a "what to do next" page that
//      lists the DNS records the operator needs to set, and signals
//      Run() to return.
//   6. main.go exits cleanly. Docker/systemd restart the process; the
//      next boot finds config.yaml and runs the real binary.
package setupweb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"math/big"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/config"

	"github.com/rs/zerolog/log"
)

// Result is what Run returns once the operator submits the form.
type Result struct {
	Config *config.Config
	Admin  config.SetupAdmin

	// ConsoleHost is the user-chosen FQDN where the portal will live
	// (e.g. "console.example.com" or just "example.com"). Used by
	// downstream code to advertise the right URL.
	ConsoleHost string
	// WinboxHost / WireguardHost are the DNS names for the Winbox and
	// WireGuard listeners — derived as winbox.<domain> / wg.<domain> by
	// default but overridable in the form.
	WinboxHost    string
	WireguardHost string
}

// Run binds an HTTPS listener at addr, serves the wizard, blocks until the
// form is submitted and the config has been written, then returns the
// result. The caller is expected to exit/restart the process after Run
// returns so the next boot loads the new config.yaml.
//
// If addr is empty it defaults to ":8443". The cert/key file pair is
// stored next to the configPath (alongside config.yaml). If it already
// exists it's reused — handy when the operator restarts the wizard.
func Run(addr, configPath string) (*Result, error) {
	if addr == "" {
		addr = ":8443"
	}

	certDir := filepath.Dir(configPath)
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", certDir, err)
	}
	certPath := filepath.Join(certDir, "setup-cert.pem")
	keyPath := filepath.Join(certDir, "setup-key.pem")
	tlsCert, err := loadOrGenerateCert(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("setup tls cert: %w", err)
	}

	h := &handler{
		configPath: configPath,
		results:    make(chan *Result, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.formGET)
	mux.HandleFunc("/submit", h.submitPOST)
	mux.HandleFunc("/done", h.donePage)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	log.Info().Str("addr", addr).Msg("setupweb: serving first-run setup wizard")

	listenErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()

	select {
	case res := <-h.results:
		// Give the operator a moment to read the "done" page before we
		// tear the listener down (the response is in-flight at this point).
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return res, nil
	case err := <-listenErr:
		return nil, err
	}
}

// ─────────────────────────────────────────────────────────────────────
// HTTP handlers
// ─────────────────────────────────────────────────────────────────────

type handler struct {
	configPath string

	mu      sync.Mutex
	last    *Result
	results chan *Result
}

type formData struct {
	Error string

	Domain        string
	ConsoleHost   string
	WinboxHost    string
	WireguardHost string
	WireguardPort int

	// LetsEncryptEnabled gates the ACME issuance step. Defaults to OFF so
	// local-dev installs don't accidentally hit Let's Encrypt rate limits
	// with a non-public domain. Operators flip it on for production setups
	// where they have a real DNS-resolvable domain pointing at the box.
	LetsEncryptEnabled bool
	// LetsEncryptEmail is the contact email for ACME registration. Required
	// only when LetsEncryptEnabled is true; ignored otherwise.
	LetsEncryptEmail string
	// FirewallEnabled toggles the in-container iptables ruleset. Defaults
	// to true when the container has NET_ADMIN.
	FirewallEnabled bool

	AdminEmail    string
	AdminName     string
	AdminPassword string
	AdminMaxPeers int

	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string

	RedisAddr     string
	RedisPassword string

	SMTPEnabled  bool
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	ClaudeEnabled bool
	ClaudeAPIKey  string
}

func defaultsFromEnv() formData {
	atoi := func(k string, def int) int {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return def
		}
		return n
	}
	envOr := func(k, def string) string {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			return def
		}
		return v
	}
	return formData{
		Domain:           envOr("WANTASTIC_DOMAIN", ""),
		ConsoleHost:      envOr("WANTASTIC_CONSOLE_HOST", ""),
		WireguardPort:    atoi("WANTASTIC_WIREGUARD_PORT", 51820),
		LetsEncryptEnabled: envOr("WANTASTIC_LE_ENABLED", "0") != "0",
		LetsEncryptEmail:   envOr("WANTASTIC_LE_EMAIL", ""),
		FirewallEnabled:    envOr("WANTASTIC_FIREWALL", "1") != "0",
		AdminEmail:       envOr("WANTASTIC_BOOTSTRAP_ADMIN_EMAIL", ""),
		AdminName:        envOr("WANTASTIC_BOOTSTRAP_ADMIN_NAME", ""),
		AdminMaxPeers:    atoi("WANTASTIC_BOOTSTRAP_ADMIN_MAX_PEERS", 30),
		// All-in-one image runs PG + Redis on localhost; the wizard never
		// asks the operator about them. Env vars still override for
		// external-DB deployments.
		DBHost:     envOr("WANTASTIC_DB_HOST", "127.0.0.1"),
		DBPort:     atoi("WANTASTIC_DB_PORT", 5432),
		DBUser:     envOr("WANTASTIC_DB_USER", "wantastic"),
		DBPassword: envOr("WANTASTIC_DB_PASSWORD", ""),
		DBName:     envOr("WANTASTIC_DB_NAME", "wantastic"),
		RedisAddr:  envOr("WANTASTIC_REDIS_ADDR", "127.0.0.1:6379"),
		SMTPPort:   atoi("WANTASTIC_SMTP_PORT", 587),
	}
}

func (h *handler) formGET(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := defaultsFromEnv()
	renderForm(w, data)
}

func (h *handler) submitPOST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}

	data := defaultsFromEnv()
	data.Domain = strings.TrimSpace(r.FormValue("domain"))
	data.ConsoleHost = strings.TrimSpace(r.FormValue("console_host"))
	data.WinboxHost = strings.TrimSpace(r.FormValue("winbox_host"))
	data.WireguardHost = strings.TrimSpace(r.FormValue("wireguard_host"))
	if p := r.FormValue("wireguard_port"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			data.WireguardPort = n
		}
	}
	data.AdminEmail = strings.TrimSpace(r.FormValue("admin_email"))
	data.AdminName = strings.TrimSpace(r.FormValue("admin_name"))
	data.AdminPassword = r.FormValue("admin_password")
	if p := r.FormValue("admin_max_peers"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			data.AdminMaxPeers = n
		}
	}
	data.DBHost = strings.TrimSpace(r.FormValue("db_host"))
	if p := r.FormValue("db_port"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			data.DBPort = n
		}
	}
	data.DBUser = strings.TrimSpace(r.FormValue("db_user"))
	data.DBPassword = r.FormValue("db_password")
	data.DBName = strings.TrimSpace(r.FormValue("db_name"))
	data.RedisAddr = strings.TrimSpace(r.FormValue("redis_addr"))
	data.RedisPassword = r.FormValue("redis_password")
	data.SMTPEnabled = r.FormValue("smtp_enabled") == "on"
	data.SMTPHost = strings.TrimSpace(r.FormValue("smtp_host"))
	if p := r.FormValue("smtp_port"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			data.SMTPPort = n
		}
	}
	data.SMTPUser = strings.TrimSpace(r.FormValue("smtp_user"))
	data.SMTPPassword = r.FormValue("smtp_password")
	data.SMTPFrom = strings.TrimSpace(r.FormValue("smtp_from"))
	data.ClaudeEnabled = r.FormValue("claude_enabled") == "on"
	data.ClaudeAPIKey = strings.TrimSpace(r.FormValue("claude_api_key"))
	data.LetsEncryptEnabled = r.FormValue("le_enabled") == "on"
	data.LetsEncryptEmail = strings.TrimSpace(r.FormValue("le_email"))
	data.FirewallEnabled = r.FormValue("firewall_enabled") == "on"

	if err := data.validate(); err != nil {
		data.Error = err.Error()
		renderForm(w, data)
		return
	}

	res, cfg := data.toConfig()

	if err := writeConfig(h.configPath, cfg); err != nil {
		data.Error = "write config: " + err.Error()
		renderForm(w, data)
		return
	}

	// Provision the rest of the stack (nginx site config, Let's Encrypt
	// cert, firewall) — best-effort: if the helper scripts aren't on PATH
	// (e.g. running outside the all-in-one image) we just log and proceed.
	provisionAllInOne(res, data)

	h.mu.Lock()
	h.last = res
	h.mu.Unlock()

	// Signal Run() immediately so the parent process can exit and the
	// supervisor restarts us into normal mode — even if the operator
	// navigates away before the /done page finishes loading. The buffered
	// channel + non-blocking send ensures double-submits don't deadlock.
	select {
	case h.results <- res:
	default:
	}

	http.Redirect(w, r, "/done", http.StatusSeeOther)
}

// provisionAllInOne renders the nginx site config, issues the Let's Encrypt
// cert, applies the firewall, and reloads nginx — using shell helpers
// installed by the all-in-one Docker image. Failures are logged but never
// fatal: the wizard still finishes so the operator gets a config.yaml.
func provisionAllInOne(res *Result, data formData) {
	domain := strings.TrimSpace(res.ConsoleHost)
	if domain == "" {
		domain = strings.TrimSpace(data.Domain)
	}
	if domain == "" {
		log.Warn().Msg("setupweb: no domain set, skipping nginx/LE provisioning")
		return
	}

	// Decide TLS mode for the rendered nginx site.
	//   letsencrypt → request + use a real cert (production).
	//   self-signed → keep the bootstrap cert; domain still propagated to
	//                 server_name so nginx serves the right vhost.
	tlsMode := "self-signed"
	leEmail := strings.TrimSpace(data.LetsEncryptEmail)
	if data.LetsEncryptEnabled && leEmail != "" {
		tlsMode = "letsencrypt"
	}

	// Issue the LE cert FIRST so the rendered production config has real
	// cert files to point at. On failure we fall back to self-signed.
	if tlsMode == "letsencrypt" {
		if _, err := os.Stat("/usr/local/bin/letsencrypt-issue.sh"); err == nil {
			cmd := exec.Command("/usr/local/bin/letsencrypt-issue.sh", domain, leEmail, res.ConsoleHost)
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Warn().Err(err).Bytes("output", out).Msg("setupweb: letsencrypt-issue.sh failed — falling back to self-signed (rerun the wizard once DNS is live to retry)")
				tlsMode = "self-signed"
			} else {
				log.Info().Bytes("output", out).Msg("setupweb: letsencrypt-issue.sh ok")
			}
		}
	}

	if _, err := os.Stat("/usr/local/bin/nginx-render.sh"); err == nil {
		cmd := exec.Command("/usr/local/bin/nginx-render.sh", domain, res.ConsoleHost, tlsMode)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Warn().Err(err).Bytes("output", out).Msg("setupweb: nginx-render.sh failed")
		} else {
			log.Info().Bytes("output", out).Str("tls_mode", tlsMode).Msg("setupweb: nginx-render.sh ok")
		}
	}

	// Reload nginx to pick up the new site config + cert.
	if _, err := exec.LookPath("nginx"); err == nil {
		_ = exec.Command("nginx", "-s", "reload").Run()
	}

	if data.FirewallEnabled {
		if _, err := os.Stat("/usr/local/bin/firewall-apply.sh"); err == nil {
			cmd := exec.Command("/usr/local/bin/firewall-apply.sh")
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Warn().Err(err).Bytes("output", out).Msg("setupweb: firewall-apply.sh failed")
			} else {
				log.Info().Bytes("output", out).Msg("setupweb: firewall-apply.sh ok")
			}
		}
	}
}

func (h *handler) donePage(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	res := h.last
	h.mu.Unlock()
	if res == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	renderDone(w, res, publicIP(r))

	// Signal Run() that the setup is complete. Buffered channel size 1
	// guarantees this doesn't block even if the operator double-submits.
	select {
	case h.results <- res:
	default:
	}
}

// ─────────────────────────────────────────────────────────────────────
// Validation + config construction
// ─────────────────────────────────────────────────────────────────────

func (d *formData) validate() error {
	if d.Domain == "" {
		return errors.New("domain is required (e.g. example.com)")
	}
	if strings.Contains(d.Domain, "/") || strings.Contains(d.Domain, " ") {
		return errors.New("domain must be a bare hostname (no scheme, no slashes)")
	}
	if d.ConsoleHost == "" {
		return errors.New("console host is required")
	}
	if d.WinboxHost == "" {
		d.WinboxHost = "winbox." + d.Domain
	}
	if d.WireguardHost == "" {
		d.WireguardHost = "wg." + d.Domain
	}
	if d.WireguardPort <= 0 {
		d.WireguardPort = 51820
	}
	if d.AdminEmail == "" || !strings.Contains(d.AdminEmail, "@") {
		return errors.New("super-admin email is required and must look like an email")
	}
	if d.AdminPassword == "" || len(d.AdminPassword) < 8 {
		return errors.New("super-admin password must be at least 8 characters")
	}
	if d.AdminMaxPeers <= 0 {
		d.AdminMaxPeers = 30
	}
	if d.AdminName == "" {
		d.AdminName = strings.SplitN(d.AdminEmail, "@", 2)[0]
	}
	if d.DBHost == "" || d.DBUser == "" || d.DBName == "" {
		return errors.New("database host, user, and name are required")
	}
	if d.DBPort <= 0 {
		d.DBPort = 5432
	}
	if d.SMTPEnabled {
		if d.SMTPHost == "" || d.SMTPFrom == "" {
			return errors.New("when SMTP is enabled, host and from-address are required")
		}
	}
	if d.ClaudeEnabled && d.ClaudeAPIKey == "" {
		return errors.New("when Copilot/AdminBot is enabled, the Anthropic API key is required")
	}
	return nil
}

func (d *formData) toConfig() (*Result, *config.Config) {
	cfg := config.Default()

	cfg.Network.ServerEndpoint = d.WireguardHost
	cfg.Network.SharedPort = d.WireguardPort

	cfg.Database.Host = d.DBHost
	cfg.Database.Port = d.DBPort
	cfg.Database.User = d.DBUser
	cfg.Database.Password = d.DBPassword
	cfg.Database.Database = d.DBName

	cfg.Redis.Enabled = d.RedisAddr != ""
	cfg.Redis.Addr = d.RedisAddr
	cfg.Redis.Password = d.RedisPassword

	cfg.Endpoints.WinboxServer = d.WinboxHost
	cfg.Endpoints.WireguardServer = d.WireguardHost

	cfg.SMTP.Enabled = d.SMTPEnabled
	if d.SMTPEnabled {
		cfg.SMTP.Host = d.SMTPHost
		cfg.SMTP.Port = d.SMTPPort
		cfg.SMTP.UseTLS = true
		cfg.SMTP.User = d.SMTPUser
		cfg.SMTP.Password = d.SMTPPassword
		cfg.SMTP.From = d.SMTPFrom
		cfg.SMTP.FromName = "Wantastic"
	}

	cfg.AdminBot.Enabled = d.ClaudeEnabled
	cfg.AdminBot.Claude.APIKey = d.ClaudeAPIKey

	cfg.Tenant.SessionSigningKey = randomHex(32)
	cfg.Hooks.SecretKey = randomHex(32)
	cfg.Hooks.BaseURL = "https://" + d.ConsoleHost + "/hooks"
	cfg.Auth.MTLS.AutoGenerate = true
	// MTLSManager.EnsureCertificates does os.MkdirAll(CertDir, …); leaving
	// it empty crashes on first boot. Persist a real default so the in-
	// container generator has somewhere to put its self-signed keypair.
	if cfg.Auth.MTLS.CertDir == "" {
		cfg.Auth.MTLS.CertDir = "/etc/wantastic/certs"
	}

	res := &Result{
		Config: cfg,
		Admin: config.SetupAdmin{
			Email:    d.AdminEmail,
			FullName: d.AdminName,
			Password: d.AdminPassword,
			MaxPeers: d.AdminMaxPeers,
		},
		ConsoleHost:   d.ConsoleHost,
		WinboxHost:    d.WinboxHost,
		WireguardHost: d.WireguardHost,
	}
	return res, cfg
}

// ─────────────────────────────────────────────────────────────────────
// Cert generation
// ─────────────────────────────────────────────────────────────────────

func loadOrGenerateCert(certPath, keyPath string) (tls.Certificate, error) {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return tls.LoadX509KeyPair(certPath, keyPath)
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "wantastic-core setup",
			Organization: []string{"Wantastic"},
		},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:              []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// ─────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────

func writeConfig(path string, cfg *config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return config.WriteYAML(path, cfg)
}

func publicIP(r *http.Request) string {
	// Best-effort: try the Host header, fall back to the request remote.
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return h
	}
	if r.Host != "" {
		return strings.SplitN(r.Host, ":", 2)[0]
	}
	return ""
}

var hexChars = []byte("0123456789abcdef")

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hexChars[v>>4]
		out[i*2+1] = hexChars[v&0x0f]
	}
	return string(out)
}

// Unused but kept to avoid an "imported and not used" lint when tweaking.
var _ = mathrand.Int31
var _ template.HTML
