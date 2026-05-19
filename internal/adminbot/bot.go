package adminbot

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/auth"
	core "WantasticCore/internal/core"
	"WantasticCore/internal/store"
	"WantasticCore/internal/store/adapter"
	"WantasticCore/internal/tenant"
	pb "WantasticCore/internal/types"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"github.com/srlehn/termimg"
	_ "github.com/srlehn/termimg/drawers/all"
	_ "github.com/srlehn/termimg/terminals"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	flowAnalytics    = "analytics"
	flowTenantDetail = "tenant-detail"
	flowAddPeer      = "add-peer"
	flowAddTenant    = "add-tenant"
	botCommandToken  = "@wbot"
	botReplyFooter   = "_Wantastic Bot_"

	maxSessionEntries = 50
	maxHistoryDepth   = 10
	sessionTimeout    = 20 * time.Minute

	pairingQRPixelSize     = 512
	pairingQRMaxCells      = 36
	pairingQRMinCells      = 12
	pairingQRScreenPadding = 4
	pairingQRBottomPadding = 2
)

// strPtr returns a pointer to s. whatsmeow's proto-gen types use *string
// for optional fields; this avoids pulling in the proto runtime just for
// the trivial "address of string literal" helper.
func strPtr(s string) *string { return &s }

// stateCheckpoint holds a saved position for back-navigation.
type stateCheckpoint struct {
	Flow string
	Step int
}

type Bot struct {
	cfg         *Config
	log         zerolog.Logger
	telemetry   *TelemetryService
	registry    tenantManager
	isBusiness  bool
	claude      *ClaudeClient
	memory      *MemoryStore
	services    *core.Services
	waClient    *whatsmeow.Client
	sendMessage func(context.Context, waTypes.JID, *waE2E.Message) error
	qrWriter    io.Writer

	pairingQRFile                string
	pairingQRTerminalUnavailable bool

	stateMu sync.Mutex
	states  map[string]*conversationState
}

type conversationState struct {
	Flow        string
	Step        int
	StartedAt   time.Time
	LastTouched time.Time
	Analytics   analyticsForm
	Details     tenantDetailForm
	AddPeer     addPeerForm
	AddTenant   addTenantForm
	History     []stateCheckpoint // back-navigation stack
	EntryCount  int               // total inputs in this session
}

type analyticsForm struct {
	Category string
	Period   string
	Filters  AnalyticsFilters
}

type tenantDetailForm struct {
	TenantID string
}

type addPeerForm struct {
	TenantID string
	Name     string
}

type addTenantForm struct {
	FullName string
	Email    string
	Phone    string
	Tier     pb.AccountTier
	Password string
}

type tenantManager interface {
	ListTenants() ([]*tenant.Tenant, error)
	GetTenant(string) (*tenant.Tenant, error)
	GetTenantByEmail(string) (*tenant.Tenant, error)
	CreateTenant(*tenant.Tenant) error
}

func NewBot(ctx context.Context, cfg *Config, services *core.Services, logger zerolog.Logger) (*Bot, error) {
	if services == nil {
		return nil, fmt.Errorf("adminbot: in-process Services bundle is required")
	}
	storeCfg := store.Config{
		Host:         cfg.DB.Host,
		Port:         cfg.DB.Port,
		User:         cfg.DB.User,
		Password:     cfg.DB.Password,
		Database:     cfg.DB.Database,
		SSLMode:      cfg.DB.SSLMode,
		PoolSize:     cfg.DB.PoolSize,
		MinIdleConns: cfg.DB.MinIdleConns,
		MaxRetries:   cfg.DB.MaxRetries,
	}
	if err := store.Initialize(storeCfg); err != nil {
		return nil, fmt.Errorf("initialize store: %w", err)
	}

	registry := adapter.NewTenantRegistry(store.DB())
	telemetry := NewTelemetryService(store.DB().PG(), registry)

	if err := ensureWhatsAppStoreDir(cfg.WhatsApp.StorePath); err != nil {
		return nil, err
	}

	waContainer, err := sqlstore.New(ctx, "sqlite", cfg.WhatsAppStoreDSN(), waLog.Zerolog(logger.With().Str("component", "whatsapp").Logger()))
	if err != nil {
		return nil, fmt.Errorf("init whatsapp sql store: %w", err)
	}

	deviceStore, err := waContainer.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get whatsapp device store: %w", err)
	}

	waClient := whatsmeow.NewClient(deviceStore, waLog.Zerolog(logger.With().Str("component", "whatsapp").Logger()))
	waClient.Store.PushName = cfg.WhatsApp.DeviceName
	mem := NewMemoryStore(MemoryConfig{TTL: cfg.HistoryWindowDuration()})
	mem.StartCleanup(ctx)

	bot := &Bot{
		cfg:        cfg,
		log:        logger,
		telemetry:  telemetry,
		registry:   registry,
		claude:     NewClaudeClient(cfg.Claude),
		memory:     mem,
		services:   services,
		waClient:   waClient,
		isBusiness: false,
		sendMessage: func(ctx context.Context, chat waTypes.JID, message *waE2E.Message) error {
			_, err := waClient.SendMessage(ctx, chat, message)
			return err
		},
		qrWriter: os.Stdout,
		states:   make(map[string]*conversationState),
	}
	waClient.AddEventHandler(bot.handleEvent)

	return bot, nil
}

func (b *Bot) Start(ctx context.Context) error {
	if b.waClient.Store.ID == nil {
		if err := b.startPairing(ctx); err != nil {
			return err
		}
	} else if err := b.waClient.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp: %w", err)
	}

	b.refreshBusinessState(ctx)

	<-ctx.Done()
	b.waClient.Disconnect()
	return nil
}

func (b *Bot) Close() {
	if store.IsInitialized() {
		_ = store.DB().Close()
	}
}

