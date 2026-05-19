package adminbot

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestParseAnalyticsFilters(t *testing.T) {
	filters, err := parseAnalyticsFilters("country=us, useragent=Chrome, peers>=3, peers<=8")
	if err != nil {
		t.Fatalf("parseAnalyticsFilters returned error: %v", err)
	}
	if filters.Country != "US" {
		t.Fatalf("expected country US, got %q", filters.Country)
	}
	if filters.UserAgent != "Chrome" {
		t.Fatalf("expected user agent Chrome, got %q", filters.UserAgent)
	}
	if filters.PeerCountGE == nil || *filters.PeerCountGE != 3 {
		t.Fatalf("expected peers>=3, got %#v", filters.PeerCountGE)
	}
	if filters.PeerCountLE == nil || *filters.PeerCountLE != 8 {
		t.Fatalf("expected peers<=8, got %#v", filters.PeerCountLE)
	}
}

func TestParseBotCommand(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "token only", input: "@wbot", want: "", wantOK: true},
		{name: "token with payload", input: "@wbot analytics", want: "analytics", wantOK: true},
		{name: "token with newline payload", input: "@wbot\nanalytics", want: "analytics", wantOK: true},
		{name: "token with leading whitespace", input: "   @wbot yes", want: "yes", wantOK: true},
		{name: "token with tab payload", input: "@wbot\tmenu", want: "menu", wantOK: true},
		{name: "embedded mention", input: "hello @wbot", want: "", wantOK: false},
		{name: "joined token", input: "@wbothello", want: "", wantOK: false},
		{name: "colon suffix", input: "@wbot: analytics", want: "", wantOK: false},
		{name: "comma suffix", input: "@wbot, analytics", want: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseBotCommand(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("expected ok=%v, got %v", tc.wantOK, ok)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSplitMessage(t *testing.T) {
	text := "line1\nline2\nline3\nline4"
	chunks := splitMessage(text, 8)
	if len(chunks) < 2 {
		t.Fatalf("expected more than one chunk, got %d", len(chunks))
	}
}

func TestEnsureWhatsAppStoreDirCreatesParent(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "nested", "whatsapp.db")
	if err := ensureWhatsAppStoreDir(storePath); err != nil {
		t.Fatalf("ensureWhatsAppStoreDir returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Dir(storePath)); err != nil {
		t.Fatalf("expected store directory to exist: %v", err)
	}
}

func TestAllowedGroup(t *testing.T) {
	bot := &Bot{
		cfg: &Config{
			WhatsApp: WhatsAppConfig{
				AllowedGroups: []string{"120363423506750832@g.us"},
			},
		},
	}

	if !bot.allowedGroup("120363423506750832@g.us") {
		t.Fatal("expected allowed group to pass")
	}
	if bot.allowedGroup("120363000000000000@g.us") {
		t.Fatal("expected non-allowed group to fail")
	}
}

func TestLogIncomingGroupMessageIncludesGroupID(t *testing.T) {
	var buf bytes.Buffer
	bot := &Bot{
		log: zerolog.New(&buf),
	}

	logger := bot.logIncomingGroupMessage("120363423506750832@g.us", "15551234567@s.whatsapp.net", false)
	logger.Warn().Msg("blocked group")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least two log lines, got %d", len(lines))
	}

	var received map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &received); err != nil {
		t.Fatalf("unmarshal received-group log: %v", err)
	}
	if received["group_id"] != "120363423506750832@g.us" {
		t.Fatalf("expected group_id field, got %#v", received["group_id"])
	}
	if received["allowed_group"] != false {
		t.Fatalf("expected allowed_group=false, got %#v", received["allowed_group"])
	}
	if received["sender"] != "15551234567@s.whatsapp.net" {
		t.Fatalf("expected sender field, got %#v", received["sender"])
	}
}

func TestConversationKeyIncludesChatAndSender(t *testing.T) {
	got := conversationKey("120363423506750832@g.us", "15551234567@s.whatsapp.net")
	want := "120363423506750832@g.us|15551234567@s.whatsapp.net"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDecorateReply(t *testing.T) {
	bot := &Bot{
		cfg: &Config{
			WhatsApp: WhatsAppConfig{
				ReplySignatureEnabled: true,
			},
		},
	}

	got := bot.decorateReply("hello")
	want := "hello\n\n_Wantastic Bot_"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	bot.cfg.WhatsApp.ReplySignatureEnabled = false
	got = bot.decorateReply("hello")
	if got != "hello" {
		t.Fatalf("expected undecorated reply, got %q", got)
	}
}

func TestMainMenuListIncludesRequiredFields(t *testing.T) {
	msg := mainMenuList()
	list := msg.GetListMessage()
	if list == nil {
		t.Fatal("expected list message")
	}
	if list.GetTitle() != "Wantastic Admin" {
		t.Fatalf("expected title to be set, got %q", list.GetTitle())
	}
	if list.GetButtonText() != "Open menu" {
		t.Fatalf("expected button text, got %q", list.GetButtonText())
	}
	if list.GetFooterText() != "Wantastic Bot" {
		t.Fatalf("expected footer text, got %q", list.GetFooterText())
	}
	if list.GetListType() != 1 {
		t.Fatalf("expected single-select list type, got %v", list.GetListType())
	}
	if len(list.GetSections()) != 1 || len(list.GetSections()[0].GetRows()) != 4 {
		t.Fatalf("expected one section with four rows, got %d sections and %d rows", len(list.GetSections()), len(list.GetSections()[0].GetRows()))
	}
}
