// Package hooks provides HTTP handlers for notification webhook endpoints.
// This file implements the unsubscribe link handler for disabling peer notifications.
package hooks

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/crypto"
	core "WantasticCore/internal/core"
	"io"

	"github.com/rs/zerolog/log"
)

// Handler handles notification hook requests. After the in-process gRPC
// rip, the handler calls service implementations directly via the bundle
// supplied to SetServices — no client/conn indirection.
type Handler struct {
	services   HookServices
	hookCipher *crypto.NotificationHookCipher
	portalHost string // Host for console redirects (e.g., "console.wantastic.app")
}

// HookServices is the subset of service interfaces the hook handlers
// call. Defined here (instead of importing the portal's package) so the
// hooks package stays import-cycle-free.
type HookServices struct {
	TenantPortal       core.TenantPortalService
	TenantRegistration core.TenantRegistrationService
	WUSP               core.WUSPServiceHandler
}

// NewHandler creates a new hooks handler with the given secret key.
// secretKeyHex should be a hex-encoded string (at least 32 chars = 16 bytes).
//
// If secretKeyHex is empty, the handler is created with a NIL cipher. In that
// mode the unsubscribe/invite hooks (which require the cipher to decode the
// payload-bearing token) return 503, while the backup/stripe/sms hooks (which
// validate via Redis tokens or shared secrets, not the cipher) keep working.
// This lets dev environments enable the backup endpoint without configuring a
// notification secret.
func NewHandler(secretKeyHex string) (*Handler, error) {
	h := &Handler{portalHost: "console.wantastic.app"}

	if secretKeyHex != "" {
		secretKey, err := hex.DecodeString(secretKeyHex)
		if err != nil {
			return nil, fmt.Errorf("invalid hooks secret key (must be hex): %w", err)
		}
		cipher, err := crypto.NewNotificationHookCipher(secretKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create hook cipher: %w", err)
		}
		h.hookCipher = cipher
	}

	return h, nil
}

// SetServices wires the in-process service bundle used by the hook
// handlers. Callers (cmd/wantastic-core) supply this once at startup
// after the core's service registry is built.
func (h *Handler) SetServices(svc HookServices) {
	h.services = svc
}

// SetPortalHost sets the portal host for redirects.
func (h *Handler) SetPortalHost(host string) {
	h.portalHost = host
}

// GetHookCipher returns the hook cipher for generating tokens.
func (h *Handler) GetHookCipher() *crypto.NotificationHookCipher {
	return h.hookCipher
}

// ServeHTTP handles hook requests at /hooks/*
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract the hook type from the path
	// Expected: /hooks/unsubscribe/{token}
	path := strings.TrimPrefix(r.URL.Path, "/hooks/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 1 {
		h.renderError(w, "Invalid hook path", http.StatusBadRequest)
		return
	}

	hookType := parts[0]

	switch hookType {
	case "unsubscribe":
		if h.hookCipher == nil {
			h.renderError(w, "Notification hooks not configured on this server", http.StatusServiceUnavailable)
			return
		}
		if len(parts) < 2 || parts[1] == "" {
			h.renderError(w, "Missing token", http.StatusBadRequest)
			return
		}
		h.handleUnsubscribe(w, r, parts[1])
	case "stripe", "stripe-in":
		h.HandleStripeWebhook(w, r)
	case "backup":
		// Backup uploads validate peer-scoped backup tokens via gRPC, not via the
		// notification cipher — they work without WANTASTIC_HOOKS_SECRET.
		h.HandleBackupUpload(w, r)
	default:
		h.renderError(w, "Unknown hook type", http.StatusNotFound)
	}
}

