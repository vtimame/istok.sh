// Package updater contains the update domain workflow and deliberately has no Fx dependency.
package updater

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	selfupdate "github.com/creativeprojects/go-selfupdate"

	"github.com/vtimame/istok.sh/internal/buildinfo"
)

const UpdateNamespace = "istok"
const UpdateProduct = "cli"
const MaxAssetSize = 256 << 20

type Update struct {
	Current string
	Target  string
	Notes   string
	Size    int
	Release *selfupdate.Release
}

type Service struct {
	baseURL     string
	certificate []byte
	executable  func() (string, error)
	newUpdater  func(selfupdate.Config) (*selfupdate.Updater, error)
}

func New(info buildinfo.Info) (*Service, error) {
	if info.CertificateBase64 == "" {
		return nil, fmt.Errorf("automatic updates are disabled: %w", buildinfo.ErrCertificateMissing)
	}
	certificate, err := base64.StdEncoding.DecodeString(info.CertificateBase64)
	if err != nil {
		return nil, fmt.Errorf("automatic updates are disabled: %w", err)
	}

	return NewWith(info.Version, info.ReleaseBaseURL, certificate)
}

func NewWith(current, baseURL string, certificate []byte) (*Service, error) {
	version, err := semver.NewVersion(current)
	if err != nil || version.Prerelease() != "" {
		return nil, fmt.Errorf("automatic updates require a stable semver build; current version %q is not updateable", current)
	}
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}
	if _, err := ecdsaPublicKey(certificate); err != nil {
		return nil, fmt.Errorf("automatic updates are disabled: %w", err)
	}
	return &Service{baseURL: baseURL, certificate: certificate, executable: selfupdate.ExecutablePath, newUpdater: selfupdate.NewUpdater}, nil
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid update release URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopback(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("update release URL must use HTTPS (HTTP is allowed only for loopback fixtures)")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ecdsaPublicKey(data []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("update certificate is not a PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse update certificate: %w", err)
	}
	key, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("update certificate does not contain an ECDSA key")
	}
	return key, nil
}

func (s *Service) Check(ctx context.Context, current string) (*Update, bool, error) {
	version, err := semver.NewVersion(current)
	if err != nil || version.Prerelease() != "" {
		return nil, false, fmt.Errorf("automatic updates require a stable semver build; current version %q is not updateable", current)
	}
	up, err := s.makeUpdater("")
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, found, err := up.DetectLatest(ctx, selfupdate.NewRepositorySlug(UpdateNamespace, UpdateProduct))
	if err != nil {
		return nil, false, fmt.Errorf("check latest release: %w", err)
	}
	if !found || !release.GreaterThan(current) {
		return nil, false, nil
	}
	if release.AssetByteSize <= 0 || release.AssetByteSize > MaxAssetSize {
		return nil, false, fmt.Errorf("release asset size %d is outside the allowed range", release.AssetByteSize)
	}
	return &Update{Current: current, Target: release.Version(), Notes: release.ReleaseNotes, Size: release.AssetByteSize, Release: release}, true, nil
}

func (s *Service) Apply(ctx context.Context, release *selfupdate.Release, target, oldSavePath string) error {
	if release == nil {
		return errors.New("missing update release")
	}
	if err := checkWritableTarget(target); err != nil {
		return err
	}
	up, err := s.makeUpdater(oldSavePath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := up.UpdateTo(ctx, release, target); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}

func (s *Service) makeUpdater(oldSavePath string) (*selfupdate.Updater, error) {
	key, err := ecdsaPublicKey(s.certificate)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 15 * time.Second
	source, err := selfupdate.NewHttpSource(selfupdate.HttpConfig{BaseURL: s.baseURL, Transport: transport})
	if err != nil {
		return nil, fmt.Errorf("configure update source: %w", err)
	}
	return s.newUpdater(selfupdate.Config{
		Source: source, Validator: &selfupdate.ECDSAValidator{PublicKey: key},
		Filters: []string{fmt.Sprintf("^istok_%s_%s\\.tar\\.gz$", runtime.GOOS, runtime.GOARCH)}, OldSavePath: oldSavePath,
	})
}

func (s *Service) Executable() (string, error) { return s.executable() }

func checkWritableTarget(target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("stat executable target: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("executable target %q is a directory", target)
	}
	directory := filepath.Dir(target)
	probe, err := os.CreateTemp(directory, ".istok-update-probe-*")
	if err != nil {
		return fmt.Errorf("executable directory is not writable: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	return nil
}
