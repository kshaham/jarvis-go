package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
)

// ensureSelfSignedCert loads the persisted self-signed cert + key from
// <dataDir>/tls/, generating a new pair on first run. The cert is valid for
// 10 years and lists localhost, 127.0.0.1, ::1, and every local non-loopback
// IP so LAN hostnames/IPs all validate against the same cert.
//
// This is only used when the user passes --https without wiring up their
// own TLS certificates. It is intentionally simple: a self-signed cert lets
// browsers expose navigator.mediaDevices (secure context) for voice features
// after the user accepts the "untrusted" warning once.
func ensureSelfSignedCert(dataDir string) (*tls.Config, error) {
	tlsDir := filepath.Join(dataDir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create tls dir: %w", err)
	}
	certPath := filepath.Join(tlsDir, "jarvis.crt")
	keyPath := filepath.Join(tlsDir, "jarvis.key")

	// Fast path: load existing cert if present.
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil {
				return &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS12,
				}, nil
			}
			// Fall through and regenerate on load failure.
		}
	}

	// Generate a new ECDSA P-256 key -- small, fast, widely supported.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}

	// Collect SANs: loopback + all local non-loopback IPs.
	dnsNames := []string{"localhost"}
	ipAddrs := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				ipAddrs = append(ipAddrs, ipNet.IP)
			}
		}
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Jarvis"},
			CommonName:   "Jarvis Self-Signed",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}

	// Persist cert (PEM)
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open cert for write: %w", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certOut.Close()
		return nil, fmt.Errorf("pem encode cert: %w", err)
	}
	if err := certOut.Close(); err != nil {
		return nil, fmt.Errorf("close cert: %w", err)
	}

	// Persist key (PEM, 0600)
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open key for write: %w", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		keyOut.Close()
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		keyOut.Close()
		return nil, fmt.Errorf("pem encode key: %w", err)
	}
	if err := keyOut.Close(); err != nil {
		return nil, fmt.Errorf("close key: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load generated cert: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
