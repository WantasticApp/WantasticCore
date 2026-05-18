package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/rs/zerolog/log"
)

// SMTPConfig holds SMTP configuration.
type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	UseTLS   bool
	From     string
	FromName string
}

type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// SMTPClient manages SMTP email interactions.
type SMTPClient struct {
	config SMTPConfig
	ctx    context.Context

	// In-memory verification store for codes
	verifications map[string]*pendingVerification
	mu            sync.RWMutex
}

// NewSMTPClient creates a new SMTP client.
func NewSMTPClient(cfg SMTPConfig) *SMTPClient {
	return &SMTPClient{
		config:        cfg,
		ctx:           context.Background(),
		verifications: make(map[string]*pendingVerification),
	}
}

// IsConfigured returns true if SMTP is properly configured.
func (c *SMTPClient) IsConfigured() bool {
	return c.config.Host != "" && c.config.User != "" && c.config.From != ""
}

// SendVerificationEmail sends a verification code to an email address.
func (c *SMTPClient) SendVerificationEmail(email string) error {
	code, err := generateVerificationCode()
	if err != nil {
		return fmt.Errorf("failed to generate verification code: %w", err)
	}

	c.mu.Lock()
	c.verifications[email] = &pendingVerification{
		Email:     email,
		Code:      code,
		ExpiresAt: time.Now().Add(10 * time.Minute),
		Attempts:  0,
	}
	c.mu.Unlock()

	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(email)).
			Str("code", code).
			Msg(" LOCAL EMAIL VERIFICATION (SMTP not configured) - Code logged for testing")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, VerificationEmail(code))
	if err != nil {
		return fmt.Errorf("failed to render verification email: %w", err)
	}
	textContent := fmt.Sprintf("Your Wantastic verification code is: %s\n\nThis code expires in 10 minutes.", code)

	return c.SendEmailActual(email, "Your Wantastic Verification Code", htmlContent, textContent, nil)
}

// CheckVerification checks a verification code.
func (c *SMTPClient) CheckVerification(email, code string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	code = strings.TrimSpace(code)

	v, exists := c.verifications[email]
	if !exists {
		log.Warn().Str("email", maskEmail(email)).Msg("❌ No pending verification found for email")
		return false, nil
	}

	if time.Now().After(v.ExpiresAt) {
		delete(c.verifications, email)
		log.Warn().Str("email", maskEmail(email)).Msg("❌ Verification code expired")
		return false, nil
	}

	v.Attempts++
	if v.Attempts > 5 {
		delete(c.verifications, email)
		return false, fmt.Errorf("too many verification attempts")
	}

	if v.Code == code {
		delete(c.verifications, email)
		log.Debug().Str("email", maskEmail(email)).Msg(" Email verification approved")
		return true, nil
	}

	log.Warn().
		Str("email", maskEmail(email)).
		Int("attempts", v.Attempts).
		Str("expected_prefix", v.Code[:2]+"****").
		Str("received_prefix", code[:min(2, len(code))]+"****").
		Int("expected_len", len(v.Code)).
		Int("received_len", len(code)).
		Msg("❌ Wrong verification code")
	return false, nil
}

// SendLoginCode sends a login verification code to an email address.
func (c *SMTPClient) SendLoginCode(email, code string) error {
	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(email)).
			Str("code", code).
			Msg(" LOCAL LOGIN CODE (SMTP not configured) - Code logged for testing")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, VerificationEmail(code))
	if err != nil {
		return fmt.Errorf("failed to render login code email: %w", err)
	}
	textContent := fmt.Sprintf("Your Wantastic verification code is: %s\n\nThis code expires in 10 minutes.", code)

	return c.SendEmailActual(email, "Your Login Verification Code", htmlContent, textContent, nil)
}

// SendPasswordResetCode sends a password reset verification code to an email address.
func (c *SMTPClient) SendPasswordResetCode(email, code string) error {
	c.mu.Lock()
	c.verifications[email] = &pendingVerification{
		Email:     email,
		Code:      code,
		ExpiresAt: time.Now().Add(10 * time.Minute),
		Attempts:  0,
	}
	c.mu.Unlock()

	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(email)).
			Str("code", code).
			Msg(" LOCAL PASSWORD RESET (SMTP not configured) - Code logged for testing")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, PasswordResetEmail(code))
	if err != nil {
		return fmt.Errorf("failed to render password reset email: %w", err)
	}
	textContent := fmt.Sprintf("Your Wantastic password reset code is: %s\n\nThis code expires in 10 minutes.\n\nIf you did not request a password reset, please ignore this email.", code)

	return c.SendEmailActual(email, "Reset Your Wantastic Password", htmlContent, textContent, nil)
}

