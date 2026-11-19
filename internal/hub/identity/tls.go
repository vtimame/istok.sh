package identity

import (
	"crypto/tls"
	"errors"
	"fmt"
)

// ErrUnknownPeer rejects a TLS peer whose fingerprint is not trusted.
var ErrUnknownPeer = errors.New("unknown peer certificate")

// ServerConfig is the hub side of mutual TLS. Every client presents a
// certificate; accept decides during the handshake whether its fingerprint
// may connect, so unknown clients never reach application code.
func ServerConfig(hub Identity, accept func(fingerprint string) bool) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{hub.Certificate},
		ClientAuth:   tls.RequireAnyClientCert,
		NextProtos:   []string{"h2"},
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return ErrUnknownPeer
			}
			if !accept(Fingerprint(state.PeerCertificates[0])) {
				return ErrUnknownPeer
			}

			return nil
		},
	}
}

// ClientConfig is the device side: it presents the device certificate and
// accepts only the hub with the pinned fingerprint. Chain verification is
// replaced by the pin; the handshake still proves the hub holds the key.
func ClientConfig(device Identity, hubFingerprint string) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{device.Certificate},
		InsecureSkipVerify: true, //nolint:gosec // the hub is verified by its pinned fingerprint below
		NextProtos:         []string{"h2"},
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return ErrUnknownPeer
			}

			got := Fingerprint(state.PeerCertificates[0])
			if got != hubFingerprint {
				return fmt.Errorf("%w: hub fingerprint is %s, expected %s", ErrUnknownPeer, got, hubFingerprint)
			}

			return nil
		},
	}
}