func ensureWhatsAppStoreDir(storePath string) error {
	cleaned := strings.TrimSpace(storePath)
	if cleaned == "" || cleaned == ":memory:" {
		return nil
	}
	dir := filepath.Dir(filepath.Clean(cleaned))
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create whatsapp store directory %s: %w", dir, err)
	}
	return nil
}

func (b *Bot) startPairing(ctx context.Context) error {
	qrCtx, cancel := context.WithTimeout(ctx, b.cfg.LoginTimeoutDuration())
	defer cancel()
	defer b.cleanupPairingQRRenderer()

	qrChan, err := b.waClient.GetQRChannel(qrCtx)
	if err != nil {
		return fmt.Errorf("start qr channel: %w", err)
	}
	if err := b.waClient.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp: %w", err)
	}

	b.log.Info().
		Str("store_path", b.cfg.WhatsApp.StorePath).
		Msg("WhatsApp is not paired yet; starting QR login flow")

	lastCode := ""
	for {
		select {
		case <-qrCtx.Done():
			b.waClient.Disconnect()
			if errors.Is(qrCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("whatsapp pairing timed out after %s", b.cfg.LoginTimeoutDuration())
			}
			if errors.Is(qrCtx.Err(), context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return qrCtx.Err()
		case item, ok := <-qrChan:
			if !ok {
				if b.waClient.Store.ID != nil {
					b.log.Info().Msg("WhatsApp pairing completed")
					return nil
				}
				b.waClient.Disconnect()
				if err := qrCtx.Err(); err != nil {
					if errors.Is(err, context.Canceled) && ctx.Err() != nil {
						return nil
					}
					if !errors.Is(err, context.Canceled) {
						return fmt.Errorf("whatsapp QR flow stopped: %w", err)
					}
				}
				if ctx.Err() == nil {
					return fmt.Errorf("whatsapp QR flow stopped before pairing completed")
				}
				return nil
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				if item.Code == "" || item.Code == lastCode {
					continue
				}
				lastCode = item.Code
				// whatsmeow rotates the pairing code every ~20s until the user
				// scans it; render each fresh code as it arrives — no retry
				// loop, no email fallback. The QR loop is the qrChan range
				// itself, so we just print the current code and wait for the
				// next event (success or another QRChannelEventCode).
				b.renderPairingQRCode(item.Code)
			case "success":
				b.log.Info().Msg("WhatsApp pairing completed")
				b.isBusiness = IsBotBusiness(b.waClient)
				return nil
			case "timeout":
				b.waClient.Disconnect()
				return fmt.Errorf("whatsapp pairing timed out after %s without a successful scan", b.cfg.LoginTimeoutDuration())
			default:
				b.log.Info().Str("event", item.Event).Msg("WhatsApp QR event")
			}
		}
	}
}
func IsBotBusiness(client *whatsmeow.Client) bool {
	if client.Store.ID == nil {
		return false
	}
	profile, err := client.GetBusinessProfile(context.Background(), *client.Store.ID)
	if err != nil {
		return false
	}
	return profile != nil
}

func (b *Bot) refreshBusinessState(ctx context.Context) {
	if b == nil || b.waClient == nil || b.waClient.Store.ID == nil {
		b.isBusiness = false
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	profile, err := b.waClient.GetBusinessProfile(checkCtx, *b.waClient.Store.ID)
	if err != nil {
		b.isBusiness = false
		b.log.Debug().Err(err).Msg("Could not confirm WhatsApp business profile")
		return
	}

	b.isBusiness = profile != nil
	b.log.Info().Bool("is_business", b.isBusiness).Msg("Detected WhatsApp account capabilities")
}

// renderPairingQRCode
func (b *Bot) renderPairingQRCode(code string) {
	b.log.Info().Msg("Scan the QR code below with WhatsApp to pair adminbot")
	qrImg, err := buildPairingQRCodeImage(code)
	if err != nil {
		b.log.Warn().Err(err).Msg("Failed to build pairing QR image")
		b.renderPairingQRCodeFallback(code, err)
		return
	}

	if b.pairingQRTerminalUnavailable {
		b.renderPairingQRCodeFallback(code, nil)
		return
	}

	if err := b.drawPairingQRCode(qrImg); err != nil {
		if shouldDisableTerminalQR(err) {
			b.pairingQRTerminalUnavailable = true
			b.log.Info().Err(err).Msg("Terminal QR rendering unavailable in this environment; using PNG fallback")
		} else {
			b.log.Warn().Err(err).Msg("Failed to render pairing QR with termimg")
		}
		b.renderPairingQRCodeFallback(code, err)
	}
}

func (b *Bot) drawPairingQRCode(qrImg image.Image) error {
	tm, err := termimg.Terminal()
	if err != nil {
		return fmt.Errorf("open terminal for qr rendering: %w", err)
	}

	_, cursorY, err := tm.Cursor()
	if err != nil {
		return fmt.Errorf("query terminal cursor: %w", err)
	}

	termWidth, termHeight, err := tm.SizeInCells()
	if err != nil {
		return fmt.Errorf("query terminal size: %w", err)
	}

	bounds, err := pairingQRBounds(int(termWidth), int(termHeight), int(cursorY))
	if err != nil {
		return err
	}

	if err := tm.Draw(qrImg, bounds); err != nil {
		return fmt.Errorf("draw qr image: %w", err)
	}

	if err := tm.SetCursor(0, uint(bounds.Max.Y+1)); err != nil {
		b.log.Debug().Err(err).Msg("Failed to move cursor below rendered QR image")
	}

	return nil
}

func (b *Bot) renderPairingQRCodeFallback(code string, cause error) {
	path, created, err := b.ensurePairingQRFile()
	if err == nil {
		writeErr := qrcode.WriteFile(code, qrcode.Medium, pairingQRPixelSize, path)
		if writeErr == nil {
			if b.qrWriter != nil {
				if created {
					_, _ = fmt.Fprintf(
						b.qrWriter,
						"QR image saved to %s\nOpen it locally and scan it with WhatsApp.\n",
						path,
					)
				} else {
					_, _ = fmt.Fprintf(
						b.qrWriter,
						"QR image updated at %s\nRefresh it and scan it with WhatsApp.\n",
						path,
					)
				}
			}
			if created {
				b.log.Info().Str("path", path).Msg("Saved pairing QR fallback image")
			} else {
				b.log.Info().Str("path", path).Msg("Updated pairing QR fallback image")
			}
			return
		}
		err = writeErr
	}

	if b.qrWriter != nil {
		_, _ = fmt.Fprintln(
			b.qrWriter,
			"Unable to render the WhatsApp pairing QR code in this terminal. Please rerun adminbot in a local terminal that supports image rendering.",
		)
	}
	if cause != nil {
		b.log.Warn().Err(cause).Msg("QR render fallback exhausted")
	}
	if err != nil {
		b.log.Warn().Err(err).Msg("Failed to write pairing QR fallback image")
	}
}

func (b *Bot) ensurePairingQRFile() (path string, created bool, err error) {
	if strings.TrimSpace(b.pairingQRFile) != "" {
		return b.pairingQRFile, false, nil
	}

	preferredPath, err := b.pairingQRFallbackPath()
	if err == nil && strings.TrimSpace(preferredPath) != "" {
		if _, statErr := os.Stat(preferredPath); statErr == nil {
			b.pairingQRFile = preferredPath
			return preferredPath, false, nil
		} else if os.IsNotExist(statErr) {
			file, createErr := os.OpenFile(preferredPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if createErr == nil {
				_ = file.Close()
				b.pairingQRFile = preferredPath
				return preferredPath, true, nil
			}
			err = createErr
		} else {
			err = statErr
		}
	}

	tempFile, tempErr := os.CreateTemp("", "wantastic-adminbot-qr-*.png")
	if tempErr != nil {
		if err != nil {
			return "", false, err
		}
		return "", false, tempErr
	}
	path = tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", false, closeErr
	}
	b.pairingQRFile = path
	return path, true, nil
}

func (b *Bot) pairingQRFallbackPath() (string, error) {
	if b == nil || b.cfg == nil {
		return "", fmt.Errorf("missing bot config")
	}
	storePath := strings.TrimSpace(b.cfg.WhatsApp.StorePath)
	if storePath == "" || storePath == ":memory:" {
		return "", fmt.Errorf("whatsapp store path unavailable")
	}

	storeDir := filepath.Dir(filepath.Clean(storePath))
	if storeDir == "." || storeDir == "" {
		return "", fmt.Errorf("invalid whatsapp store directory")
	}
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return "", fmt.Errorf("create whatsapp store directory %s: %w", storeDir, err)
	}

	return filepath.Join(storeDir, "pairing-qr.png"), nil
}

