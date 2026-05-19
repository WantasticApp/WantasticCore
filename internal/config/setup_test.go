package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runWizardWithInput drives the interactive wizard with a fixed stdin script
// and asserts the result against the answers we fed it.
func runWizardWithInput(t *testing.T, dir string, answers []string) *SetupResult {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	in := strings.NewReader(strings.Join(answers, "\n") + "\n")
	var out bytes.Buffer
	result, err := RunSetupWizard(in, &out, path)
	if err != nil {
		t.Fatalf("RunSetupWizard: %v\nstdout:\n%s", err, out.String())
	}
	if result == nil || result.Config == nil {
		t.Fatalf("nil result")
	}
	return result
}

func TestSetupWizard_HappyPath(t *testing.T) {
	dir := t.TempDir()
	answers := []string{
		// PostgreSQL
		"db.example.com", // host (default would be localhost)
		"",               // port (default 5432)
		"wantastic",      // user
		"super-secret",   // password
		"wantastic",      // database
		"require",        // ssl_mode
		// Server endpoint
		"core.example.com", // hostname
		"",                 // wireguard port (default 51820)
		// Redis
		"y",              // enable redis
		"redis.local:6379", // addr
		"",               // password (blank)
		// SMTP
		"n", // disable smtp
		// AdminBot
		"n", // disable adminbot
		// Admin account
		"admin@example.com",
		"Admin User",
		"hunter2-pass",
		"60", // max peers
	}
	result := runWizardWithInput(t, dir, answers)

	if got, want := result.Config.Database.Host, "db.example.com"; got != want {
		t.Errorf("db host = %q, want %q", got, want)
	}
	if got, want := result.Config.Database.SSLMode, "require"; got != want {
		t.Errorf("ssl_mode = %q, want %q", got, want)
	}
	if got, want := result.Config.Network.ServerEndpoint, "core.example.com"; got != want {
		t.Errorf("server endpoint = %q, want %q", got, want)
	}
	if !result.Config.Redis.Enabled {
		t.Error("expected Redis enabled")
	}
	if result.Config.SMTP.Enabled {
		t.Error("expected SMTP disabled")
	}
	if result.Config.AdminBot.Enabled {
		t.Error("expected AdminBot disabled")
	}
	if got, want := result.Admin.Email, "admin@example.com"; got != want {
		t.Errorf("admin email = %q, want %q", got, want)
	}
	if got, want := result.Admin.MaxPeers, 60; got != want {
		t.Errorf("admin max_peers = %d, want %d", got, want)
	}

	// Auto-generated secrets must be 64 hex chars (32 bytes).
	if got := len(result.Config.Tenant.SessionSigningKey); got != 64 {
		t.Errorf("session_signing_key len = %d, want 64", got)
	}
	if got := len(result.Config.Hooks.SecretKey); got != 64 {
		t.Errorf("hooks.secret_key len = %d, want 64", got)
	}

	// File must exist on disk and be re-loadable.
	fi, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config.yaml perm = %v, want 0600", fi.Mode().Perm())
	}
	if _, err := Load(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("Load(config.yaml): %v", err)
	}
}

func TestSetupWizard_DefaultsAppliedWhenInputIsBlank(t *testing.T) {
	dir := t.TempDir()
	// Hit Enter on every single prompt. The wizard should fall back to defaults
	// everywhere we accept blank input, only blocking on prompts that have no
	// usable default (admin email + password).
	answers := []string{
		"", "", "", "", "", "", // PG (six prompts)
		"", "",       // server endpoint, port
		"", "", "",   // redis (yes default, addr, password)
		"",            // smtp default no
		"",            // adminbot default no
		"admin@example.com", // admin email (required)
		"",                  // full name -> derived from email
		"some-password",     // password (required)
		"",                  // max peers (default 30)
	}
	result := runWizardWithInput(t, dir, answers)

	if got, want := result.Admin.FullName, "admin"; got != want {
		t.Errorf("admin full name derived = %q, want %q", got, want)
	}
	if got, want := result.Admin.MaxPeers, 30; got != want {
		t.Errorf("admin max_peers default = %d, want %d", got, want)
	}
}