// handleUnsubscribe processes an unsubscribe request from a notification email.
// GET: Shows a confirmation page
// POST: Actually disables the notification
func (h *Handler) handleUnsubscribe(w http.ResponseWriter, r *http.Request, token string) {
	// Validate and decrypt the token
	payload, err := h.hookCipher.ValidateToken(token)
	if err != nil {
		log.Warn().
			Err(err).
			Str("token_preview", truncateToken(token)).
			Msg(" Invalid unsubscribe token")

		switch err {
		case crypto.ErrHookTokenExpired:
			h.renderError(w, "This unsubscribe link has expired. Please log in to your dashboard to manage notification settings.", http.StatusGone)
		default:
			h.renderError(w, "Invalid or corrupted unsubscribe link. Please log in to your dashboard to manage notification settings.", http.StatusBadRequest)
		}
		return
	}

	log.Debug().
		Str("tenant_id", payload.TenantID).
		Str("tenant_name", payload.TenantName).
		Msg(" Processing unsubscribe request")

	if r.Method == http.MethodGet {
		// Show confirmation page
		h.renderUnsubscribeConfirmation(w, payload, token)
		return
	}

	if r.Method == http.MethodPost {
		// Process the unsubscribe
		h.processUnsubscribe(w, r, payload)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// processUnsubscribe disables notifications for all peers of the tenant via gRPC.
func (h *Handler) processUnsubscribe(w http.ResponseWriter, r *http.Request, payload *crypto.NotificationHookPayload) {
	if h.services.TenantPortal == nil {
		log.Error().Msg("TenantPortal service not wired for hooks handler")
		h.renderError(w, "Service temporarily unavailable. Please try again later.", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.services.TenantPortal.DisableAllPeerNotifications(ctx, &pb.DisableAllPeerNotificationsRequest{
		TenantId: payload.TenantID,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("tenant_id", payload.TenantID).
			Msg("❌ Failed to disable all peer notifications via gRPC")
		h.renderError(w, "Failed to update notification settings. Please try again or log in to your dashboard.", http.StatusInternalServerError)
		return
	}

	if !resp.Success {
		h.renderError(w, "Failed to update notification settings: "+resp.Message, http.StatusInternalServerError)
		return
	}

	log.Debug().
		Str("tenant_id", payload.TenantID).
		Str("tenant_name", payload.TenantName).
		Int32("disabled_count", resp.DisabledCount).
		Msg(" All peer notifications disabled via unsubscribe link")

	// Render success page
	h.renderUnsubscribeSuccess(w, payload, resp.DisabledCount)
}

// renderUnsubscribeConfirmation shows a confirmation page before unsubscribing.
func (h *Handler) renderUnsubscribeConfirmation(w http.ResponseWriter, payload *crypto.NotificationHookPayload, token string) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Unsubscribe from Alerts - Wantastic</title>
    <style lang="scss">
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .card {
            background: white;
            border-radius: 16px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
            max-width: 450px;
            width: 100%%;
            padding: 40px;
            text-align: center;
        }
        .icon { font-size: 48px; margin-bottom: 20px; }
        h1 { color: #374151; font-size: 24px; margin-bottom: 16px; }
        .account-name {
            background: #f3f4f6;
            border-radius: 8px;
            padding: 12px 16px;
            margin: 20px 0;
            font-weight: 600;
            color: #4b5563;
        }
        p { color: #6b7280; line-height: 1.6; margin-bottom: 16px; }
        .warning {
            background: #fef3c7;
            border: 1px solid #f59e0b;
            border-radius: 8px;
            padding: 12px 16px;
            margin: 20px 0;
            color: #92400e;
            font-size: 14px;
        }
        .buttons { display: flex; gap: 12px; margin-top: 24px; }
        .btn {
            flex: 1;
            padding: 14px 24px;
            border-radius: 8px;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            border: none;
            transition: all 0.2s;
        }
        .btn-primary {
            background: linear-gradient(135deg, #ef4444 0%%, #dc2626 100%%);
            color: white;
        }
        .btn-primary:hover { opacity: 0.9; transform: translateY(-1px); }
        .btn-secondary {
            background: #f3f4f6;
            color: #374151;
        }
        .btn-secondary:hover { background: #e5e7eb; }
        .footer { margin-top: 24px; font-size: 14px; color: #9ca3af; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">🔕</div>
        <h1>Disable All Device Alerts?</h1>
        <p>You're about to turn off offline notifications for all devices in:</p>
        <div class="account-name">%s's Network</div>
        <div class="warning"> This will disable alerts for ALL your devices. You will no longer receive emails when any device goes offline.</div>
        <form method="POST" action="/hooks/unsubscribe/%s">
            <div class="buttons">
                <a href="https://%s" class="btn btn-secondary">Cancel</a>
                <button type="submit" class="btn btn-primary">Disable All Alerts</button>
            </div>
        </form>
        <p class="footer">You can re-enable alerts for individual devices from your dashboard.</p>
    </div>
</body>
</html>`, payload.TenantName, token, h.portalHost)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// renderUnsubscribeSuccess shows a success page after unsubscribing.
func (h *Handler) renderUnsubscribeSuccess(w http.ResponseWriter, payload *crypto.NotificationHookPayload, disabledCount int32) {
	countText := "all devices"
	if disabledCount == 1 {
		countText = "1 device"
	} else if disabledCount > 1 {
		countText = fmt.Sprintf("%d devices", disabledCount)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Alerts Disabled - Wantastic</title>
    <style lang="scss">
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .card {
            background: white;
            border-radius: 16px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
            max-width: 450px;
            width: 100%%;
            padding: 40px;
            text-align: center;
        }
        .icon { font-size: 48px; margin-bottom: 20px; }
        h1 { color: #374151; font-size: 24px; margin-bottom: 16px; }
        .count-badge {
            background: #d1fae5;
            color: #065f46;
            border-radius: 8px;
            padding: 12px 16px;
            margin: 20px 0;
            font-weight: 600;
        }
        p { color: #6b7280; line-height: 1.6; margin-bottom: 16px; }
        .btn {
            display: inline-block;
            padding: 14px 32px;
            border-radius: 8px;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            color: white;
            margin-top: 16px;
            transition: all 0.2s;
        }
        .btn:hover { opacity: 0.9; transform: translateY(-1px); }
        .footer { margin-top: 24px; font-size: 14px; color: #9ca3af; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon"></div>
        <h1>Alerts Disabled</h1>
        <p>Offline notifications have been disabled for:</p>
        <div class="count-badge">%s in %s's Network</div>
        <p>You will no longer receive emails when devices go offline.</p>
        <a href="https://%s" class="btn">Go to Dashboard</a>
        <p class="footer">You can re-enable alerts for individual devices from your dashboard.</p>
    </div>
</body>
</html>`, countText, payload.TenantName, h.portalHost)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// renderError shows an error page matching the portal's dark design system.
func (h *Handler) renderError(w http.ResponseWriter, message string, statusCode int) {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Error - Wantastic</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Segoe UI Variable', 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
            background: #0f1117;
            color: #e2e8f0;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .card {
            background: rgba(26, 29, 39, 0.85);
            backdrop-filter: blur(24px);
            border: 1px solid rgba(255,255,255,0.08);
            border-radius: 10px;
            max-width: 400px;
            width: 100%%;
            padding: 2.5rem 2rem;
            text-align: center;
        }
        .icon { font-size: 32px; margin-bottom: 16px; opacity: 0.5; }
        h1 { font-size: 18px; font-weight: 700; margin-bottom: 12px; color: #f1f5f9; }
        p { color: rgba(226,232,240,0.55); font-size: 14px; line-height: 1.6; margin-bottom: 20px; }
        .btn {
            display: inline-block;
            padding: 10px 24px;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 600;
            text-decoration: none;
            background: rgba(11, 71, 169, 0.85);
            color: white;
            transition: opacity 0.15s;
        }
        .btn:hover { opacity: 0.88; }
    </style>
</head>
<body>
    <div class="card">
        <div class="icon">&#x26A0;</div>
        <h1>Something went wrong</h1>
        <p>%s</p>
        <a href="https://%s" class="btn">Go to Dashboard</a>
    </div>
</body>
</html>`, message, h.portalHost)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write([]byte(html))
}

// truncateToken returns a preview of the token for logging (first 16 chars).
func truncateToken(token string) string {
	if len(token) <= 16 {
		return token
	}
	return token[:16] + "..."
}

// GenerateUnsubscribeURL creates an unsubscribe URL for a tenant.
// This disables all peer notifications for the tenant when clicked.
func (h *Handler) GenerateUnsubscribeURL(baseURL, tenantID, tenantName string) (string, error) {
	token, err := h.hookCipher.GenerateToken(tenantID, tenantName)
	if err != nil {
		return "", fmt.Errorf("failed to generate unsubscribe token: %w", err)
	}
	return fmt.Sprintf("%s/unsubscribe/%s", strings.TrimSuffix(baseURL, "/"), token), nil
}

// HandleStripeWebhook forwards Stripe webhooks to the core service via gRPC.
func (h *Handler) HandleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if h.services.TenantRegistration == nil {
		log.Error().Msg("TenantRegistration service not wired for hooks handler")
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Read body
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read Stripe webhook body")
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Get signature
	signature := r.Header.Get("Stripe-Signature")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.services.TenantRegistration.ProcessStripeWebhook(ctx, &pb.ProcessStripeWebhookRequest{
		Body:      body,
		Signature: signature,
	})

	if err != nil {
		log.Error().Err(err).Msg("Failed to process Stripe webhook via gRPC")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if !resp.Success {
		log.Warn().Str("message", resp.Message).Msg("Stripe webhook processing failed")
		http.Error(w, resp.Message, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// HandleSMSStatusWebhook used to forward Twilio SMS status webhooks via gRPC.
// Phase 3: Twilio/SMS removed; the /hooks/sms-status route has been retired.
