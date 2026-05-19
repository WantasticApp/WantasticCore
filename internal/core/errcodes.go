package core

// Error codes returned to the frontend as gRPC status desc values.
// The frontend translates these machine-readable codes via i18n lookup.
// All codes are ALL_CAPS_UNDERSCORE so the frontend can detect them with a simple regex.

const (
	// ── Validation ────────────────────────────────────────────────────────────
	ErrEmailRequired         = "EMAIL_REQUIRED"
	ErrFullNameRequired      = "FULL_NAME_REQUIRED"
	ErrPhoneRequired         = "PHONE_REQUIRED"
	ErrPasswordTooShort      = "PASSWORD_TOO_SHORT"
	ErrDisposableEmail       = "DISPOSABLE_EMAIL"
	ErrInvalidEmailFormat    = "INVALID_EMAIL_FORMAT"
	ErrInvalidEmailDomain    = "INVALID_EMAIL_DOMAIN"
	ErrInvalidPhone          = "INVALID_PHONE"
	ErrPhoneRegionNotAllowed = "PHONE_REGION_NOT_ALLOWED"

	// ── Registration ──────────────────────────────────────────────────────────
	ErrEmailAlreadyExists   = "EMAIL_ALREADY_REGISTERED"
	ErrPhoneAlreadyExists   = "PHONE_ALREADY_REGISTERED"
	ErrRegistrationNotFound = "REGISTRATION_NOT_FOUND"
	ErrRegistrationExpired  = "REGISTRATION_EXPIRED"

	// ── Verification ──────────────────────────────────────────────────────────
	ErrVerificationSendFailed  = "VERIFICATION_SEND_FAILED"
	ErrVerificationInvalid     = "INVALID_VERIFICATION_CODE"
	ErrVerificationExpired     = "VERIFICATION_CODE_EXPIRED"
	ErrVerificationMaxAttempts = "VERIFICATION_MAX_ATTEMPTS"
	ErrTwilioTrialUnverified   = "TWILIO_TRIAL_UNVERIFIED"

	// ── Authentication ────────────────────────────────────────────────────────
	ErrInvalidCredentials = "INVALID_CREDENTIALS"
	ErrAccountDeleted     = "ACCOUNT_DELETED"
	ErrAccountSuspended   = "ACCOUNT_SUSPENDED"
	ErrSessionExpired     = "SESSION_EXPIRED"
	ErrSessionInvalid     = "SESSION_INVALID"

	// ── CAPTCHA ───────────────────────────────────────────────────────────────
	ErrCaptchaInvalid  = "CAPTCHA_INVALID"
	ErrCaptchaExpired  = "CAPTCHA_EXPIRED"
	ErrCaptchaNotFound = "CAPTCHA_NOT_FOUND"

	// ── Password Reset ────────────────────────────────────────────────────────
	ErrResetTokenInvalid  = "RESET_TOKEN_INVALID"
	ErrResetTokenExpired  = "RESET_TOKEN_EXPIRED"
	ErrResetCodeInvalid   = "RESET_CODE_INVALID"
	ErrResetCodeExpired   = "RESET_CODE_EXPIRED"
	ErrResetMaxAttempts   = "RESET_MAX_ATTEMPTS"
	ErrResetPhoneRequired = "RESET_PHONE_REQUIRED"

	// ── Rate Limiting / General ───────────────────────────────────────────────
	ErrRateLimitExceeded  = "RATE_LIMIT_EXCEEDED"
	ErrServiceUnavailable = "SERVICE_UNAVAILABLE"
	ErrInternalError      = "INTERNAL_ERROR"
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrPeerLimitReached   = "PEER_LIMIT_REACHED"
)