func (b *Bot) cleanupPairingQRRenderer() {
	if err := termimg.CleanUp(); err != nil {
		b.log.Debug().Err(err).Msg("Failed to clean up termimg renderer")
	}
}

func shouldDisableTerminalQR(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "nil tty provider") ||
		strings.Contains(lower, "/dev/tty") ||
		strings.Contains(lower, "no such device or address") ||
		strings.Contains(lower, "failed tty provision")
}

func buildPairingQRCodeImage(code string) (image.Image, error) {
	qrCode, err := qrcode.New(code, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("create qr code: %w", err)
	}
	return qrCode.Image(pairingQRPixelSize), nil
}

func pairingQRBounds(termWidth, termHeight, cursorRow int) (image.Rectangle, error) {
	if termWidth <= 0 || termHeight <= 0 {
		return image.Rectangle{}, fmt.Errorf("invalid terminal size %dx%d", termWidth, termHeight)
	}
	if cursorRow < 0 {
		cursorRow = 0
	}

	availableWidth := termWidth - pairingQRScreenPadding
	availableHeight := termHeight - cursorRow - pairingQRBottomPadding
	side := pairingMinInt(pairingQRMaxCells, pairingMinInt(availableWidth, availableHeight))
	if side < pairingQRMinCells {
		return image.Rectangle{}, fmt.Errorf(
			"terminal too small for qr rendering: width=%d height=%d cursor=%d",
			termWidth,
			termHeight,
			cursorRow,
		)
	}

	startX := pairingMaxInt(0, (termWidth-side)/2)
	startY := cursorRow

	return image.Rect(startX, startY, startX+side, startY+side), nil
}

func pairingMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func pairingMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// =============================================================================
// Event routing
// =============================================================================

func (b *Bot) handleEvent(evt interface{}) {
	switch event := evt.(type) {
	case *events.Message:
		go b.handleIncomingMessage(event)
	}
}

func (b *Bot) logIncomingGroupMessage(chat, sender string, allowed bool) zerolog.Logger {
	logger := b.log.With().
		Str("chat", chat).
		Str("group_id", chat).
		Str("sender", sender).
		Bool("allowed_group", allowed).
		Logger()
	logger.Info().Msg("Received group message")
	return logger
}

