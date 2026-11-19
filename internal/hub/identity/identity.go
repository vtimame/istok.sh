// Package identity gives hubs and devices a key pair whose public key
// fingerprint names them. Hubs and devices pin each other's fingerprints, so
// connections need neither a domain nor a certificate authority.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// Identity is a self-signed TLS certificate and its fingerprint.
type Identity struct {
	Certificate tls.Certificate
	Fingerprint string
}

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Fingerprint names a certificate by the SHA-256 of its public key, so a
// renewed certificate with the same key keeps its name.
func Fingerprint(certificate *x509.Certificate) string {
	sum := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)

	return strings.ToLower(encoding.EncodeToString(sum[:]))
}

// LoadOrCreate reads name.key and name.crt from dir, creating a new Ed25519
// identity on first use. The directory and files are private to the user.
func LoadOrCreate(dir, name string) (Identity, error) {
	keyPath := filepath.Join(dir, name+".key")
	certPath := filepath.Join(dir, name+".crt")

	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err == nil {
		return fromCertificate(certificate)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Identity{}, fmt.Errorf("read %s identity: %w", name, err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, fmt.Errorf("create identity directory: %w", err)
	}

	// Two processes may create the identity at once; the lock makes one of
	// them write it and the other read it.
	lock := flock.New(filepath.Join(dir, name+".lock"), flock.SetPermissions(0o600))
	if err := lock.Lock(); err != nil {
		return Identity{}, fmt.Errorf("lock %s identity: %w", name, err)
	}
	defer func() { _ = lock.Unlock() }()

	if certificate, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return fromCertificate(certificate)
	}

	keyPEM, certPEM, err := generate(name)
	if err != nil {
		return Identity{}, err
	}

	if err := writePrivate(keyPath, keyPEM); err != nil {
		return Identity{}, err
	}
	if err := writePrivate(certPath, certPEM); err != nil {
		return Identity{}, err
	}

	certificate, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Identity{}, fmt.Errorf("load new %s identity: %w", name, err)
	}

	return fromCertificate(certificate)
}

func fromCertificate(certificate tls.Certificate) (Identity, error) {
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return Identity{}, fmt.Errorf("parse identity certificate: %w", err)
	}
	certificate.Leaf = leaf

	return Identity{Certificate: certificate, Fingerprint: Fingerprint(leaf)}, nil
}

func generate(name string) ([]byte, []byte, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate %s key: %w", name, err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "istok " + name},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s certificate: %w", name, err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s key: %w", name, err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return keyPEM, certPEM, nil
}

// writePrivate writes through a temporary file, so a reader never sees half
// a key.
func writePrivate(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	return os.Rename(temporary, path)
}
