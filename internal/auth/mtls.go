package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// MTLSConfig holds mTLS configuration
type MTLSConfig struct {
	// Server certificates
	ServerCertFile string
	ServerKeyFile  string

	// CA certificate for client verification
	CACertFile string

	// Client certificates (for admin panel)
	ClientCertFile string
	ClientKeyFile  string

	// Certificate directory for auto-generation
	CertDir string

	// Auto-generate if certs don't exist
	AutoGenerate bool
}

// DefaultMTLSConfig returns default mTLS configuration
func DefaultMTLSConfig() *MTLSConfig {
	return &MTLSConfig{
		CertDir:      "./certs",
		AutoGenerate: true,
	}
}

// MTLSManager handles mTLS certificate management
type MTLSManager struct {
	config *MTLSConfig
}

// NewMTLSManager creates a new mTLS manager
func NewMTLSManager(config *MTLSConfig) (*MTLSManager, error) {
	if config == nil {
		config = DefaultMTLSConfig()
	}
	// Empty CertDir is a config gap, not an intent — default rather than
	// crash on mkdir("").
	if config.CertDir == "" {
		config.CertDir = "./certs"
	}

	m := &MTLSManager{config: config}

	// Set default paths if not specified
	if config.CertDir != "" {
		if config.ServerCertFile == "" {
			config.ServerCertFile = filepath.Join(config.CertDir, "server.crt")
		}
		if config.ServerKeyFile == "" {
			config.ServerKeyFile = filepath.Join(config.CertDir, "server.key")
		}
		if config.CACertFile == "" {
			config.CACertFile = filepath.Join(config.CertDir, "ca.crt")
		}
		if config.ClientCertFile == "" {
			config.ClientCertFile = filepath.Join(config.CertDir, "client.crt")
		}
		if config.ClientKeyFile == "" {
			config.ClientKeyFile = filepath.Join(config.CertDir, "client.key")
		}
	}

	// Auto-generate certificates if they don't exist
	if config.AutoGenerate {
		if err := m.EnsureCertificates(); err != nil {
			return nil, fmt.Errorf("failed to ensure certificates: %w", err)
		}
	}

	return m, nil
}

// EnsureCertificates checks if certificates exist and generates them if needed
func (m *MTLSManager) EnsureCertificates() error {
	// Check if all required files exist
	requiredFiles := []string{
		m.config.CACertFile,
		m.config.ServerCertFile,
		m.config.ServerKeyFile,
		m.config.ClientCertFile,
		m.config.ClientKeyFile,
	}

	allExist := true
	var missingFile string
	for _, file := range requiredFiles {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			allExist = false
			missingFile = file
			break
		}
	}

	if allExist {
		log.Debug().Str("cert_dir", m.config.CertDir).Msg("mTLS certificates already exist")
		return nil
	}

	// If auto-generate is off and files are missing, we can't do anything
	// but we shouldn't necessarily crash here if the caller might fallback to insecure.
	// However, most methods will fail later if they try to load these files.
	if !m.config.AutoGenerate {
		log.Warn().Str("missing_file", missingFile).Msg("  mTLS certificates missing and auto-generation disabled")
		return nil
	}

	// Create cert directory
	if err := os.MkdirAll(m.config.CertDir, 0755); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	log.Debug().Str("cert_dir", m.config.CertDir).Msg("Generating mTLS certificates...")

	// Generate CA certificate
	caCert, caKey, err := m.generateCA()
	if err != nil {
		return fmt.Errorf("failed to generate CA: %w", err)
	}

	// Save CA certificate
	if err := m.saveCertificate(m.config.CACertFile, caCert); err != nil {
		return fmt.Errorf("failed to save CA certificate: %w", err)
	}

	// Generate and save server certificate
	if err := m.generateAndSaveCert("server", caCert, caKey, m.config.ServerCertFile, m.config.ServerKeyFile); err != nil {
		return fmt.Errorf("failed to generate server certificate: %w", err)
	}

	// Generate and save client certificate
	if err := m.generateAndSaveCert("client", caCert, caKey, m.config.ClientCertFile, m.config.ClientKeyFile); err != nil {
		return fmt.Errorf("failed to generate client certificate: %w", err)
	}

	log.Debug().
		Str("ca_cert", m.config.CACertFile).
		Str("server_cert", m.config.ServerCertFile).
		Str("client_cert", m.config.ClientCertFile).
		Msg("mTLS certificates generated successfully")

	return nil
}

// generateCA generates a self-signed CA certificate
func (m *MTLSManager) generateCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	// Generate RSA key pair
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, err
	}

	// Create CA certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	ca := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:  []string{"WantasticCore"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "WantasticCore CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(100, 0, 0), // Valid for 100 years
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	// Parse the certificate
	caCert, err := x509.ParseCertificate(caBytes)
	if err != nil {
		return nil, nil, err
	}

	return caCert, caKey, nil
}

// generateAndSaveCert generates a certificate signed by CA and saves it
func (m *MTLSManager) generateAndSaveCert(commonName string, caCert *x509.Certificate, caKey *rsa.PrivateKey, certFile, keyFile string) error {
	// Generate key pair
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"WantasticCore"},
			CommonName:   commonName,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	// Add DNS names and IPs for server cert
	if commonName == "server" {
		template.DNSNames = []string{"localhost", "overlay-node"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}

	// Create certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}

	// Save certificate
	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return err
	}

	// Save private key
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return err
	}

	return nil
}

// saveCertificate saves a certificate to file
func (m *MTLSManager) saveCertificate(filename string, cert *x509.Certificate) error {
	certOut, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer certOut.Close()

	return pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// GetServerTLSConfig returns TLS config for gRPC server with client certificate verification
func (m *MTLSManager) GetServerTLSConfig() (*tls.Config, error) {
	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(m.config.ServerCertFile, m.config.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA certificate for client verification
	caCert, err := os.ReadFile(m.config.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
	}

	// Create TLS config with client certificate verification
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12,
	}

	return tlsConfig, nil
}

// GetServerTLSConfigNoClientAuth returns TLS config for the HTTPS server
// without client certificate verification.
func (m *MTLSManager) GetServerTLSConfigNoClientAuth() (*tls.Config, error) {
	serverCert, err := tls.LoadX509KeyPair(m.config.ServerCertFile, m.config.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	}

	return tlsConfig, nil
}
