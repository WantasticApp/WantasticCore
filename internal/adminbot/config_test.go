package adminbot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigUsesSeparateWhatsAppStore(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.WhatsApp.StorePath != "/var/lib/wantastic/adminbot/whatsapp.db" {
		t.Fatalf("expected separate whatsapp store path, got %q", cfg.WhatsApp.StorePath)
	}
	if !cfg.WhatsApp.ReplySignatureEnabled {
		t.Fatal("expected reply signature to be enabled by default")
	}
	if !strings.Contains(cfg.WhatsAppStoreDSN(), "file:/var/lib/wantastic/adminbot/whatsapp.db") {
		t.Fatalf("expected sqlite dsn, got %q", cfg.WhatsAppStoreDSN())
	}
}

func TestLoadConfigUsesFileValuesOnly(t *testing.T) {
	t.Setenv("ADMINBOT_DB_HOST", "env-host-should-not-apply")
	t.Setenv("ANTHROPIC_API_KEY", "env-key-should-not-apply")

	dir := t.TempDir()
	path := filepath.Join(dir, "bot.conf")
	configText := `
bot_name: adminbot

database:
  host: config-host
  port: 5432
  user: wantastic
  password: secret
  database: wantastic_dev
  ssl_mode: disable

grpc:
  address: 127.0.0.1:50051

whatsapp:
  store_path: /tmp/adminbot-whatsapp.db

claude:
  api_key: config-key
`
	if err := os.WriteFile(path, []byte(configText), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.DB.Host != "config-host" {
		t.Fatalf("expected config host to win, got %q", cfg.DB.Host)
	}
	if cfg.Claude.APIKey != "config-key" {
		t.Fatalf("expected config Claude API key to win, got %q", cfg.Claude.APIKey)
	}
}