// HasPendingVerification checks if there's a pending verification for an email.
func (c *SMTPClient) HasPendingVerification(email string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	v, exists := c.verifications[email]
	if !exists {
		return false
	}
	return time.Now().Before(v.ExpiresAt)
}

// CleanupExpired removes expired verifications.
func (c *SMTPClient) CleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for email, v := range c.verifications {
		if now.After(v.ExpiresAt) {
			delete(c.verifications, email)
		}
	}
}

// SendEmail sends a custom email with the provided subject and content.
func (c *SMTPClient) SendEmail(toEmail, subject, htmlContent, textContent string) error {
	return c.SendEmailWithAttachments(toEmail, subject, htmlContent, textContent, nil)
}

// SendEmailWithAttachments sends a custom email with optional attachments.
func (c *SMTPClient) SendEmailWithAttachments(toEmail, subject, htmlContent, textContent string, attachments []Attachment) error {
	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(toEmail)).
			Str("subject", subject).
			Msg(" LOCAL EMAIL (SMTP not configured) - Email logged for testing")
		log.Debug().Str("text_content", textContent).Msg("Email content")
		return nil
	}

	renderedHTML, err := renderToString(c.ctx, GenericEmail(subject, htmlContent))
	if err != nil {
		log.Warn().Err(err).Msg("Failed to render generic layout, sending raw content")
	} else {
		htmlContent = renderedHTML
	}

	return c.SendEmailActual(toEmail, subject, htmlContent, textContent, attachments)
}

// SendInactivityWarning sends an inactivity warning email.
func (c *SMTPClient) SendInactivityWarning(email, fullName string, daysInactive int) error {
	if !c.IsConfigured() {
		log.Warn().Str("email", maskEmail(email)).Msg(" LOCAL INACTIVITY WARNING (SMTP not configured)")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, InactivityWarningEmail(fullName, daysInactive))
	if err != nil {
		return fmt.Errorf("failed to render inactivity warning: %w", err)
	}

	textContent := fmt.Sprintf("Hi %s, we noticed you haven't logged into your Wantastic account for %d days. We miss you, your free account is still here, and if you need a refresher you can visit https://wantastic.app/docs anytime.", fullName, daysInactive)

	return c.SendEmailActual(email, "We miss you at Wantastic", htmlContent, textContent, nil)
}

// SendAccountDeleted sends a final account deletion notification.
func (c *SMTPClient) SendAccountDeleted(email, fullName string) error {
	if !c.IsConfigured() {
		log.Warn().Str("email", maskEmail(email)).Msg(" LOCAL ACCOUNT DELETED (SMTP not configured)")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, AccountDeletedEmail(fullName))
	if err != nil {
		return fmt.Errorf("failed to render account deleted email: %w", err)
	}

	textContent := fmt.Sprintf("Hi %s, your Wantastic free account has been deleted due to inactivity.", fullName)

	return c.SendEmailActual(email, "Wantastic Account Deleted Due to Inactivity", htmlContent, textContent, nil)
}

// SendPeerOfflineNotification sends an email about offline peers.
func (c *SMTPClient) SendPeerOfflineNotification(email, fullName string, peers []EmailPeerInfo, unsubscribeURL string) error {
	if !c.IsConfigured() {
		log.Warn().Str("email", maskEmail(email)).Msg(" LOCAL OFFLINE NOTIFICATION (SMTP not configured)")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, PeerOfflineNotificationEmail(fullName, peers, unsubscribeURL))
	if err != nil {
		return fmt.Errorf("failed to render offline notification: %w", err)
	}

	textContent := fmt.Sprintf("Hi %s, %d device(s) in your network have gone offline.", fullName, len(peers))

	return c.SendEmailActual(email, fmt.Sprintf("Wantastic Alert: %d Device(s) Offline", len(peers)), htmlContent, textContent, nil)
}

