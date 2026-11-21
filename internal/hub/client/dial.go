package client

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"time"
)

// dialTimeout bounds one connection attempt, handshake included.
const dialTimeout = 10 * time.Second

// SplitAddresses reads a hub address: one HOST:PORT or several separated by
// commas, for a hub reached differently from different networks (a LAN
// address at home, a VPN address away).
func SplitAddresses(address string) []string {
	var addresses []string
	for _, part := range strings.Split(address, ",") {
		if part = strings.TrimSpace(part); part != "" {
			addresses = append(addresses, part)
		}
	}

	return addresses
}

// raceDialer connects to every address at once and keeps the first that
// completes the pinned TLS handshake, so an address out of reach costs no
// time and a stranger listening on the same address in another network
// never wins.
type raceDialer struct {
	addresses []string
	config    *tls.Config
}

func (d raceDialer) DialTLSContext(ctx context.Context, _, _ string) (net.Conn, error) {
	if len(d.addresses) == 0 {
		return nil, errors.New("no hub address")
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	type attempt struct {
		conn net.Conn
		err  error
	}
	results := make(chan attempt, len(d.addresses))
	for _, address := range d.addresses {
		go func() {
			dialer := &tls.Dialer{Config: d.config}
			conn, err := dialer.DialContext(ctx, "tcp", address)
			results <- attempt{conn: conn, err: err}
		}()
	}

	var (
		winner net.Conn
		errs   []error
	)
	for range d.addresses {
		result := <-results
		switch {
		case result.err != nil:
			errs = append(errs, result.err)
		case winner == nil:
			winner = result.conn
			cancel()
		default:
			_ = result.conn.Close()
		}
	}

	if winner != nil {
		return winner, nil
	}

	return nil, errors.Join(errs...)
}
