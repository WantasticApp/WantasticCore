package webssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type testInteractiveAuthHandler struct {
	prompts []testPrompt
	answers []string
	banners []string
}

type testPrompt struct {
	question string
	echo     bool
}

func (h *testInteractiveAuthHandler) Prompt(question string, echo bool) (string, error) {
	h.prompts = append(h.prompts, testPrompt{question: question, echo: echo})
	if len(h.answers) == 0 {
		return "", nil
	}

	answer := h.answers[0]
	h.answers = h.answers[1:]
	return answer, nil
}

func (h *testInteractiveAuthHandler) Banner(message string) error {
	h.banners = append(h.banners, message)
	return nil
}

func TestSSHAuthStatePasswordSecretReusesStoredSecretAcrossRetries(t *testing.T) {
	handler := &testInteractiveAuthHandler{answers: []string{"fresh-secret"}}
	creds := &SSHCredentials{
		Username:    "root",
		Password:    "cached-secret",
		AuthHandler: handler,
	}

	authState := newSSHAuthState(creds)

	first, err := authState.passwordSecret()
	if err != nil {
		t.Fatalf("first passwordSecret returned error: %v", err)
	}
	if first != "cached-secret" {
		t.Fatalf("first passwordSecret = %q, want cached-secret", first)
	}

	second, err := authState.passwordSecret()
	if err != nil {
		t.Fatalf("second passwordSecret returned error: %v", err)
	}
	if second != "cached-secret" {
		t.Fatalf("second passwordSecret = %q, want cached-secret", second)
	}

	if creds.Password != "cached-secret" {
		t.Fatalf("creds.Password = %q, want cached-secret", creds.Password)
	}
	if len(handler.prompts) != 0 {
		t.Fatalf("prompt count = %d, want 0", len(handler.prompts))
	}
}

func TestSSHAuthStateKeyboardInteractivePromptsAndStoresPassword(t *testing.T) {
	handler := &testInteractiveAuthHandler{answers: []string{"typed-secret"}}
	creds := &SSHCredentials{
		Username:    "root",
		AuthHandler: handler,
	}

	authState := newSSHAuthState(creds)
	answers, err := authState.answerQuestions("", []string{"Password:"}, []bool{false})
	if err != nil {
		t.Fatalf("answerQuestions returned error: %v", err)
	}
	if len(answers) != 1 || answers[0] != "typed-secret" {
		t.Fatalf("answers = %#v, want typed-secret", answers)
	}
	if creds.Password != "typed-secret" {
		t.Fatalf("creds.Password = %q, want typed-secret", creds.Password)
	}
	if len(handler.prompts) != 1 {
		t.Fatalf("prompt count = %d, want 1", len(handler.prompts))
	}
}

func TestSSHAuthStateKeyboardInteractiveReusesStoredSecretAcrossPasswordPrompts(t *testing.T) {
	handler := &testInteractiveAuthHandler{answers: []string{"fresh-secret"}}
	creds := &SSHCredentials{
		Username:    "root",
		Password:    "cached-secret",
		AuthHandler: handler,
	}

	authState := newSSHAuthState(creds)

	firstAnswers, err := authState.answerQuestions("", []string{"Password:"}, []bool{false})
	if err != nil {
		t.Fatalf("first answerQuestions returned error: %v", err)
	}
	if len(firstAnswers) != 1 || firstAnswers[0] != "cached-secret" {
		t.Fatalf("firstAnswers = %#v, want cached-secret", firstAnswers)
	}

	secondAnswers, err := authState.answerQuestions("", []string{"Password:"}, []bool{false})
	if err != nil {
		t.Fatalf("second answerQuestions returned error: %v", err)
	}
	if len(secondAnswers) != 1 || secondAnswers[0] != "cached-secret" {
		t.Fatalf("secondAnswers = %#v, want cached-secret", secondAnswers)
	}
	if creds.Password != "cached-secret" {
		t.Fatalf("creds.Password = %q, want cached-secret", creds.Password)
	}
	if len(handler.prompts) != 0 {
		t.Fatalf("prompt count = %d, want 0", len(handler.prompts))
	}
}