func (b *Bot) handleIncomingMessage(evt *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error().Interface("panic", r).Msg("recovered panic in message handler")
		}
	}()

	if evt == nil || !evt.Info.IsGroup {
		return
	}
	chat := evt.Info.Chat.String()
	sender := evt.Info.Sender.ToNonAD().String()
	allowed := b.allowedGroup(chat)
	b.logIncomingGroupMessage(chat, sender, allowed)
	if !allowed {
		return
	}
	// Interactive replies (button/list taps) don't need the @wbot prefix.
	interactive := isInteractiveResponse(evt.Message)
	text := strings.TrimSpace(extractMessageText(evt.Message))
	if text == "" {
		return
	}

	key := conversationKey(chat, sender)

	var input string
	if interactive {
		// Only honour interactive replies when the user has an active session.
		if !b.activeState(key) {
			return
		}
		input = text
	} else {
		stripped, ok := parseBotCommand(text)
		if !ok {
			return
		}
		input = stripped
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.cfg.GRPCTimeout())
	defer cancel()

	active := b.activeState(key)

	// Handle navigation commands (text only).
	if !interactive {
		lower := strings.ToLower(input)
		switch {
		case input == "":
			b.clearState(key)
			if true {
				err := b.sendWhatsAppMessage(ctx, evt.Info.Chat, mainMenuList())
				if err != nil {
					b.log.Warn().
						Err(err).
						Str("chat", chat).
						Bool("is_business", b.isBusiness).
						Msg("Failed to send interactive main menu; falling back to text")
				} else {
					return
				}
			}
			_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, mainMenuMessage())
			return
		case lower == "cancel" || input == "0":
			b.clearState(key)
			_ = b.sendText(ctx, evt.Info.Chat, fmt.Sprintf("Cancelled. Send `%s` to open the menu.", botCommandToken))
			return
		case lower == "menu" || lower == "help":
			b.clearState(key)
			_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, mainMenuMessage())
			return
		case lower == "back":
			if !active {
				return
			}
			prev, ok := b.popHistory(key)
			if !ok || prev.Flow == "" {
				b.clearState(key)
				_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, mainMenuMessage())
				return
			}
			state := b.touchState(key)
			_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, b.rePromptForState(prev.Flow, prev.Step, state))
			return
		}
	}

	// Enforce session entry cap.
	state := b.touchState(key)
	state.EntryCount++
	if state.EntryCount > maxSessionEntries {
		b.clearState(key)
		_ = b.sendText(ctx, evt.Info.Chat, "Session limit reached.")
		_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, mainMenuMessage())
		return
	}

	msg, keep, err := b.advanceConversation(ctx, key, sender, input, active)
	if err != nil {
		b.log.Error().Err(err).Str("chat", chat).Str("sender", sender).Msg("message handling error")
		keep = false
		msg = textMsg("Error: " + err.Error())
	}
	if !keep {
		b.clearState(key)
	}
	if msg != nil {
		_ = b.sendWhatsAppMessage(ctx, evt.Info.Chat, b.decorateMsg(msg))
	}
}

// =============================================================================
// Conversation state machine
// =============================================================================

func (b *Bot) advanceConversation(ctx context.Context, key, senderID, input string, active bool) (*waE2E.Message, bool, error) {
	state := b.touchState(key)

	if !active {
		// User is at the main menu — push a blank checkpoint so "back" returns here.
		b.pushHistory(key, "", 0)
		switch menuSelection(input) {
		case "analytics":
			state.Flow = flowAnalytics
			state.Step = 0
			return analyticsCategoryMessage(), true, nil
		case "tenant":
			state.Flow = flowTenantDetail
			state.Step = 0
			return textMsg(fmt.Sprintf("Tenant lookup — send `%s <email / phone / ID / name>`.", botCommandToken)), true, nil
		case "add peer":
			state.Flow = flowAddPeer
			state.Step = 0
			return textMsg(fmt.Sprintf("Add peer — send `%s <tenant email / phone / ID / name>`.", botCommandToken)), true, nil
		case "add tenant":
			state.Flow = flowAddTenant
			state.Step = 0
			return textMsg(fmt.Sprintf("New tenant — send `%s <full name>` (step 1/5).", botCommandToken)), true, nil
		case "":
			// Empty input — show the menu.
			return mainMenuMessage(), false, nil
		default:
			// Not a menu selection — treat as a direct question to Claude.
			return b.handleDirectClaudeQuestion(ctx, key, senderID, input)
		}
	}

	// Push current position before advancing so "back" can restore it.
	b.pushHistory(key, state.Flow, state.Step)

	switch state.Flow {
	case flowAnalytics:
		return b.handleAnalyticsFlow(ctx, state, input)
	case flowTenantDetail:
		return b.handleTenantDetailFlow(ctx, state, input)
	case flowAddPeer:
		return b.handleAddPeerFlow(ctx, state, input)
	case flowAddTenant:
		return b.handleAddTenantFlow(ctx, state, input)
	default:
		return mainMenuMessage(), false, nil
	}
}

func (b *Bot) handleAnalyticsFlow(ctx context.Context, state *conversationState, input string) (*waE2E.Message, bool, error) {
	switch state.Step {
	case 0:
		state.Analytics.Category = normalizeCategory(input)
		state.Step = 1
		return analyticsPeriodMessage(), true, nil
	case 1:
		state.Analytics.Period = normalizePeriod(input)
		state.Step = 2
		return textMsg(fmt.Sprintf(
			"Optional filters (e.g. country=US, useragent=Chrome, peers>=3).\nSend `%s skip` for none.", botCommandToken,
		)), true, nil
	default:
		filters, err := parseAnalyticsFilters(input)
		if err != nil {
			return nil, true, err
		}
		state.Analytics.Filters = filters
		report, err := b.telemetry.RenderAnalytics(ctx, AnalyticsRequest{
			Category: state.Analytics.Category,
			Period:   state.Analytics.Period,
			Filters:  state.Analytics.Filters,
		})
		if err != nil {
			return nil, false, err
		}
		return textMsg(report), false, nil
	}
}

func (b *Bot) handleTenantDetailFlow(ctx context.Context, state *conversationState, input string) (*waE2E.Message, bool, error) {
	switch state.Step {
	case 0:
		card, err := b.telemetry.ResolveTenant(ctx, input)
		if err != nil {
			return nil, true, err
		}
		state.Details.TenantID = card.ID
		state.Step = 1
		return tenantActionMessage(emptyAs(card.FullName, card.Email), card.ID), true, nil
	default:
		switch strings.ToLower(strings.TrimSpace(input)) {
		case "card", "1":
			text, err := b.telemetry.RenderTenantCard(ctx, state.Details.TenantID)
			return textMsg(text), false, err
		case "activity", "activities", "2":
			text, err := b.telemetry.RenderTenantActivity(ctx, state.Details.TenantID)
			return textMsg(text), false, err
		default:
			return tenantActionMessage(state.Details.TenantID, state.Details.TenantID), true, nil
		}
	}
}

