// Command release creates and verifies the signing artifacts used by production releases.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"gopkg.in/yaml.v3"
)

var expectedArchives = []string{
	"istok_darwin_amd64.tar.gz",
	"istok_darwin_arm64.tar.gz",
	"istok_linux_amd64.tar.gz",
	"istok_linux_arm64.tar.gz",
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: release <secret|certificate|assemble>"))
	}

	var err error
	switch os.Args[1] {
	case "secret":
		err = runSecret(os.Stdout)
	case "certificate":
		err = runCertificate(os.Args[2:], os.Stdout)
	case "assemble":
		err = runAssemble(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "release:", err)
	os.Exit(1)
}

func runSecret(output *os.File) error {
	bundle, err := newBundle(time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, base64.StdEncoding.EncodeToString(bundle))
	return err
}

func runCertificate(args []string, output *os.File) error {
	flags := flag.NewFlagSet("certificate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	bundlePath := flags.String("bundle", "", "PEM signing bundle")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || flags.NArg() != 0 {
		return errors.New("usage: release certificate --bundle PATH")
	}

	_, certificate, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, base64.StdEncoding.EncodeToString(certificate))
	return err
}

func runAssemble(args []string) error {
	flags := flag.NewFlagSet("assemble", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	bundlePath := flags.String("bundle", "", "PEM signing bundle")
	dist := flags.String("dist", "", "release asset directory")
	version := flags.String("version", "", "stable release tag")
	repository := flags.String("repository", "", "GitHub owner/repository")
	publishedAt := flags.String("published-at", "", "RFC3339 publication time")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || *dist == "" || *version == "" || *repository == "" || *publishedAt == "" || flags.NArg() != 0 {
		return errors.New("usage: release assemble --bundle PATH --dist PATH --version TAG --repository OWNER/REPO --published-at RFC3339")
	}

	key, _, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}
	if err := validateStableTag(*version); err != nil {
		return err
	}
	owner, repo, err := splitRepository(*repository)
	if err != nil {
		return err
	}
	published, err := time.Parse(time.RFC3339, *publishedAt)
	if err != nil {
		return fmt.Errorf("parse published-at: %w", err)
	}

	return assemble(*dist, key, *version, owner, repo, published.UTC())
}

func newBundle(now time.Time) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA key: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal ECDSA key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	serial.SetBit(serial, 0, 1)
	certificateTemplate := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Istok CLI Update Signing"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, certificateTemplate, certificateTemplate, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	bundle := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})...)
	return bundle, nil
}

func readBundle(path string) (*ecdsa.PrivateKey, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read signing bundle: %w", err)
	}
	key, certificate, err := parseBundle(data)
	if err != nil {
		return nil, nil, err
	}
	return key, certificate, nil
}

func parseBundle(data []byte) (*ecdsa.PrivateKey, []byte, error) {
	keyBlock, rest := pem.Decode(data)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, nil, errors.New("signing bundle must start with a PKCS#8 PRIVATE KEY PEM block")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
	}
	key, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, nil, errors.New("signing bundle private key must be ECDSA P-256")
	}
	certificateBlock, rest := pem.Decode(rest)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, nil, errors.New("signing bundle must contain exactly one CERTIFICATE PEM block after the private key")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}
	if err := certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature); err != nil {
		return nil, nil, fmt.Errorf("signing certificate must be self-signed: %w", err)
	}
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() || publicKey.X.Cmp(key.PublicKey.X) != 0 || publicKey.Y.Cmp(key.PublicKey.Y) != 0 {
		return nil, nil, errors.New("certificate public key does not match signing private key")
	}

	return key, pem.EncodeToMemory(certificateBlock), nil
}

func assemble(dist string, key *ecdsa.PrivateKey, version, owner, repository string, publishedAt time.Time) error {
	archives, err := releaseArchives(dist)
	if err != nil {
		return err
	}

	assets := make([]*selfupdate.HttpAsset, 0, len(archives)*2)
	checksums := make([]string, 0, len(archives))
	assetID := int64(1)
	for _, archive := range archives {
		data, err := os.ReadFile(filepath.Join(dist, archive))
		if err != nil {
			return fmt.Errorf("read archive %q: %w", archive, err)
		}
		hash := sha256.Sum256(data)
		r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
		if err != nil {
			return fmt.Errorf("sign archive %q: %w", archive, err)
		}
		signature, err := asn1.Marshal(struct{ R, S *big.Int }{R: r, S: s})
		if err != nil {
			return fmt.Errorf("marshal signature for %q: %w", archive, err)
		}
		if err := os.WriteFile(filepath.Join(dist, archive+".sig"), signature, 0o644); err != nil {
			return fmt.Errorf("write signature for %q: %w", archive, err)
		}

		assets = append(assets,
			manifestAsset(assetID, archive, len(data), owner, repository, version),
			manifestAsset(assetID+1, archive+".sig", len(signature), owner, repository, version),
		)
		checksums = append(checksums, fmt.Sprintf("%x  %s", hash, archive))
		assetID += 2
	}
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(strings.Join(checksums, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}

	manifest := selfupdate.HttpManifest{
		LastReleaseID: 1,
		LastAssetID:   assetID - 1,
		Releases: []*selfupdate.HttpRelease{{
			ID:           1,
			Name:         version,
			TagName:      version,
			URL:          fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", owner, repository, version),
			PublishedAt:  publishedAt,
			ReleaseNotes: fmt.Sprintf("Release notes: https://github.com/%s/%s/releases/tag/%s", owner, repository, version),
			Assets:       assets,
		}},
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "manifest.yaml"), data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func manifestAsset(id int64, name string, size int, owner, repository, version string) *selfupdate.HttpAsset {
	return &selfupdate.HttpAsset{
		ID:   id,
		Name: name,
		Size: size,
		URL:  fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repository, version, name),
	}
}

func releaseArchives(dist string) ([]string, error) {
	entries, err := os.ReadDir(dist)
	if err != nil {
		return nil, fmt.Errorf("read dist directory: %w", err)
	}
	expected := make(map[string]struct{}, len(expectedArchives))
	for _, name := range expectedArchives {
		expected[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(expectedArchives))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		if _, ok := expected[entry.Name()]; !ok {
			return nil, fmt.Errorf("unexpected release archive %q", entry.Name())
		}
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("release archive %q is not a regular file", entry.Name())
		}
		seen[entry.Name()] = struct{}{}
	}
	for _, name := range expectedArchives {
		if _, ok := seen[name]; !ok {
			return nil, fmt.Errorf("missing expected release archive %q", name)
		}
	}
	archives := append([]string(nil), expectedArchives...)
	sort.Strings(archives)
	return archives, nil
}

func validateStableTag(tag string) error {
	if !stableTag(tag) {
		return fmt.Errorf("version %q must be a stable tag in vMAJOR.MINOR.PATCH form", tag)
	}
	return nil
}

func stableTag(tag string) bool {
	if !strings.HasPrefix(tag, "v") {
		return false
	}
	version, err := semver.NewVersion(tag)
	if err != nil || version.Prerelease() != "" || version.Metadata() != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func splitRepository(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository %q must be OWNER/REPO", value)
	}
	for _, part := range parts {
		if strings.ContainsAny(part, " /\\\t\n\r") {
			return "", "", fmt.Errorf("repository %q must be OWNER/REPO", value)
		}
	}
	return parts[0], parts[1], nil
}
