package email

import (
	"strings"
	"testing"
)

func TestBuildSMTPMessageWithAttachment(t *testing.T) {
	message, err := buildSMTPMessage(
		"Wantastic Admin Bot <bot@example.com>",
		"admin@example.com",
		"Adminbot WhatsApp Login QR",
		"Plain text body",
		"<p>HTML body</p>",
		[]Attachment{{
			Filename:    "adminbot-whatsapp-login.png",
			ContentType: "image/png",
			Data:        []byte{0x01, 0x02},
		}},
		"example.com",
	)
	if err != nil {
		t.Fatalf("buildSMTPMessage returned error: %v", err)
	}

	body := string(message)
	if !strings.Contains(body, "multipart/mixed") {
		t.Fatalf("expected multipart/mixed body, got %q", body)
	}
	if !strings.Contains(body, "adminbot-whatsapp-login.png") {
		t.Fatalf("expected attachment filename in body, got %q", body)
	}
	if !strings.Contains(body, "AQI=") {
		t.Fatalf("expected base64 attachment payload, got %q", body)
	}
}

func TestBuildSMTPMessageWithoutAttachment(t *testing.T) {
	message, err := buildSMTPMessage(
		"Wantastic Admin Bot <bot@example.com>",
		"admin@example.com",
		"Hello",
		"Plain text body",
		"<p>HTML body</p>",
		nil,
		"example.com",
	)
	if err != nil {
		t.Fatalf("buildSMTPMessage returned error: %v", err)
	}

	body := string(message)
	if !strings.Contains(body, "multipart/alternative") {
		t.Fatalf("expected multipart/alternative body, got %q", body)
	}
	if strings.Contains(body, "multipart/mixed") {
		t.Fatalf("did not expect multipart/mixed body, got %q", body)
	}
}