func (b *Bot) handleAddPeerFlow(ctx context.Context, state *conversationState, input string) (*waE2E.Message, bool, error) {
	switch state.Step {
	case 0:
		card, err := b.telemetry.ResolveTenant(ctx, input)
		if err != nil {
			return nil, true, err
		}
		state.AddPeer.TenantID = card.ID
		state.Step = 1
		return textMsg(fmt.Sprintf(
			"Tenant: *%s* (%s)\nSend `%s <peer-name>`.", emptyAs(card.FullName, card.Email), card.ID, botCommandToken,
		)), true, nil
	case 1:
		state.AddPeer.Name = strings.TrimSpace(input)
		if state.AddPeer.Name == "" {
			return textMsg("Peer name cannot be empty."), true, nil
		}
		state.Step = 2
		return confirmMessage(fmt.Sprintf("Add peer *%s* to tenant %s?", state.AddPeer.Name, state.AddPeer.TenantID)), true, nil
	default:
		if !isAffirmative(input) {
			return textMsg("Add peer cancelled."), false, nil
		}
		grpcCtx, cancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
		defer cancel()
		grpcCtx = auth.WithCallContext(grpcCtx, &auth.CallContext{TenantID: state.AddPeer.TenantID})
		resp, err := b.services.TenantPortal.AddTenantPeer(grpcCtx, &pb.AddTenantPeerRequest{
			TenantId: state.AddPeer.TenantID,
			Name:     state.AddPeer.Name,
		})
		if err != nil {
			return nil, false, fmt.Errorf("add tenant peer: %w", err)
		}
		lines := []string{
			fmt.Sprintf("Peer created for tenant %s.", state.AddPeer.TenantID),
			fmt.Sprintf("Peer ID: %s", resp.GetPeer().GetId()),
			fmt.Sprintf("Assigned IP: %s", resp.GetPeer().GetAssignedIp()),
		}
		if cfg := strings.TrimSpace(resp.GetConfig()); cfg != "" {
			lines = append(lines, "WireGuard config:", "```"+cfg+"```")
		} else {
			lines = append(lines, "WireGuard config is not available yet. Retrieve it later from the portal.")
		}
		return textMsg(strings.Join(lines, "\n")), false, nil
	}
}

func (b *Bot) handleAddTenantFlow(ctx context.Context, state *conversationState, input string) (*waE2E.Message, bool, error) {
	switch state.Step {
	case 0:
		state.AddTenant.FullName = strings.TrimSpace(input)
		if state.AddTenant.FullName == "" {
			return textMsg("Full name cannot be empty."), true, nil
		}
		state.Step = 1
		return textMsg(fmt.Sprintf("Email? (2/5)\nSend `%s <email>`.", botCommandToken)), true, nil
	case 1:
		state.AddTenant.Email = strings.TrimSpace(input)
		if state.AddTenant.Email == "" {
			return textMsg("Email cannot be empty."), true, nil
		}
		state.Step = 2
		return textMsg(fmt.Sprintf("Phone in international format? (3/5)\nSend `%s <+1234567890>`.", botCommandToken)), true, nil
	case 2:
		state.AddTenant.Phone = strings.TrimSpace(input)
		if state.AddTenant.Phone == "" {
			return textMsg("Phone cannot be empty."), true, nil
		}
		state.Step = 3
		return tierSelectMessage(), true, nil
	case 3:
		tier, err := parseTier(input)
		if err != nil {
			return tierSelectMessage(), true, nil
		}
		state.AddTenant.Tier = tier
		state.Step = 4
		return textMsg(fmt.Sprintf("Password? (5/5)\nSend `%s <password>`.", botCommandToken)), true, nil
	case 4:
		state.AddTenant.Password = strings.TrimSpace(input)
		if state.AddTenant.Password == "" {
			return textMsg("Password cannot be empty."), true, nil
		}
		state.Step = 5
		return confirmMessage(fmt.Sprintf(
			"Create tenant?\nName: %s\nEmail: %s\nPhone: %s\nTier: %s",
			state.AddTenant.FullName, state.AddTenant.Email,
			state.AddTenant.Phone, tierName(state.AddTenant.Tier),
		)), true, nil
	default:
		if !isAffirmative(input) {
			return textMsg("Add tenant cancelled."), false, nil
		}
		summary, err := b.createTenant(ctx, state.AddTenant)
		return textMsg(summary), false, err
	}
}

// handleDirectClaudeQuestion routes a free-form question directly to Claude,
// carrying the chatter's per-group compressed conversation memory for continuity.
// It never enters a flow state — the response is always terminal (keep=false).
func (b *Bot) handleDirectClaudeQuestion(ctx context.Context, memoryKey, senderID, question string) (*waE2E.Message, bool, error) {
	if !b.claude.Enabled() {
		return textMsg("Claude is not configured. Add claude.api_key to /etc/bot.conf."), false, nil
	}
	if strings.TrimSpace(question) == "" {
		return mainMenuMessage(), false, nil
	}

	contextText, err := b.telemetry.BuildClaudeContext(ctx, question, AnalyticsFilters{})
	if err != nil {
		b.log.Warn().Err(err).Str("sender", senderID).Msg("failed to build telemetry context for claude")
		contextText = ""
	}

	const systemPrompt = "You are Wantastic adminbot. " +
		"Use the provided telemetry context and conversation history to answer accurately. " +
		"The pre-baked context only includes the top 5 tenants by activity — when a question " +
		"requires data outside that (e.g. \"who is the oldest tenant?\", \"how many signed up " +
		"last week?\", or anything that needs an email or phone number), call the list_tenants " +
		"or get_tenant tool instead of saying you can't answer. " +
		"When the user asks you to change something operationally, use the matching write tool " +
		"instead of apologizing or claiming the backend is unavailable. " +
		"For add-device requests, use create_peer. If the tenant is already clear from context " +
		"but the user did not provide a device name, call create_peer without `name` and let " +
		"the tool auto-generate one. " +
		"Never invent facts that are not present in the context or tool results. " +
		"Keep answers concise and operational."

	answer, err := b.claude.AskWithMemory(ctx, b.memory, memoryKey, systemPrompt, contextText, question, b)
	if err != nil {
		return nil, false, err
	}
	return textMsg(answer), false, nil
}