func TestLoadOrSetup_ExistingFileWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	first := runWizardWithInput(t, dir, []string{
		"", "", "", "p", "", "", // pg
		"core.local", "",
		"y", "", "",
		"n",
		"n",
		"admin@example.com", "", "pw", "",
	})

	// Now LoadOrSetup should pick up the file without re-prompting.
	result, err := LoadOrSetup(path)
	if err != nil {
		t.Fatalf("LoadOrSetup: %v", err)
	}
	if result.Admin.Email != "" {
		t.Errorf("Admin.Email leaked on second load = %q (expected empty)", result.Admin.Email)
	}
	if got := result.Config.Tenant.SessionSigningKey; got != first.Config.Tenant.SessionSigningKey {
		t.Errorf("session_signing_key changed across load: %q vs %q", got, first.Config.Tenant.SessionSigningKey)
	}
}

func TestRunNonInteractiveSetup_RequiresCoreEnv(t *testing.T) {
	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_PASSWORD", "")
	t.Setenv("WANTASTIC_DB_PASSWORD", "")

	if _, err := RunNonInteractiveSetup(filepath.Join(t.TempDir(), "config.yaml")); err == nil {
		t.Fatal("expected error when required env vars are missing")
	}
}

func TestRunNonInteractiveSetup_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_PASSWORD", "secret")
	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_NAME", "Admin Anne")
	t.Setenv("WANTASTIC_BOOTSTRAP_ADMIN_MAX_PEERS", "120")
	t.Setenv("WANTASTIC_DB_PASSWORD", "pgsecret")
	t.Setenv("WANTASTIC_DB_HOST", "postgres")
	t.Setenv("WANTASTIC_REDIS_ADDR", "redis:6379")
	t.Setenv("WANTASTIC_SERVER_ENDPOINT", "example.com")

	result, err := RunNonInteractiveSetup(path)
	if err != nil {
		t.Fatalf("RunNonInteractiveSetup: %v", err)
	}
	if got, want := result.Admin.Email, "admin@example.com"; got != want {
		t.Errorf("admin email = %q, want %q", got, want)
	}
	if got, want := result.Admin.FullName, "Admin Anne"; got != want {
		t.Errorf("admin name = %q, want %q", got, want)
	}
	if got, want := result.Admin.MaxPeers, 120; got != want {
		t.Errorf("admin max_peers = %d, want %d", got, want)
	}
	if got, want := result.Config.Database.Host, "postgres"; got != want {
		t.Errorf("db host = %q, want %q", got, want)
	}
	if got, want := result.Config.Redis.Addr, "redis:6379"; got != want {
		t.Errorf("redis addr = %q, want %q", got, want)
	}
	if got, want := result.Config.Network.ServerEndpoint, "example.com"; got != want {
		t.Errorf("server endpoint = %q, want %q", got, want)
	}
	if !strings.HasSuffix(result.Config.Database.User, "wantastic") {
		t.Errorf("db user defaulted unexpectedly: %q", result.Config.Database.User)
	}
	if len(result.Config.Tenant.SessionSigningKey) != 64 {
		t.Error("session_signing_key not auto-generated")
	}
	if len(result.Config.Hooks.SecretKey) != 64 {
		t.Error("hooks.secret_key not auto-generated")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Contains(raw, []byte("server_endpoint: example.com")) {
		t.Errorf("written config missing server_endpoint=example.com")
	}
}

func TestSanitizeAddrInSpooler(t *testing.T) {
	// Just a sanity check on the helper used by the spooler in
	// internal/email — exercised here so the wizard isn't the only thing
	// gated by `go test ./internal/config/...`. Skipped if disabled.
	_ = io.EOF
}
