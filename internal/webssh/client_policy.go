package webssh

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

type SSHCompatibilityMode string

const (
	SSHCompatibilityUnknown SSHCompatibilityMode = ""
	SSHCompatibilityModern  SSHCompatibilityMode = "modern"
	SSHCompatibilityLegacy  SSHCompatibilityMode = "legacy"
)

const (
	sshConnectTimeout          = 20 * time.Second
	sshMaxKeyboardChallenges   = 4
	sshMaxChallengeQuestions   = 4
	sshMaxBannerChars          = 4096
	sshMaxPromptChars          = 512
	sshLegacyRetryLogReasonMax = 512
)

type sshAuthState struct {
	creds *SSHCredentials

	mu                 sync.Mutex
	storedPassword     string
	promptedPassword   string
	passwordAuthCalls  int
	keyboardChallenges int
}

func newSSHAuthState(creds *SSHCredentials) *sshAuthState {
	return &sshAuthState{
		creds:          creds,
		storedPassword: creds.Password,
	}
}

func (a *sshAuthState) passwordSecret() (string, error) {
	a.mu.Lock()
	a.passwordAuthCalls++
	promptedPassword := a.promptedPassword
	storedPassword := a.storedPassword
	a.mu.Unlock()

	if promptedPassword != "" {
		return promptedPassword, nil
	}

	if storedPassword != "" {
		return storedPassword, nil
	}

	if a.creds.AuthHandler != nil {
		return a.prompt("Password: ", false, true)
	}

	return storedPassword, nil
}

func (a *sshAuthState) answerQuestions(instruction string, questions []string, echos []bool) ([]string, error) {
	if strings.TrimSpace(instruction) != "" {
		if err := a.sendBanner(instruction); err != nil {
			return nil, err
		}
	}

	a.mu.Lock()
	a.keyboardChallenges++
	challengeCount := a.keyboardChallenges
	promptedPassword := a.promptedPassword
	storedPassword := a.storedPassword
	a.mu.Unlock()

	if challengeCount > sshMaxKeyboardChallenges {
		return nil, fmt.Errorf("ssh: too many keyboard-interactive authentication challenges")
	}
	if len(questions) > sshMaxChallengeQuestions {
		return nil, fmt.Errorf("ssh: too many keyboard-interactive authentication prompts")
	}

	answers := make([]string, len(questions))
	for i, question := range questions {
		echo := false
		if i < len(echos) {
			echo = echos[i]
		}

		switch {
		case !echo && promptedPassword != "" && looksLikePasswordPrompt(question):
			answers[i] = promptedPassword
			continue
		case !echo && storedPassword != "" && looksLikePasswordPrompt(question):
			answers[i] = storedPassword
			continue
		case !echo && a.creds.AuthHandler == nil && storedPassword != "":
			answers[i] = storedPassword
			continue
		}

		answer, err := a.prompt(question, echo, looksLikePasswordPrompt(question))
		if err != nil {
			return nil, err
		}
		answers[i] = answer
	}

	return answers, nil
}

func (a *sshAuthState) prompt(question string, echo bool, rememberSecret bool) (string, error) {
	if a.creds.AuthHandler == nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !echo {
			if a.promptedPassword != "" {
				return a.promptedPassword, nil
			}
			return a.storedPassword, nil
		}
		return "", nil
	}

	answer, err := a.creds.AuthHandler.Prompt(clampSSHText(question, sshMaxPromptChars), echo)
	if err != nil {
		return "", err
	}
	if !echo && rememberSecret && answer == "" {
		return "", fmt.Errorf("ssh: empty response for interactive secret prompt")
	}

	if !echo && rememberSecret && answer != "" {
		a.mu.Lock()
		a.promptedPassword = answer
		a.storedPassword = answer
		a.creds.Password = answer
		a.mu.Unlock()
	}

	return answer, nil
}

func (a *sshAuthState) sendBanner(message string) error {
	if a.creds.AuthHandler == nil {
		return nil
	}

	trimmed := clampSSHText(message, sshMaxBannerChars)
	if trimmed == "" {
		return nil
	}

	return a.creds.AuthHandler.Banner(trimmed)
}

type sshHostKeyPolicy struct {
	mu sync.Mutex

	expectedRaw         []byte
	expectedFingerprint string
	expectedAlgorithm   string

	acceptedRaw         []byte
	acceptedFingerprint string
	acceptedAlgorithm   string
}

func newSSHHostKeyPolicy(expectedRaw []byte, expectedFingerprint, expectedAlgorithm string) *sshHostKeyPolicy {
	policy := &sshHostKeyPolicy{
		expectedFingerprint: strings.TrimSpace(expectedFingerprint),
		expectedAlgorithm:   strings.TrimSpace(expectedAlgorithm),
	}
	if len(expectedRaw) > 0 {
		policy.expectedRaw = append([]byte(nil), expectedRaw...)
	}
	return policy
}

func (p *sshHostKeyPolicy) callback(hostname string, _ net.Addr, key ssh.PublicKey) error {
	raw := key.Marshal()
	fingerprint := ssh.FingerprintSHA256(key)
	algorithm := key.Type()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.acceptedRaw = append([]byte(nil), raw...)
	p.acceptedFingerprint = fingerprint
	p.acceptedAlgorithm = algorithm

	if len(p.expectedRaw) > 0 {
		if !bytes.Equal(raw, p.expectedRaw) {
			return fmt.Errorf(
				"ssh: host key mismatch for %s (expected %s %s, got %s %s)",
				hostname,
				defaultSSHText(p.expectedAlgorithm, "unknown"),
				defaultSSHText(p.expectedFingerprint, "unknown"),
				algorithm,
				fingerprint,
			)
		}
		return nil
	}

	if p.expectedFingerprint != "" && p.expectedFingerprint != fingerprint {
		return fmt.Errorf(
			"ssh: host key fingerprint mismatch for %s (expected %s, got %s)",
			hostname,
			p.expectedFingerprint,
			fingerprint,
		)
	}

	return nil
}