// =============================================================================
// Back-navigation helpers
// =============================================================================

// pushHistory saves the given flow+step onto the session's navigation stack.
func (b *Bot) pushHistory(key, flow string, step int) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	state, ok := b.states[key]
	if !ok {
		return
	}
	if len(state.History) >= maxHistoryDepth {
		state.History = state.History[1:] // drop oldest
	}
	state.History = append(state.History, stateCheckpoint{Flow: flow, Step: step})
}

// popHistory restores the previous flow+step and returns it.
func (b *Bot) popHistory(key string) (stateCheckpoint, bool) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	state, ok := b.states[key]
	if !ok || len(state.History) == 0 {
		return stateCheckpoint{}, false
	}
	prev := state.History[len(state.History)-1]
	state.History = state.History[:len(state.History)-1]
	state.Flow = prev.Flow
	state.Step = prev.Step
	return prev, true
}

// rePromptForState returns the right interactive/text prompt for a restored state.
func (b *Bot) rePromptForState(flow string, step int, state *conversationState) *waE2E.Message {
	switch flow {
	case "":
		return mainMenuMessage()
	case flowAnalytics:
		switch step {
		case 0:
			return analyticsCategoryMessage()
		case 1:
			return analyticsPeriodMessage()
		default:
			return textMsg(fmt.Sprintf("Optional filters (country=US, useragent=Chrome, peers>=3).\nSend `%s skip` for none.", botCommandToken))
		}
	case flowTenantDetail:
		switch step {
		case 0:
			return textMsg(fmt.Sprintf("Tenant lookup — send `%s <email / phone / ID / name>`.", botCommandToken))
		default:
			return tenantActionMessage(state.Details.TenantID, state.Details.TenantID)
		}
	case flowAddPeer:
		switch step {
		case 0:
			return textMsg(fmt.Sprintf("Add peer — send `%s <tenant email / phone / ID / name>`.", botCommandToken))
		case 1:
			return textMsg(fmt.Sprintf("Tenant: %s\nSend `%s <peer-name>`.", state.AddPeer.TenantID, botCommandToken))
		default:
			return confirmMessage(fmt.Sprintf("Add peer *%s* to tenant %s?", state.AddPeer.Name, state.AddPeer.TenantID))
		}
	case flowAddTenant:
		return b.addTenantStepPrompt(step, state)
	}
	return mainMenuMessage()
}

func (b *Bot) addTenantStepPrompt(step int, state *conversationState) *waE2E.Message {
	switch step {
	case 0:
		return textMsg(fmt.Sprintf("Full name? (1/5)\nSend `%s <name>`.", botCommandToken))
	case 1:
		return textMsg(fmt.Sprintf("Email? (2/5) — Name: %s\nSend `%s <email>`.", state.AddTenant.FullName, botCommandToken))
	case 2:
		return textMsg(fmt.Sprintf("Phone? (3/5) — Email: %s\nSend `%s <+1234567890>`.", state.AddTenant.Email, botCommandToken))
	case 3:
		return tierSelectMessage()
	case 4:
		return textMsg(fmt.Sprintf("Password? (5/5)\nSend `%s <password>`.", botCommandToken))
	default:
		return confirmMessage(fmt.Sprintf("Create tenant?\nName: %s\nEmail: %s\nPhone: %s\nTier: %s",
			state.AddTenant.FullName, state.AddTenant.Email,
			state.AddTenant.Phone, tierName(state.AddTenant.Tier)))
	}
}

// =============================================================================
// Interactive message builders
// =============================================================================

// textMsg wraps a plain string in a WhatsApp Conversation message.
func textMsg(text string) *waE2E.Message {
	return &waE2E.Message{Conversation: strPtr(text)}
}

// mainMenuMessage returns the main menu as a formatted text message.
// WhatsApp personal/linked accounts cannot send interactive List/Button messages
// in group chats — they are silently dropped by WA servers.
func mainMenuMessage() *waE2E.Message {
	return textMsg(strings.Join([]string{
		"*Wantastic Admin*",
		"",
		"1️⃣  Analytics",
		"2️⃣  Tenant details",
		"3️⃣  Add peer",
		"4️⃣  Add tenant",
		"",
		fmt.Sprintf("Reply `%s <number>` — e.g. `%s 1`", botCommandToken, botCommandToken),
		fmt.Sprintf("Or just ask me anything directly with `%s <question>`", botCommandToken),
		fmt.Sprintf("`%s cancel` to stop  •  `%s back` to go back", botCommandToken, botCommandToken),
	}, "\n"))
}
func mainMenuList() *waE2E.Message {
	return &waE2E.Message{
		ListMessage: &waE2E.ListMessage{
			Title:       strPtr("Wantastic Admin"),
			Description: strPtr("Choose an option for adminbot."),
			ButtonText:  strPtr("Open menu"),
			ListType:    waE2E.ListMessage_SINGLE_SELECT.Enum(),
			FooterText:  strPtr("Wantastic Bot"),
			Sections: []*waE2E.ListMessage_Section{{
				Title: strPtr("Main Menu"),
				Rows: []*waE2E.ListMessage_Row{
					{RowID: strPtr("analytics"), Title: strPtr("Analytics"), Description: strPtr("View usage analytics")},
					{RowID: strPtr("tenant"), Title: strPtr("Tenant details"), Description: strPtr("View tenant information")},
					{RowID: strPtr("add peer"), Title: strPtr("Add peer"), Description: strPtr("Add a new peer to a tenant")},
					{RowID: strPtr("add tenant"), Title: strPtr("Add tenant"), Description: strPtr("Create a new tenant")},
				},
			}},
		},
	}
}