// SendMigrationInvitation sends a device migration invitation email.
func (c *SMTPClient) SendMigrationInvitation(invite MigrationInvitation) error {
	acceptURL := fmt.Sprintf("%s/#accept-migration?token=%s", invite.BaseURL, invite.InviteToken)

	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(invite.ToEmail)).
			Str("from", invite.OwnerEmail).
			Str("accept_url", acceptURL).
			Msg(" LOCAL MIGRATION INVITATION (SMTP not configured) - Email logged for testing")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, MigrationInvitationEmail(invite.OwnerName, invite.OwnerEmail, invite.DeviceNames, invite.DeviceCount, acceptURL, invite.ExpiresInDays))
	if err != nil {
		return fmt.Errorf("failed to render migration invitation email: %w", err)
	}

	deviceList := ""
	for i, name := range invite.DeviceNames {
		if i >= 5 {
			deviceList += fmt.Sprintf("  ... and %d more\n", invite.DeviceCount-5)
			break
		}
		deviceList += fmt.Sprintf("  • %s\n", name)
	}

	textContent := fmt.Sprintf(
		"%s (%s) wants to transfer %d device(s) to your Wantastic account.\n\n"+
			"Devices:\n%s\n"+
			"To accept this transfer, visit: %s\n\n"+
			"This invitation expires in %d days.\n\n"+
			"If you don't have a Wantastic account, you'll be able to create one when you accept.\n\n"+
			"If you didn't expect this invitation, you can safely ignore this email.",
		invite.OwnerName, invite.OwnerEmail, invite.DeviceCount, deviceList, acceptURL, invite.ExpiresInDays,
	)

	return c.SendEmailActual(invite.ToEmail, fmt.Sprintf("%s wants to transfer %d device(s) to you", invite.OwnerName, invite.DeviceCount), htmlContent, textContent, nil)
}

// SendSecurityAlert sends a security alert email.
func (c *SMTPClient) SendSecurityAlert(email, token, actionDescription string) error {
	baseURL := "https://app.wantastic.app"
	alertURL := fmt.Sprintf("%s/#security-alert?token=%s", baseURL, token)

	if !c.IsConfigured() {
		log.Warn().
			Str("email", maskEmail(email)).
			Str("action", actionDescription).
			Msg(" LOCAL SECURITY ALERT (SMTP not configured) - Email logged for testing")
		return nil
	}

	htmlContent, err := renderToString(c.ctx, SecurityAlertEmail(actionDescription, alertURL))
	if err != nil {
		return fmt.Errorf("failed to render security alert email: %w", err)
	}

	textContent := fmt.Sprintf(
		"Security Alert: %s\n\n"+
			"If this was you, you can ignore this email.\n\n"+
			"IF THIS WASN'T YOU, click the link below immediately to secure your account:\n%s\n\n"+
			"This link will invalidate all active sessions and prompt you to reset your password.",
		actionDescription, alertURL,
	)

	return c.SendEmailActual(email, "Security Alert: "+actionDescription, htmlContent, textContent, nil)
}

