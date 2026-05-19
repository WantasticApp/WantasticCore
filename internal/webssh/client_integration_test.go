package webssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type sshTestServerOptions struct {
	username            string
	password            string
	allowNoClientAuth   bool
	keyboardInteractive bool
	authorizedPublicKey ssh.PublicKey
	keyExchanges        []string
	ciphers             []string
	macs                []string
}

type sshTestServer struct {
	listener net.Listener
	addr     string
	config   *ssh.ServerConfig
	wg       sync.WaitGroup
}

func TestSSHClientIntegrationPasswordAuth(t *testing.T) {
	server := newSSHTestServer(t, sshTestServerOptions{
		username: "root",
		password: "s3cret",
	})

	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username: "root",
		Password: "s3cret",
	}, SSHCompatibilityModern)
	defer mux.Close()

	assertSessionPrompt(t, mux, "password")
}

func TestSSHClientIntegrationKeyboardInteractivePromptAuth(t *testing.T) {
	server := newSSHTestServer(t, sshTestServerOptions{
		username:            "root",
		password:            "typed-secret",
		keyboardInteractive: true,
	})

	handler := &testInteractiveAuthHandler{answers: []string{"typed-secret"}}
	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username:    "root",
		AuthHandler: handler,
	}, SSHCompatibilityModern)
	defer mux.Close()

	if len(handler.prompts) == 0 {
		t.Fatal("expected keyboard-interactive auth prompt")
	}
	assertSessionPrompt(t, mux, "keyboard")
}

func TestSSHClientIntegrationNoClientAuth(t *testing.T) {
	server := newSSHTestServer(t, sshTestServerOptions{
		username:          "root",
		allowNoClientAuth: true,
	})

	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username: "root",
	}, SSHCompatibilityModern)
	defer mux.Close()

	assertSessionPrompt(t, mux, "noauth")
}

func TestSSHClientIntegrationPublicKeyAuth(t *testing.T) {
	privateKeyPEM, publicKey := generateRSAPrivateKeyPEM(t)
	server := newSSHTestServer(t, sshTestServerOptions{
		username:            "root",
		authorizedPublicKey: publicKey,
	})

	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username:   "root",
		PrivateKey: string(privateKeyPEM),
	}, SSHCompatibilityModern)
	defer mux.Close()

	assertSessionPrompt(t, mux, "pubkey")
}

func TestSSHClientIntegrationEncryptedPublicKeyAuth(t *testing.T) {
	privateKeyPEM, publicKey := generateEncryptedRSAPrivateKeyPEM(t, "test-passphrase")
	server := newSSHTestServer(t, sshTestServerOptions{
		username:            "root",
		authorizedPublicKey: publicKey,
	})

	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username:             "root",
		PrivateKey:           string(privateKeyPEM),
		PrivateKeyPassphrase: "test-passphrase",
	}, SSHCompatibilityModern)
	defer mux.Close()

	assertSessionPrompt(t, mux, "encrypted-pubkey")
}

func TestSSHClientIntegrationLegacyServerRequiresLegacyCompatibility(t *testing.T) {
	server := newSSHTestServer(t, sshTestServerOptions{
		username:     "root",
		password:     "legacy-secret",
		keyExchanges: []string{"diffie-hellman-group1-sha1"},
		ciphers:      []string{"aes128-cbc"},
		macs:         []string{"hmac-sha1"},
	})

	_, err := dialSSHMultiplexerForTestWithError(server.addr, &SSHCredentials{
		Username: "root",
		Password: "legacy-secret",
	}, SSHCompatibilityModern)
	if err == nil {
		t.Fatal("expected modern compatibility handshake to fail against legacy-only server")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no common algorithm") {
		t.Fatalf("modern handshake error = %q, want no common algorithm", err.Error())
	}

	mux := dialSSHMultiplexerForTest(t, server.addr, &SSHCredentials{
		Username: "root",
		Password: "legacy-secret",
	}, SSHCompatibilityLegacy)
	defer mux.Close()

	assertSessionPrompt(t, mux, "legacy")
}

