package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"

	"github.com/vtimame/istok.sh/internal/hub/identity"
)

const metaAdvertise = "advertise"

// DefaultDataDir holds the hub database and key.
func DefaultDataDir() string {
	return filepath.Join(xdg.DataHome, "istok-hub")
}

// Hub is an opened hub data directory: its database and identity.
type Hub struct {
	Store    *Store
	Identity identity.Identity
}

// Open prepares the data directory, creating the hub key on first use.
func Open(ctx context.Context, dataDir string) (*Hub, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve hub data directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create hub data directory: %w", err)
	}
	// The database names devices and projects; keep the directory private.
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect hub data directory: %w", err)
	}

	hubIdentity, err := identity.LoadOrCreate(dataDir, "hub")
	if err != nil {
		return nil, err
	}

	store, err := OpenStore(ctx, filepath.Join(dataDir, "hub.db"))
	if err != nil {
		return nil, err
	}

	return &Hub{Store: store, Identity: hubIdentity}, nil
}

func (h *Hub) Close() error { return h.Store.Close() }

// Advertised is the address devices use, saved by the last `hub serve`.
func (h *Hub) Advertised(ctx context.Context) (string, error) {
	return h.Store.Meta(ctx, metaAdvertise)
}

// Invite opens an enrollment and returns the code a device joins with.
func (h *Hub) Invite(ctx context.Context, name, address string, ttl time.Duration) (identity.Invitation, error) {
	if address == "" {
		advertised, err := h.Advertised(ctx)
		if err != nil {
			return identity.Invitation{}, err
		}
		address = advertised
	}
	if address == "" {
		return identity.Invitation{}, errors.New("the hub address is unknown: pass --address HOST:PORT or start `istok hub serve --advertise HOST:PORT` once")
	}

	secret, err := identity.NewSecret()
	if err != nil {
		return identity.Invitation{}, err
	}
	if err := h.Store.AddEnrollment(ctx, name, secret, ttl); err != nil {
		return identity.Invitation{}, err
	}

	return identity.Invitation{Address: address, Fingerprint: h.Identity.Fingerprint, Secret: secret, Name: name}, nil
}

// ServeConfig configures `istok hub serve`.
type ServeConfig struct {
	Listen    string
	Advertise string
	Logger    *slog.Logger

	// Limits default to DefaultLimits when zero.
	Limits Limits

	// Ready, when set, receives the bound address once the hub listens.
	Ready func(address string)
}

// Serve accepts devices until ctx ends.
func (h *Hub) Serve(ctx context.Context, config ServeConfig) error {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	if config.Advertise != "" {
		if err := h.Store.SetMeta(ctx, metaAdvertise, config.Advertise); err != nil {
			return err
		}
	}

	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.Listen, err)
	}

	tlsConfig := identity.ServerConfig(h.Identity, func(fingerprint string) bool {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		return h.Store.Accepts(checkCtx, fingerprint)
	})

	limits := config.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	guard := newGuard(limits)
	guarded := newGuardedListener(ctx, listener, tlsConfig, guard)

	server := &http.Server{
		Handler:           newHandler(h.Store, guard, logger),
		ConnState:         guarded.connState,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		// Rejected handshakes are expected on an open port; keep them out of the log.
		ErrorLog: slog.NewLogLogger(slog.DiscardHandler, slog.LevelError),
	}

	logger.Info("hub listening", "address", listener.Addr().String(), "fingerprint", h.Identity.Fingerprint)
	if config.Ready != nil {
		config.Ready(listener.Addr().String())
	}

	done := make(chan error, 1)
	go func() { done <- server.Serve(guarded) }()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return server.Shutdown(shutdown)
	}
}