// SendEmailActual is a helper to send an email using net/smtp.
// When SMTP isn't configured it falls back to (a) the local sendmail binary,
// or (b) a disk spool under LocalSpoolDir — so admin-only deployments don't
// have to stand up SMTP just to receive verification codes.
func (c *SMTPClient) SendEmailActual(toEmail, subject, htmlContent, textContent string, attachments []Attachment) error {
	from := c.config.From
	if from == "" {
		from = "noreply@localhost"
	}
	if c.config.FromName != "" {
		from = fmt.Sprintf("%s <%s>", c.config.FromName, from)
	}

	host := c.config.Host
	if host == "" {
		host = "localhost"
	}
	msg, err := buildSMTPMessage(from, toEmail, subject, textContent, htmlContent, attachments, host)
	if err != nil {
		return err
	}

	if !c.IsConfigured() {
		return deliverWithoutSMTP(toEmail, msg)
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	var auth smtp.Auth
	if strings.TrimSpace(c.config.User) != "" {
		auth = smtp.PlainAuth("", c.config.User, c.config.Password, c.config.Host)
	}

	if c.config.UseTLS && c.config.Port == 465 {
		// Implicit TLS support
		tlsConfig := &tls.Config{
			ServerName: c.config.Host,
		}
		conn, tlsErr := tls.Dial("tcp", addr, tlsConfig)
		if tlsErr != nil {
			log.Error().Err(tlsErr).Str("email", maskEmail(toEmail)).Msg("Failed to dial TLS connection")
			return fmt.Errorf("failed to dial TLS connection: %w", tlsErr)
		}
		defer conn.Close()

		client, smtpErr := smtp.NewClient(conn, c.config.Host)
		if smtpErr != nil {
			log.Error().Err(smtpErr).Str("email", maskEmail(toEmail)).Msg("Failed to create SMTP client")
			return fmt.Errorf("failed to create SMTP client: %w", smtpErr)
		}
		defer client.Close()

		if auth != nil {
			if authErr := client.Auth(auth); authErr != nil {
				log.Error().Err(authErr).Str("email", maskEmail(toEmail)).Msg("Failed to authenticate SMTP client")
				return fmt.Errorf("failed to authenticate SMTP client: %w", authErr)
			}
		}

		if mailErr := client.Mail(c.config.From); mailErr != nil {
			log.Error().Err(mailErr).Str("email", maskEmail(toEmail)).Msg("Failed to issue MAIL command")
			return fmt.Errorf("failed to issue MAIL command: %w", mailErr)
		}
		if rcptErr := client.Rcpt(toEmail); rcptErr != nil {
			log.Error().Err(rcptErr).Str("email", maskEmail(toEmail)).Msg("Failed to issue RCPT command")
			return fmt.Errorf("failed to issue RCPT command: %w", rcptErr)
		}

		w, dataErr := client.Data()
		if dataErr != nil {
			log.Error().Err(dataErr).Str("email", maskEmail(toEmail)).Msg("Failed to issue DATA command")
			return fmt.Errorf("failed to issue DATA command: %w", dataErr)
		}
		_, writeErr := w.Write(msg)
		if writeErr != nil {
			log.Error().Err(writeErr).Str("email", maskEmail(toEmail)).Msg("Failed to write email body")
			return fmt.Errorf("failed to write email body: %w", writeErr)
		}
		if closeErr := w.Close(); closeErr != nil {
			log.Error().Err(closeErr).Str("email", maskEmail(toEmail)).Msg("Failed to close DATA writer")
			return fmt.Errorf("failed to close DATA writer: %w", closeErr)
		}
		client.Quit()
	} else {
		// Use standard SendMail (handles unencrypted or STARTTLS on port 587)
		err = smtp.SendMail(addr, auth, c.config.From, []string{toEmail}, msg)
	}

	if err != nil {
		log.Error().Err(err).Str("email", maskEmail(toEmail)).Msg("Failed to send email via SMTP")
		return fmt.Errorf("failed to send email via SMTP: %w", err)
	}

	log.Debug().
		Str("email", maskEmail(toEmail)).
		Str("subject", subject).
		Msg("📧 Email sent via SMTP")
	return nil
}

func buildSMTPMessage(from, toEmail, subject, textContent, htmlContent string, attachments []Attachment, host string) ([]byte, error) {
	messageID := fmt.Sprintf("<%x@%s>", time.Now().UnixNano(), host)
	dateStr := time.Now().Format(time.RFC1123Z)
	altBoundary := "wantastic-alt-" + fmt.Sprintf("%x", time.Now().UnixNano())

	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	body.WriteString(fmt.Sprintf("Date: %s\r\n", dateStr))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString(fmt.Sprintf("From: %s\r\n", from))
	body.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))

	if len(attachments) == 0 {
		body.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n", altBoundary))
		body.WriteString("\r\n")
		writeAlternativeBody(&body, altBoundary, textContent, htmlContent)
		return body.Bytes(), nil
	}

	mixedBoundary := "wantastic-mixed-" + fmt.Sprintf("%x", time.Now().UnixNano())
	body.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n", mixedBoundary))
	body.WriteString("\r\n")
	body.WriteString(fmt.Sprintf("--%s\r\n", mixedBoundary))
	body.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n\r\n", altBoundary))
	writeAlternativeBody(&body, altBoundary, textContent, htmlContent)

	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Filename) == "" || len(attachment.Data) == 0 {
			continue
		}
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		body.WriteString(fmt.Sprintf("--%s\r\n", mixedBoundary))
		body.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", contentType, attachment.Filename))
		body.WriteString("Content-Transfer-Encoding: base64\r\n")
		body.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", attachment.Filename))
		body.WriteString(wrapBase64(attachment.Data))
		body.WriteString("\r\n")
	}

	body.WriteString(fmt.Sprintf("--%s--\r\n", mixedBoundary))
	return body.Bytes(), nil
}

func writeAlternativeBody(body *bytes.Buffer, boundary, textContent, htmlContent string) {
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
	body.WriteString(textContent)
	body.WriteString("\r\n")
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	body.WriteString(htmlContent)
	body.WriteString("\r\n")
	body.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
}

func wrapBase64(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	if encoded == "" {
		return ""
	}

	var wrapped strings.Builder
	for len(encoded) > 76 {
		wrapped.WriteString(encoded[:76])
		wrapped.WriteString("\r\n")
		encoded = encoded[76:]
	}
	wrapped.WriteString(encoded)
	return wrapped.String()
}

// SMTPService wraps SMTPClient with token management, acting as the primary interface.
type SMTPService struct {
	client *SMTPClient

	verificationTokens map[string]*verificationToken
	tokenMu            sync.RWMutex
}

// NewSMTPService creates a new SMTPService.
func NewSMTPService(cfg SMTPConfig) *SMTPService {
	return &SMTPService{
		client:             NewSMTPClient(cfg),
		verificationTokens: make(map[string]*verificationToken),
	}
}

