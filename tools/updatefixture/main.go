package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if len(os.Args) < 2 {
		panic("usage: updatefixture serve | verify DATABASE")
	}
	var err error
	switch os.Args[1] {
	case "serve":
		if len(os.Args) != 2 {
			err = fmt.Errorf("usage: updatefixture serve")
		} else {
			err = serve()
		}
	case "verify":
		if len(os.Args) != 3 {
			err = fmt.Errorf("usage: updatefixture verify DATABASE")
		} else {
			err = verify(os.Args[2])
		}
	default:
		err = fmt.Errorf("usage: updatefixture serve | verify DATABASE")
	}
	if err != nil {
		panic(err)
	}
}

func serve() error {
	root := filepath.Join("tmp", "update-fixture", "istok", "cli")
	fixtureRoot := filepath.Join("tmp", "update-fixture")
	if err := os.RemoveAll(fixtureRoot); err != nil {
		return fmt.Errorf("reset update fixture: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create update fixture: %w", err)
	}
	key, cert, err := certificate()
	if err != nil {
		return err
	}
	cert64 := base64.StdEncoding.EncodeToString(cert)
	old := filepath.Join(fixtureRoot, "istok-old"+exe())
	if err := build(old, "v0.0.1", "http://127.0.0.1:8080/", cert64); err != nil {
		return err
	}
	newbin := filepath.Join(fixtureRoot, "istok-new"+exe())
	if err := build(newbin, "v0.0.2", "http://127.0.0.1:8080/", cert64); err != nil {
		return err
	}
	asset := fmt.Sprintf("istok_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := filepath.Join(root, asset)
	if err := pack(archive, newbin, filepath.Base(old)); err != nil {
		return err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return fmt.Errorf("read fixture archive: %w", err)
	}
	hash := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return fmt.Errorf("sign fixture archive: %w", err)
	}
	sig, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		return fmt.Errorf("encode fixture signature: %w", err)
	}
	if err := os.WriteFile(archive+".sig", sig, 0o600); err != nil {
		return fmt.Errorf("write fixture signature: %w", err)
	}
	manifest := fmt.Sprintf("last_release_id: 1\nlast_asset_id: 2\nreleases:\n  - id: 1\n    name: v0.0.2\n    tag_name: v0.0.2\n    published_at: %s\n    release_notes: local fixture\n    assets:\n      - id: 1\n        name: %s\n        size: %d\n        url: %s\n      - id: 2\n        name: %s.sig\n        size: %d\n        url: %s.sig\n", time.Now().UTC().Format(time.RFC3339), asset, len(data), asset, asset, len(sig), asset)
	if err := os.WriteFile(filepath.Join(root, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		return fmt.Errorf("write fixture manifest: %w", err)
	}
	cmd := exec.Command("docker", "compose", "up", "-d", "update-server")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start update fixture server: %w", err)
	}
	return nil
}
func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
func certificate() (*ecdsa.PrivateKey, []byte, error) {
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, nil, fmt.Errorf("generate fixture key: %w", e)
	}
	t := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	d, e := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if e != nil {
		return nil, nil, fmt.Errorf("create fixture certificate: %w", e)
	}
	return k, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d}), nil
}
func build(out, v, u, c string) error {
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", fmt.Sprintf("-X s26.dev/istok-cli/internal/buildinfo.Version=%s -X s26.dev/istok-cli/internal/buildinfo.ReleaseBaseURL=%s -X s26.dev/istok-cli/internal/buildinfo.CertificateBase64=%s", v, u, c), "./cmd/istok")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("build fixture binary: %w", e)
	}
	return nil
}
func pack(out, binary, name string) (err error) {
	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close archive: %w", closeErr)
		}
	}()

	g := gzip.NewWriter(f)
	defer func() {
		if closeErr := g.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close gzip archive: %w", closeErr)
		}
	}()
	t := tar.NewWriter(g)
	defer func() {
		if closeErr := t.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close tar archive: %w", closeErr)
		}
	}()

	d, err := os.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("read update binary: %w", err)
	}
	if err = t.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(d))}); err != nil {
		return fmt.Errorf("write archive header: %w", err)
	}
	if _, err = t.Write(d); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

func verify(path string) (err error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close database: %w", closeErr)
		}
	}()
	var version int
	if err := db.QueryRow("SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&version); err != nil {
		return fmt.Errorf("read goose version: %w", err)
	}
	if version != 7 {
		return fmt.Errorf("goose version = %d, want 7", version)
	}
	fmt.Println(version)
	return nil
}
