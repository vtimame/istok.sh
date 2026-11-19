package server

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/vtimame/istok.sh/internal/hub/identity"
)

func TestGuardBansAnAddressAfterFailedHandshakes(t *testing.T) {
	limits := DefaultLimits()
	limits.FailedHandshakes = 3
	guard := newGuard(limits)

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	guard.now = func() time.Time { return now }

	for range 3 {
		if !guard.admit("203.0.113.7") {
			t.Fatal("address refused before the ban")
		}
		guard.release("203.0.113.7")
		guard.failed("203.0.113.7")
	}

	if guard.admit("203.0.113.7") {
		t.Fatal("address admitted after three failed handshakes")
	}
	if !guard.admit("198.51.100.1") {
		t.Fatal("another address was refused")
	}

	now = now.Add(limits.BanWindow + time.Second)
	if !guard.admit("203.0.113.7") {
		t.Fatal("ban did not expire")
	}
}

func TestGuardLimitsConnectionsPerAddress(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConnectionsPerIP = 2
	guard := newGuard(limits)

	if !guard.admit("203.0.113.7") || !guard.admit("203.0.113.7") {
		t.Fatal("first connections refused")
	}
	if guard.admit("203.0.113.7") {
		t.Fatal("third open connection admitted")
	}

	guard.release("203.0.113.7")
	if !guard.admit("203.0.113.7") {
		t.Fatal("slot was not released")
	}
}

func TestGuardLimitsEnrollmentAttempts(t *testing.T) {
	limits := DefaultLimits()
	limits.EnrollBurst = 2
	guard := newGuard(limits)

	if !guard.allowEnroll("203.0.113.7") || !guard.allowEnroll("203.0.113.7") {
		t.Fatal("first attempts refused")
	}
	if guard.allowEnroll("203.0.113.7") {
		t.Fatal("attempt beyond the burst allowed")
	}
}

// An unknown client certificate fails the handshake and counts toward the ban.
func TestHubDropsUnknownCertificatesAndBansTheAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()

	limits := DefaultLimits()
	limits.FailedHandshakes = 2

	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- hub.Serve(ctx, ServeConfig{Listen: "127.0.0.1:0", Limits: limits, Ready: func(address string) { ready <- address }})
	}()
	address := <-ready

	stranger, err := identity.LoadOrCreate(t.TempDir(), "device")
	if err != nil {
		t.Fatal(err)
	}

	dial := func() error {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", address, identity.ClientConfig(stranger, hub.Identity.Fingerprint))
		if err != nil {
			return err
		}
		defer conn.Close()

		// TLS 1.3 reports a rejected client certificate on the first read.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err = conn.Read(make([]byte, 1))
		return err
	}

	for attempt := range 2 {
		if err := dial(); err == nil {
			t.Fatalf("attempt %d: unknown certificate was accepted", attempt)
		}
	}

	// Banned now: the connection is closed before any TLS answer.
	err = dial()
	if err == nil {
		t.Fatal("banned address connected")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
