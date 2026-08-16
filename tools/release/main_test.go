package main

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"gopkg.in/yaml.v3"
)

func TestGeneratedBundleValidatesAndCertificateMatchesKey(t *testing.T) {
	bundle, err := newBundle(time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	key, certificate, err := parseBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificate)
	parsedCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, ok := parsedCertificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.X.Cmp(key.PublicKey.X) != 0 || publicKey.Y.Cmp(key.PublicKey.Y) != 0 {
		t.Fatal("generated bundle did not retain a matching certificate")
	}
	if _, err := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString(certificate)); err != nil {
		t.Fatalf("certificate output is not base64: %v", err)
	}
}

func TestParseBundleRejectsCertificateForAnotherKey(t *testing.T) {
	first, err := newBundle(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := newBundle(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	keyBlock, _ := pem.Decode(first)
	_, certificateRest := pem.Decode(second)
	certificateBlock, _ := pem.Decode(certificateRest)
	mismatched := pem.EncodeToMemory(keyBlock)
	mismatched = append(mismatched, pem.EncodeToMemory(certificateBlock)...)
	if _, _, err := parseBundle(mismatched); err == nil {
		t.Fatal("bundle with a different certificate key was accepted")
	}
}

func TestAssembleSignsArchivesAndWritesUpdaterManifest(t *testing.T) {
	dist := t.TempDir()
	for _, name := range expectedArchives {
		if err := os.WriteFile(filepath.Join(dist, name), []byte("archive "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := newBundle(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	key, certificate, err := parseBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := assemble(dist, key, "v1.2.3", "istok", "cli", time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	for _, name := range expectedArchives {
		archive, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			t.Fatal(err)
		}
		signature, err := os.ReadFile(filepath.Join(dist, name+".sig"))
		if err != nil {
			t.Fatal(err)
		}
		var rs struct{ R, S *big.Int }
		if _, err := asn1.Unmarshal(signature, &rs); err != nil {
			t.Fatalf("parse %s signature: %v", name, err)
		}
		hash := sha256.Sum256(archive)
		block, _ := pem.Decode(certificate)
		parsedCertificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		publicKey, ok := parsedCertificate.PublicKey.(*ecdsa.PublicKey)
		if !ok || !ecdsa.Verify(publicKey, hash[:], rs.R, rs.S) {
			t.Fatalf("signature for %s did not validate", name)
		}
	}

	data, err := os.ReadFile(filepath.Join(dist, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest selfupdate.HttpManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest does not parse as HttpManifest: %v", err)
	}
	if len(manifest.Releases) != 1 || manifest.Releases[0].TagName != "v1.2.3" || len(manifest.Releases[0].Assets) != 8 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if !strings.Contains(manifest.Releases[0].ReleaseNotes, "/releases/tag/v1.2.3") {
		t.Fatalf("release notes = %q", manifest.Releases[0].ReleaseNotes)
	}
	expectedAssets := make(map[string]struct{}, len(expectedArchives)*2)
	for _, name := range expectedArchives {
		expectedAssets[name] = struct{}{}
		expectedAssets[name+".sig"] = struct{}{}
	}
	seenAssets := make(map[string]struct{}, len(expectedAssets))
	for _, asset := range manifest.Releases[0].Assets {
		if _, ok := expectedAssets[asset.Name]; !ok {
			t.Fatalf("unexpected manifest asset %q", asset.Name)
		}
		if _, duplicate := seenAssets[asset.Name]; duplicate {
			t.Fatalf("duplicate manifest asset %q", asset.Name)
		}
		seenAssets[asset.Name] = struct{}{}
		want := "https://github.com/istok/cli/releases/download/v1.2.3/"
		if !strings.HasPrefix(asset.URL, want) {
			t.Fatalf("asset URL %q does not use the GitHub Release download URL", asset.URL)
		}
	}
	if len(seenAssets) != len(expectedAssets) {
		t.Fatalf("manifest assets = %d, want %d", len(seenAssets), len(expectedAssets))
	}
}

func TestReleaseArchivesEnforcesExpectedSet(t *testing.T) {
	dist := t.TempDir()
	for _, name := range expectedArchives[:3] {
		if err := os.WriteFile(filepath.Join(dist, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := releaseArchives(dist); err == nil || !strings.Contains(err.Error(), "missing expected") {
		t.Fatalf("missing archive error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, expectedArchives[3]), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "unexpected.tar.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseArchives(dist); err == nil || !strings.Contains(err.Error(), "unexpected release archive") {
		t.Fatalf("unexpected archive error = %v", err)
	}
}

func TestStableTag(t *testing.T) {
	for _, tag := range []string{"v0.0.0", "v1.2.3", "v123.456.789"} {
		if !stableTag(tag) {
			t.Errorf("stableTag(%q) = false", tag)
		}
	}
	for _, tag := range []string{"1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2", "v1.2.3-rc.1", "v1.2.3+build", "v1.2.3.4"} {
		if stableTag(tag) {
			t.Errorf("stableTag(%q) = true", tag)
		}
	}
}