func TestSSHAuthStateKeyboardInteractiveUsesStoredPasswordWithinMultiPromptChallenge(t *testing.T) {
	handler := &testInteractiveAuthHandler{answers: []string{"654321"}}
	creds := &SSHCredentials{
		Username:    "root",
		Password:    "cached-secret",
		AuthHandler: handler,
	}

	authState := newSSHAuthState(creds)
	answers, err := authState.answerQuestions(
		"",
		[]string{"Password:", "Verification code:"},
		[]bool{false, false},
	)
	if err != nil {
		t.Fatalf("answerQuestions returned error: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("answer count = %d, want 2", len(answers))
	}
	if answers[0] != "cached-secret" {
		t.Fatalf("first answer = %q, want cached-secret", answers[0])
	}
	if answers[1] != "654321" {
		t.Fatalf("second answer = %q, want 654321", answers[1])
	}
	if len(handler.prompts) != 1 {
		t.Fatalf("prompt count = %d, want 1", len(handler.prompts))
	}
	if handler.prompts[0].question != "Verification code:" {
		t.Fatalf("prompted question = %q, want verification prompt", handler.prompts[0].question)
	}
}

func TestSSHAuthStatePasswordSecretRejectsEmptyInteractiveSecret(t *testing.T) {
	handler := &testInteractiveAuthHandler{answers: []string{""}}
	creds := &SSHCredentials{
		Username:    "root",
		AuthHandler: handler,
	}

	authState := newSSHAuthState(creds)
	_, err := authState.passwordSecret()
	if err == nil {
		t.Fatal("passwordSecret unexpectedly accepted an empty interactive secret")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("passwordSecret error = %q, want empty response error", err.Error())
	}
	if len(handler.prompts) != 1 {
		t.Fatalf("prompt count = %d, want 1", len(handler.prompts))
	}
}

func TestSSHHostKeyPolicyAcceptsFirstSeenKey(t *testing.T) {
	key := testSSHPublicKey(t)
	policy := newSSHHostKeyPolicy(nil, "", "")

	if err := policy.callback("10.0.0.2:22", nil, key); err != nil {
		t.Fatalf("callback returned error: %v", err)
	}

	raw, fingerprint, algorithm := policy.acceptedMetadata()
	if len(raw) == 0 {
		t.Fatal("accepted raw host key is empty")
	}
	if fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, ssh.FingerprintSHA256(key))
	}
	if algorithm != key.Type() {
		t.Fatalf("algorithm = %q, want %q", algorithm, key.Type())
	}
}

func TestSSHHostKeyPolicyRejectsPinnedMismatch(t *testing.T) {
	expected := testSSHPublicKey(t)
	actual := testSSHPublicKey(t)
	policy := newSSHHostKeyPolicy(expected.Marshal(), ssh.FingerprintSHA256(expected), expected.Type())

	err := policy.callback("10.0.0.2:22", nil, actual)
	if err == nil {
		t.Fatal("callback unexpectedly accepted mismatched host key")
	}
	if !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("error = %q, want host key mismatch", err.Error())
	}
}

func TestBuildSSHClientConfigModernDoesNotIncludeInsecureAlgorithms(t *testing.T) {
	cfg, err := buildSSHClientConfig(&SSHCredentials{Username: "root"}, "10.0.0.2:22", SSHCompatibilityModern, newSSHHostKeyPolicy(nil, "", ""))
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	insecure := ssh.InsecureAlgorithms()

	assertNoOverlap(t, cfg.Config.KeyExchanges, insecure.KeyExchanges)
	assertNoOverlap(t, cfg.Config.Ciphers, insecure.Ciphers)
	assertNoOverlap(t, cfg.Config.MACs, insecure.MACs)
	assertNoOverlap(t, cfg.HostKeyAlgorithms, insecure.HostKeys)
}