func newSSHTestServer(t *testing.T, opts sshTestServerOptions) *sshTestServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}

	hostSigner := generateED25519Signer(t)
	config := &ssh.ServerConfig{
		NoClientAuth: opts.allowNoClientAuth,
		Config: ssh.Config{
			KeyExchanges: opts.keyExchanges,
			Ciphers:      opts.ciphers,
			MACs:         opts.macs,
		},
		BannerCallback: func(conn ssh.ConnMetadata) string {
			return "integration-banner"
		},
	}
	config.AddHostKey(hostSigner)

	if opts.password != "" && !opts.keyboardInteractive {
		config.PasswordCallback = func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == opts.username && string(password) == opts.password {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("invalid password")
		}
	}

	if opts.keyboardInteractive {
		config.KeyboardInteractiveCallback = func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge(conn.User(), "integration-login", []string{"Password:"}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) == 1 && conn.User() == opts.username && answers[0] == opts.password {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("invalid keyboard-interactive response")
		}
	}

	if opts.authorizedPublicKey != nil {
		config.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == opts.username && bytes.Equal(key.Marshal(), opts.authorizedPublicKey.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("unauthorized public key")
		}
	}

	server := &sshTestServer{
		listener: listener,
		addr:     listener.Addr().String(),
		config:   config,
	}

	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			server.wg.Add(1)
			go func(rawConn net.Conn) {
				defer server.wg.Done()
				server.serveConn(rawConn)
			}(conn)
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		server.wg.Wait()
	})

	return server
}

func (s *sshTestServer) serveConn(rawConn net.Conn) {
	serverConn, chans, reqs, err := ssh.NewServerConn(rawConn, s.config)
	if err != nil {
		_ = rawConn.Close()
		return
	}
	defer serverConn.Close()

	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go handleSSHTestSessionChannel(channel, requests)
	}
}

func handleSSHTestSessionChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	var startShell sync.Once
	for req := range requests {
		switch req.Type {
		case "pty-req":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			startShell.Do(func() {
				_, _ = channel.Write([]byte("ready> "))
				go func() {
					buf := make([]byte, 1024)
					for {
						n, err := channel.Read(buf)
						if n > 0 {
							_, _ = channel.Write(buf[:n])
						}
						if err != nil {
							return
						}
					}
				}()
			})
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func dialSSHMultiplexerForTest(t *testing.T, target string, creds *SSHCredentials, mode SSHCompatibilityMode) *SSHMultiplexer {
	t.Helper()

	mux, err := dialSSHMultiplexerForTestWithError(target, creds, mode)
	if err != nil {
		t.Fatalf("dialSSHMultiplexerForTest returned error: %v", err)
	}
	return mux
}

func dialSSHMultiplexerForTestWithError(target string, creds *SSHCredentials, mode SSHCompatibilityMode) (*SSHMultiplexer, error) {
	hostKeyPolicy := newSSHHostKeyPolicy(nil, "", "")
	config, err := buildSSHClientConfig(creds, target, mode, hostKeyPolicy)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, err
	}

	mux, err := NewSSHMultiplexer(ctx, "tenant", "peer", target, conn, config, mode, hostKeyPolicy)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	return mux, nil
}

func assertSessionPrompt(t *testing.T, mux *SSHMultiplexer, name string) {
	t.Helper()

	sessionID := "session-" + name
	muxSession, err := mux.NewSession(sessionID, 24, 80)
	if err != nil {
		t.Fatalf("NewSession(%s) returned error: %v", name, err)
	}
	defer mux.CloseSession(sessionID)

	output := readSSHSessionOutput(t, muxSession.stdout)
	if !strings.Contains(output, "ready>") {
		t.Fatalf("session output = %q, want prompt", output)
	}
}

func readSSHSessionOutput(t *testing.T, reader io.Reader) string {
	t.Helper()

	type readResult struct {
		data string
		err  error
	}

	results := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := reader.Read(buf)
		results <- readResult{data: string(buf[:n]), err: err}
	}()

	select {
	case result := <-results:
		if result.err != nil && result.err != io.EOF {
			t.Fatalf("reader.Read returned error: %v", result.err)
		}
		return result.data
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSH session output")
		return ""
	}
}

func generateED25519Signer(t *testing.T) ssh.Signer {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey returned error: %v", err)
	}

	return signer
}

func generateRSAPrivateKeyPEM(t *testing.T) ([]byte, ssh.PublicKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey returned error: %v", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return privateKeyPEM, signer.PublicKey()
}

func generateEncryptedRSAPrivateKeyPEM(t *testing.T, passphrase string) ([]byte, ssh.PublicKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey returned error: %v", err)
	}

	block, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(privateKey), []byte(passphrase), x509.PEMCipherAES256)
	if err != nil {
		t.Fatalf("EncryptPEMBlock returned error: %v", err)
	}

	return pem.EncodeToMemory(block), signer.PublicKey()
}
