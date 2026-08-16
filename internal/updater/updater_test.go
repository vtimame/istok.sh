package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

func TestNewWithRejectsUnsafeConfiguration(t *testing.T) {
	certificate := testCertificate(t)
	for _, baseURL := range []string{"http://updates.example.test", "ftp://updates.example.test", "https://", "https://user:pass@updates.example.test", "https://updates.example.test/?x=1", "https://updates.example.test/#fragment"} {
		if _, err := NewWith("v1.0.0", baseURL, certificate); err == nil {
			t.Errorf("NewWith(%q) unexpectedly succeeded", baseURL)
		}
	}
	if _, err := NewWith("v1.0.0", "http://127.0.0.1:8080", certificate); err != nil {
		t.Fatalf("loopback fixture rejected: %v", err)
	}
}

func TestRejectsInvalidDirectSignatureWithoutReplacingTarget(t *testing.T) {
	key, certificate := testKeyAndCertificate(t)
	archive := testArchive(t, []byte("tampered binary"))
	hash := sha256.Sum256([]byte("original archive before tampering"))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	badSignature, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		t.Fatal(err)
	}
	asset := fmt.Sprintf("istok_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/istok/cli/manifest.yaml":
			_, _ = fmt.Fprintf(writer, "last_release_id: 1\nlast_asset_id: 2\nreleases:\n  - id: 1\n    name: v1.0.1\n    tag_name: v1.0.1\n    published_at: 2026-01-01T00:00:00Z\n    assets:\n      - id: 1\n        name: %s\n        size: %d\n        url: %s\n      - id: 2\n        name: %s.sig\n        size: %d\n        url: %s.sig\n", asset, len(archive), asset, asset, len(badSignature), asset)
		case "/istok/cli/" + asset:
			_, _ = writer.Write(archive)
		case "/istok/cli/" + asset + ".sig":
			_, _ = writer.Write(badSignature)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service, err := NewWith("v1.0.0", server.URL+"/", certificate)
	if err != nil {
		t.Fatal(err)
	}
	update, available, err := service.Check(context.Background(), "v1.0.0")
	if err != nil || !available {
		t.Fatalf("Check() update=%v available=%v err=%v", update, available, err)
	}
	target := filepath.Join(t.TempDir(), "istok")
	if err := os.WriteFile(target, []byte("original binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), "old")
	if err := service.Apply(context.Background(), update.Release, target, old); !errors.Is(err, selfupdate.ErrECDSAValidationFailed) {
		t.Fatalf("Apply() error = %v, want ECDSA validation failure", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "original binary" {
		t.Fatalf("target changed to %q, err=%v", got, err)
	}
}

func TestCheckRejectsOversizedAsset(t *testing.T) {
	certificate := testCertificate(t)
	asset := fmt.Sprintf("istok_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/istok/cli/manifest.yaml" {
			_, _ = fmt.Fprintf(writer, "last_release_id: 1\nlast_asset_id: 1\nreleases:\n  - id: 1\n    name: v1.0.1\n    tag_name: v1.0.1\n    published_at: 2026-01-01T00:00:00Z\n    assets:\n      - id: 1\n        name: %s\n        size: %d\n        url: /%s\n", asset, MaxAssetSize+1, asset)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	service, err := NewWith("v1.0.0", server.URL+"/", certificate)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Check(context.Background(), "v1.0.0"); err == nil {
		t.Fatal("oversized asset was accepted")
	}
}

func TestNewWithFailsClosed(t *testing.T) {
	if _, err := NewWith("dev", "https://updates.example.test", []byte("not a certificate")); err == nil {
		t.Fatal("NewWith unexpectedly accepted a dev build and invalid certificate")
	}
}

func testCertificate(t *testing.T) []byte {
	t.Helper()
	_, certificate := testKeyAndCertificate(t)
	return certificate
}

func testKeyAndCertificate(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "istok", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
