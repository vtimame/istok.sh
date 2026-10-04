package webui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

const shutdownTimeout = 5 * time.Second

// Server is a loopback-only HTTP server for the embedded UI.
type Server struct {
	listener net.Listener
	server   *http.Server
}

// Listen binds the UI to 127.0.0.1; port 0 lets the OS pick a free port.
func Listen(port int, services Services) (*Server, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("listen on 127.0.0.1:%d (choose another with --port): %w", port, err)
	}

	mux := http.NewServeMux()
	api{services: services}.register(mux)
	mux.Handle("/", assetsHandler())

	actualPort := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	server := &http.Server{
		Handler:           guard(actualPort, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &Server{listener: listener, server: server}, nil
}

// URL returns the address to open in a browser.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/", s.listener.Addr().String())
}

// Serve blocks until ctx is cancelled, then shuts the server down gracefully.
func (s *Server) Serve(ctx context.Context) error {
	served := make(chan error, 1)
	go func() { served <- s.server.Serve(s.listener) }()

	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		return s.server.Shutdown(shutdownCtx)
	}
}