func (p *sshHostKeyPolicy) acceptedMetadata() ([]byte, string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var raw []byte
	if len(p.acceptedRaw) > 0 {
		raw = append([]byte(nil), p.acceptedRaw...)
	}
	return raw, p.acceptedFingerprint, p.acceptedAlgorithm
}

func buildSSHClientConfig(creds *SSHCredentials, target string, mode SSHCompatibilityMode, hostKeyPolicy *sshHostKeyPolicy) (*ssh.ClientConfig, error) {
	authState := newSSHAuthState(creds)
	algorithms := sshAlgorithmsForMode(mode)
	authMethods := make([]ssh.AuthMethod, 0, 3)

	if publicKeyAuth, err := buildPublicKeyAuthMethod(creds); err != nil {
		return nil, err
	} else if publicKeyAuth != nil {
		authMethods = append(authMethods, publicKeyAuth)
	}

	authMethods = append(authMethods,
		ssh.RetryableAuthMethod(ssh.PasswordCallback(authState.passwordSecret), 3),
		ssh.RetryableAuthMethod(ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			return authState.answerQuestions(instruction, questions, echos)
		}), 3),
	)

	return &ssh.ClientConfig{
		User:            creds.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyPolicy.callback,
		Timeout:         sshConnectTimeout,
		BannerCallback: func(message string) error {
			trimmed := clampSSHText(message, sshMaxBannerChars)
			if trimmed != "" {
				log.Debug().
					Str("target", target).
					Str("compatibility_mode", string(normalizeSSHCompatibilityMode(mode))).
					Str("banner", trimmed).
					Msg("Captured SSH banner")
			}
			return authState.sendBanner(message)
		},
		Config: ssh.Config{
			Ciphers:      algorithms.Ciphers,
			KeyExchanges: algorithms.KeyExchanges,
			MACs:         algorithms.MACs,
		},
		HostKeyAlgorithms: algorithms.HostKeys,
	}, nil
}

func buildPublicKeyAuthMethod(creds *SSHCredentials) (ssh.AuthMethod, error) {
	privateKey := strings.TrimSpace(creds.PrivateKey)
	if privateKey == "" {
		return nil, nil
	}

	signer, err := parsePrivateKeySigner([]byte(privateKey), creds.PrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}

	return ssh.PublicKeys(signer), nil
}

func parsePrivateKeySigner(privateKey []byte, passphrase string) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err == nil {
		return signer, nil
	}

	if passphrase == "" {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			return nil, fmt.Errorf("ssh: private key requires a passphrase")
		}
		return nil, fmt.Errorf("ssh: failed to parse private key: %w", err)
	}

	signer, err = ssh.ParsePrivateKeyWithPassphrase(privateKey, []byte(passphrase))
	if err == nil {
		return signer, nil
	}
	return nil, fmt.Errorf("ssh: failed to parse encrypted private key: %w", err)
}

func sshAlgorithmsForMode(mode SSHCompatibilityMode) ssh.Algorithms {
	supported := ssh.SupportedAlgorithms()
	if normalizeSSHCompatibilityMode(mode) != SSHCompatibilityLegacy {
		return supported
	}

	insecure := ssh.InsecureAlgorithms()
	return ssh.Algorithms{
		KeyExchanges:   mergeSSHAlgorithms(supported.KeyExchanges, insecure.KeyExchanges),
		Ciphers:        mergeSSHAlgorithms(supported.Ciphers, insecure.Ciphers),
		MACs:           mergeSSHAlgorithms(supported.MACs, insecure.MACs),
		HostKeys:       mergeSSHAlgorithms(supported.HostKeys, insecure.HostKeys),
		PublicKeyAuths: mergeSSHAlgorithms(supported.PublicKeyAuths, insecure.PublicKeyAuths),
	}
}

func mergeSSHAlgorithms(preferred, fallback []string) []string {
	seen := make(map[string]struct{}, len(preferred)+len(fallback))
	merged := make([]string, 0, len(preferred)+len(fallback))

	for _, list := range [][]string{preferred, fallback} {
		for _, value := range list {
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}

	return merged
}

func normalizeSSHCompatibilityMode(mode SSHCompatibilityMode) SSHCompatibilityMode {
	if mode == SSHCompatibilityLegacy {
		return SSHCompatibilityLegacy
	}
	return SSHCompatibilityModern
}

func shouldRetryWithLegacy(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unable to authenticate") || strings.Contains(message, "host key mismatch") {
		return false
	}

	return strings.Contains(message, "ssh: no common algorithm for")
}

// isSSHHostKeyMismatch returns true when the handshake failed because the
// server's current host key does not match the one we have stored.  This
// happens after a server reinstall or key rotation.  The caller should clear
// its cached key, warn the user, and retry with TOFU.
func isSSHHostKeyMismatch(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "host key mismatch")
}

func looksLikePasswordPrompt(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return true
	}

	return strings.Contains(q, "password") ||
		strings.Contains(q, "passphrase") ||
		strings.Contains(q, "pass phrase")
}

func clampSSHText(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || maxRunes <= 0 {
		return trimmed
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}

	return string(runes[:maxRunes]) + "..."
}

func defaultSSHText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
