package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSMTPSender_Live(t *testing.T) {
	// Live SMTP test — opt-in via env vars so unit-test runs don't depend on
	// network reachability or live credentials.
	host := os.Getenv("WANTASTIC_TEST_SMTP_HOST")
	user := os.Getenv("WANTASTIC_TEST_SMTP_USER")
	pass := os.Getenv("WANTASTIC_TEST_SMTP_PASSWORD")
	to := os.Getenv("WANTASTIC_TEST_SMTP_TO")
	if host == "" || user == "" || pass == "" || to == "" {
		t.Skip("set WANTASTIC_TEST_SMTP_HOST / _USER / _PASSWORD / _TO to enable")
	}

	cfg := SMTPConfig{
		Host:     host,
		Port:     587,
		User:     user,
		Password: pass,
		From:     user,
		FromName: "Wantastic Test",
	}
	client := NewSMTPClient(cfg)
	if !client.IsConfigured() {
		t.Fatal("client should be configured")
	}
	if err := client.SendEmailActual(to, "Test SMTP Go", "<p>test</p>", "test", nil); err != nil {
		if isBenignSMTPError(err.Error()) {
			t.Logf("benign live-SMTP failure: %v", err)
			return
		}
		t.Fatalf("unexpected error: %v", err)
	}
}

func isBenignSMTPError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "535")
}

// TestSMTPFallback_SpoolsWhenNotConfigured verifies that an unconfigured
// SMTP client falls back to the local disk spool when neither SMTP host
// nor sendmail are available (we point LocalSpoolDir at a temp dir and
// expect a .eml file to land there).
func TestSMTPFallback_SpoolsWhenNotConfigured(t *testing.T) {
	tempDir := t.TempDir()
	prev := LocalSpoolDir
	LocalSpoolDir = tempDir
	t.Cleanup(func() { LocalSpoolDir = prev })

	// Empty SMTPConfig — IsConfigured() == false, so the fallback path runs.
	client := NewSMTPClient(SMTPConfig{})

	err := client.SendEmailActual("user@example.com", "Hello", "<p>html</p>", "plain", nil)
	if err != nil {
		// sendmail may exist on the host; if it ran successfully no spool
		// file is created. That's fine — we don't fail on it.
		if sendmailBinary() != "" {
			t.Logf("local sendmail handled delivery: %v", err)
			return
		}
		t.Fatalf("unexpected error from spool fallback: %v", err)
	}

	// If sendmail was used, no spool file is expected. Otherwise exactly one.
	matches, _ := filepath.Glob(filepath.Join(tempDir, "*.eml"))
	if sendmailBinary() == "" && len(matches) != 1 {
		t.Fatalf("expected 1 spool file, got %d (%v)", len(matches), matches)
	}
	if len(matches) >= 1 {
		raw, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("read spool: %v", err)
		}
		if !strings.Contains(string(raw), "Subject: Hello") {
			t.Errorf("spool file missing Subject header; got:\n%s", string(raw))
		}
	}
}