func analyticsCategoryMessage() *waE2E.Message {
	return textMsg(fmt.Sprintf(
		"*Analytics — choose category:*\n1. Peers\n2. Tenants\n3. Winbox\n\nReply `%s <number or name>`.",
		botCommandToken,
	))
}

func analyticsPeriodMessage() *waE2E.Message {
	return textMsg(fmt.Sprintf(
		"*Analytics — choose period:*\n1. Today\n2. This month\n3. All time\n\nReply `%s <number or name>`.",
		botCommandToken,
	))
}

func tenantActionMessage(name, id string) *waE2E.Message {
	return textMsg(fmt.Sprintf(
		"*Tenant matched:* %s (%s)\n\n1. Card\n2. Activity\n\nReply `%s card` or `%s activity`.",
		name, id, botCommandToken, botCommandToken,
	))
}

func tierSelectMessage() *waE2E.Message {
	return textMsg(fmt.Sprintf(
		"*Choose tier (4/5):*\n1. Free\n2. Standard\n3. Premium\n\nReply `%s <name>`.",
		botCommandToken,
	))
}

func confirmMessage(question string) *waE2E.Message {
	return textMsg(fmt.Sprintf(
		"%s\n\nReply `%s yes` to confirm or `%s cancel` to abort.",
		question, botCommandToken, botCommandToken,
	))
}

// decorateMsg appends the reply footer to plain text messages.
func (b *Bot) decorateMsg(msg *waE2E.Message) *waE2E.Message {
	if msg == nil || !b.cfg.WhatsApp.ReplySignatureEnabled {
		return msg
	}
	if conv := msg.GetConversation(); conv != "" {
		msg.Conversation = strPtr(conv + "\n\n" + botReplyFooter)
	}
	return msg
}

// =============================================================================
// Tenant creation
// =============================================================================

func (b *Bot) createTenant(ctx context.Context, form addTenantForm) (string, error) {
	if _, err := b.registry.GetTenantByEmail(form.Email); err == nil {
		return "", fmt.Errorf("a tenant with email %s already exists", form.Email)
	}

	grpcCtx, cancel := context.WithTimeout(ctx, b.cfg.GRPCTimeout())
	defer cancel()
	accountResp, err := b.services.Account.CreateAccount(grpcCtx, &pb.CreateAccountRequest{
		Name: fmt.Sprintf("tenant-%s", uuid.NewString()[:8]),
	})
	if err != nil {
		return "", fmt.Errorf("create overlay account: %w", err)
	}

	account := accountResp.GetAccount()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(form.Password), bcrypt.DefaultCost)
	if err != nil {
		rollbackDeleteAccount(grpcCtx, b.services.Account, account.GetId())
		return "", fmt.Errorf("hash password: %w", err)
	}

	tenantID := uuid.NewString()
	newTenant := &tenant.Tenant{
		ID:               tenantID,
		Email:            form.Email,
		FullName:         form.FullName,
		PasswordHash:     string(passwordHash),
		OverlayAccountID: account.GetId(),
		Networks:         append([]string(nil), account.GetNetworks()...),
		Status:           "active",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	if err := b.registry.CreateTenant(newTenant); err != nil {
		rollbackDeleteAccount(grpcCtx, b.services.Account, account.GetId())
		return "", fmt.Errorf("create tenant record: %w", err)
	}

	lines := []string{
		fmt.Sprintf("Tenant created: %s", newTenant.FullName),
		fmt.Sprintf("Tenant ID: %s", newTenant.ID),
		fmt.Sprintf("Overlay account: %s", newTenant.OverlayAccountID),
		fmt.Sprintf("Networks: %s", strings.Join(newTenant.Networks, ", ")),
	}
	return strings.Join(lines, "\n"), nil
}

func rollbackDeleteAccount(ctx context.Context, svc core.AccountService, accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	_, _ = svc.DeleteAccount(ctx, &pb.DeleteAccountRequest{AccountId: accountID})
}

// =============================================================================
// WhatsApp send helpers
// =============================================================================