func TestBuildSSHClientConfigLegacyIncludesFallbackAlgorithms(t *testing.T) {
	cfg, err := buildSSHClientConfig(&SSHCredentials{Username: "root"}, "10.0.0.2:22", SSHCompatibilityLegacy, newSSHHostKeyPolicy(nil, "", ""))
	if err != nil {
		t.Fatalf("buildSSHClientConfig returned error: %v", err)
	}
	supported := ssh.SupportedAlgorithms()
	insecure := ssh.InsecureAlgorithms()

	assertContainsAll(t, cfg.Config.KeyExchanges, supported.KeyExchanges)
	assertContainsAll(t, cfg.Config.Ciphers, supported.Ciphers)
	assertContainsAll(t, cfg.Config.MACs, supported.MACs)
	assertContainsAll(t, cfg.HostKeyAlgorithms, supported.HostKeys)

	hasLegacyFallback := overlaps(cfg.Config.KeyExchanges, insecure.KeyExchanges) ||
		overlaps(cfg.Config.Ciphers, insecure.Ciphers) ||
		overlaps(cfg.Config.MACs, insecure.MACs) ||
		overlaps(cfg.HostKeyAlgorithms, insecure.HostKeys)
	if len(insecure.KeyExchanges)+len(insecure.Ciphers)+len(insecure.MACs)+len(insecure.HostKeys) > 0 && !hasLegacyFallback {
		t.Fatal("legacy config did not include any insecure fallback algorithms")
	}
}

func TestShouldRetryWithLegacyOnlyForNegotiationFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "negotiation failure",
			err:  fmt.Errorf("ssh: no common algorithm for key exchange; client offered: curve25519-sha256, server offered: diffie-hellman-group14-sha1"),
			want: true,
		},
		{
			name: "auth failure",
			err:  fmt.Errorf("ssh: unable to authenticate, attempted methods [none password], no supported methods remain"),
			want: false,
		},
		{
			name: "host key mismatch",
			err:  fmt.Errorf("ssh: host key mismatch for 10.0.0.2:22"),
			want: false,
		},
		{
			name: "other failure",
			err:  fmt.Errorf("unexpected EOF"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryWithLegacy(tt.err); got != tt.want {
				t.Fatalf("shouldRetryWithLegacy(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWaitForPeerEndpointReturnsWhenEndpointAppears(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	readyAt := time.Now().Add(150 * time.Millisecond)
	err := waitForPeerEndpoint(ctx, "10.0.0.2", func() bool {
		return time.Now().After(readyAt)
	})
	if err != nil {
		t.Fatalf("waitForPeerEndpoint returned error: %v", err)
	}
}

func TestWaitForPeerEndpointReturnsTunnelUnavailableError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := waitForPeerEndpoint(ctx, "10.0.0.2", func() bool { return false })
	if err == nil {
		t.Fatal("waitForPeerEndpoint unexpectedly succeeded")
	}
	if !errors.Is(err, ErrSSHTunnelUnavailable) {
		t.Fatalf("waitForPeerEndpoint error = %v, want ErrSSHTunnelUnavailable", err)
	}
	if !strings.Contains(err.Error(), "10.0.0.2") {
		t.Fatalf("waitForPeerEndpoint error = %q, want peer IP context", err.Error())
	}
}

func testSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey returned error: %v", err)
	}

	return signer.PublicKey()
}

func assertNoOverlap(t *testing.T, got, disallowed []string) {
	t.Helper()

	if overlap := firstOverlap(got, disallowed); overlap != "" {
		t.Fatalf("unexpected insecure algorithm %q found in %#v", overlap, got)
	}
}

func assertContainsAll(t *testing.T, got, want []string) {
	t.Helper()

	for _, expected := range want {
		if !contains(got, expected) {
			t.Fatalf("missing algorithm %q from %#v", expected, got)
		}
	}
}

func overlaps(a, b []string) bool {
	return firstOverlap(a, b) != ""
}

func firstOverlap(a, b []string) string {
	for _, left := range a {
		if contains(b, left) {
			return left
		}
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