func (s *SMTPService) SendVerificationCode(email string) error {
	return s.client.SendVerificationEmail(email)
}

func (s *SMTPService) SendLoginCode(email, code string) error {
	return s.client.SendLoginCode(email, code)
}

func (s *SMTPService) SendPasswordResetCode(email, code string) error {
	return s.client.SendPasswordResetCode(email, code)
}

func (s *SMTPService) SendEmail(toEmail, subject, htmlContent, textContent string) error {
	return s.client.SendEmail(toEmail, subject, htmlContent, textContent)
}

func (s *SMTPService) SendMigrationInvitation(invite MigrationInvitation) error {
	return s.client.SendMigrationInvitation(invite)
}

func (s *SMTPService) SendInactivityWarning(email, fullName string, daysInactive int) error {
	return s.client.SendInactivityWarning(email, fullName, daysInactive)
}

func (s *SMTPService) SendAccountDeleted(email, fullName string) error {
	return s.client.SendAccountDeleted(email, fullName)
}

func (s *SMTPService) SendPeerOfflineNotification(email, fullName string, peers []EmailPeerInfo, unsubscribeURL string) error {
	return s.client.SendPeerOfflineNotification(email, fullName, peers, unsubscribeURL)
}

func (s *SMTPService) VerifyCode(email, code string) (bool, error) {
	return s.client.CheckVerification(email, code)
}

func (s *SMTPService) IsConfigured() bool {
	return s.client.IsConfigured()
}

func (s *SMTPService) GenerateVerificationToken(email string) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := fmt.Sprintf("%x", b)

	s.verificationTokens[email] = &verificationToken{
		Email:     email,
		Token:     token,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	return token, nil
}

func (s *SMTPService) ValidateVerificationToken(email, token string) (bool, error) {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()

	vt, exists := s.verificationTokens[email]
	if !exists {
		return false, nil
	}
	if time.Now().After(vt.ExpiresAt) {
		delete(s.verificationTokens, email)
		return false, nil
	}
	if vt.Token != token {
		return false, nil
	}
	return true, nil
}

func (s *SMTPService) ValidateVerificationTokenByToken(token string) (string, bool, error) {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()

	for email, vt := range s.verificationTokens {
		if vt.Token == token {
			if time.Now().After(vt.ExpiresAt) {
				delete(s.verificationTokens, email)
				return "", false, nil
			}
			return email, true, nil
		}
	}
	return "", false, nil
}

func (s *SMTPService) ConsumeVerificationToken(email, token string) (bool, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	vt, exists := s.verificationTokens[email]
	if !exists {
		return false, nil
	}
	if time.Now().After(vt.ExpiresAt) {
		delete(s.verificationTokens, email)
		return false, nil
	}
	if vt.Token != token {
		return false, nil
	}

	delete(s.verificationTokens, email)
	return true, nil
}

func (s *SMTPService) CleanupExpiredTokens() {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	now := time.Now()
	for email, vt := range s.verificationTokens {
		if now.After(vt.ExpiresAt) {
			delete(s.verificationTokens, email)
		}
	}
}

func (s *SMTPService) SendSecurityAlert(email, token, actionDescription string) error {
	return s.client.SendSecurityAlert(email, token, actionDescription)
}

// --- Types and Helpers migrated from deleted brevo.go ---

type pendingVerification struct {
	Email     string
	Code      string
	ExpiresAt time.Time
	Attempts  int
}

type verificationToken struct {
	Email     string
	Token     string
	ExpiresAt time.Time
}

// MigrationInvitation contains the details for a migration invitation email.
type MigrationInvitation struct {
	ToEmail       string   // Recipient email
	OwnerName     string   // Name of the account owner sending migration
	OwnerEmail    string   // Email of the account owner
	DeviceNames   []string // Names of devices being migrated
	DeviceCount   int      // Number of devices
	InviteToken   string   // Token for accepting the migration
	BaseURL       string   // Base URL for the accept link
	ExpiresInDays int      // Number of days until expiration
}

// generateVerificationCode generates a 6-digit verification code.
func generateVerificationCode() (string, error) {
	max := big.NewInt(999999)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func renderToString(ctx context.Context, component templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// maskEmail masks an email for logging.
func maskEmail(email string) string {
	if len(email) < 5 {
		return "***"
	}
	atIdx := -1
	for i, c := range email {
		if c == '@' {
			atIdx = i
			break
		}
	}
	if atIdx < 2 {
		return "***" + email[atIdx:]
	}
	return email[:2] + "***" + email[atIdx:]
}