func (b *Bot) sendText(ctx context.Context, chat waTypes.JID, text string) error {
	text = b.decorateReply(text)
	chunks := splitMessage(text, 3200)
	for _, chunk := range chunks {
		if err := b.sendWhatsAppMessage(ctx, chat, &waE2E.Message{
			Conversation: strPtr(chunk),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) sendWhatsAppMessage(ctx context.Context, chat waTypes.JID, message *waE2E.Message) error {
	if b.sendMessage != nil {
		return b.sendMessage(ctx, chat, message)
	}
	if b.waClient == nil {
		return fmt.Errorf("whatsapp client is not configured")
	}
	_, err := b.waClient.SendMessage(ctx, chat, message)
	return err
}

// =============================================================================
// Message text extraction
// =============================================================================

func extractMessageText(message *waE2E.Message) string {
	if message == nil {
		return ""
	}
	if text := strings.TrimSpace(message.GetConversation()); text != "" {
		return text
	}
	if text := strings.TrimSpace(message.GetExtendedTextMessage().GetText()); text != "" {
		return text
	}
	// Button reply (ButtonsMessage response)
	if br := message.GetButtonsResponseMessage(); br != nil {
		if id := strings.TrimSpace(br.GetSelectedButtonID()); id != "" {
			return id
		}
		return strings.TrimSpace(br.GetSelectedDisplayText())
	}
	// List reply (ListMessage response)
	if lr := message.GetListResponseMessage(); lr != nil {
		if id := strings.TrimSpace(lr.GetSingleSelectReply().GetSelectedRowID()); id != "" {
			return id
		}
	}
	// Template button reply
	if tbr := message.GetTemplateButtonReplyMessage(); tbr != nil {
		if id := strings.TrimSpace(tbr.GetSelectedID()); id != "" {
			return id
		}
	}
	return ""
}
func isBusinessAccount(info *types.ContactInfo) bool {
	if info == nil {
		return false
	}
	return info.BusinessName != ""
}

// isInteractiveResponse returns true when the message is a button or list tap.
func isInteractiveResponse(message *waE2E.Message) bool {
	if message == nil {
		return false
	}
	return message.GetButtonsResponseMessage() != nil ||
		message.GetListResponseMessage() != nil ||
		message.GetTemplateButtonReplyMessage() != nil
}

// =============================================================================
// Input parsing helpers
// =============================================================================

func parseBotCommand(text string) (string, bool) {
	trimmedLeft := strings.TrimLeft(text, " \t\r\n")
	lower := strings.ToLower(trimmedLeft)
	if !strings.HasPrefix(lower, botCommandToken) {
		return "", false
	}
	if len(trimmedLeft) == len(botCommandToken) {
		return "", true
	}
	next := trimmedLeft[len(botCommandToken)]
	if next != ' ' && next != '\n' && next != '\t' && next != '\r' {
		return "", false
	}
	return strings.TrimSpace(trimmedLeft[len(botCommandToken):]), true
}

func parseAnalyticsFilters(input string) (AnalyticsFilters, error) {
	var filters AnalyticsFilters
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || strings.EqualFold(trimmed, "skip") {
		return filters, nil
	}

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		item := strings.TrimSpace(strings.ToLower(part))
		switch {
		case strings.HasPrefix(item, "country="):
			filters.Country = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(item, "country=")))
		case strings.HasPrefix(item, "useragent="):
			filters.UserAgent = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "useragent="))
		case strings.HasPrefix(item, "peers>="):
			value, err := parseInt(strings.TrimSpace(strings.TrimPrefix(item, "peers>=")))
			if err != nil {
				return filters, fmt.Errorf("invalid peers>= filter")
			}
			filters.PeerCountGE = &value
		case strings.HasPrefix(item, "peers<="):
			value, err := parseInt(strings.TrimSpace(strings.TrimPrefix(item, "peers<=")))
			if err != nil {
				return filters, fmt.Errorf("invalid peers<= filter")
			}
			filters.PeerCountLE = &value
		case strings.HasPrefix(item, "peers="):
			value, err := parseInt(strings.TrimSpace(strings.TrimPrefix(item, "peers=")))
			if err != nil {
				return filters, fmt.Errorf("invalid peers= filter")
			}
			filters.PeerCountEQ = &value
		default:
			return filters, fmt.Errorf("unknown filter %q", strings.TrimSpace(part))
		}
	}

	return filters, nil
}

func menuSelection(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	switch lower {
	case "", "start":
		return ""
	case "1", "analytics":
		return "analytics"
	case "2", "tenant", "tenant detail", "tenant details":
		return "tenant"
	case "3", "add peer", "peer", "add-peer":
		return "add peer"
	case "4", "add tenant", "add-tenant":
		return "add tenant"
	default:
		return lower
	}
}

func isAffirmative(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes", "confirm", "ok":
		return true
	default:
		return false
	}
}

func parseTier(input string) (pb.AccountTier, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "free", "0", "tier_free":
		return pb.AccountTier_TIER_FREE, nil
	case "standard", "1", "tier_standard":
		return pb.AccountTier_TIER_STANDARD, nil
	case "premium", "2", "tier_premium":
		return pb.AccountTier_TIER_PREMIUM, nil
	default:
		return pb.AccountTier_TIER_FREE, fmt.Errorf("tier must be free, standard, or premium")
	}
}

func tierName(tier pb.AccountTier) string {
	switch tier {
	case pb.AccountTier_TIER_STANDARD:
		return "standard"
	case pb.AccountTier_TIER_PREMIUM:
		return "premium"
	default:
		return "free"
	}
}

func parseInt(value string) (int, error) {
	var parsed int
	_, err := fmt.Sscanf(value, "%d", &parsed)
	return parsed, err
}

func splitMessage(text string, maxLen int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) <= maxLen {
		return []string{trimmed}
	}

	parts := strings.Split(trimmed, "\n")
	var chunks []string
	var current strings.Builder
	for _, part := range parts {
		line := strings.TrimRight(part, " ")
		if current.Len() > 0 && current.Len()+len(line)+1 > maxLen {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	if len(chunks) == 0 {
		return []string{trimmed}
	}
	return chunks
}

// =============================================================================
// Session state management
// =============================================================================

func (b *Bot) allowedGroup(chat string) bool {
	if len(b.cfg.WhatsApp.AllowedGroups) == 0 {
		return true
	}
	for _, allowed := range b.cfg.WhatsApp.AllowedGroups {
		if strings.EqualFold(strings.TrimSpace(allowed), chat) {
			return true
		}
	}
	return false
}

func (b *Bot) decorateReply(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if !b.cfg.WhatsApp.ReplySignatureEnabled {
		return trimmed
	}
	return trimmed + "\n\n" + botReplyFooter
}

func conversationKey(chat, sender string) string {
	return chat + "|" + sender
}

func (b *Bot) touchState(key string) *conversationState {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()

	state, ok := b.states[key]
	if !ok {
		state = &conversationState{
			StartedAt:   time.Now().UTC(),
			LastTouched: time.Now().UTC(),
		}
		b.states[key] = state
	}
	state.LastTouched = time.Now().UTC()
	return state
}

func (b *Bot) activeState(key string) bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()

	state, ok := b.states[key]
	if !ok {
		return false
	}
	if time.Since(state.LastTouched) > sessionTimeout {
		delete(b.states, key)
		return false
	}
	return state.Flow != ""
}

func (b *Bot) clearState(key string) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	delete(b.states, key)
}
