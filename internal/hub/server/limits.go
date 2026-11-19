package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limits bound what one address or device can make the hub do. The hub
// listens on the open internet; without them a single client could exhaust
// connections or keep handshakes busy.
type Limits struct {
	// MaxConnections is the total of open connections.
	MaxConnections int

	// MaxConnectionsPerIP is the open connections of one address.
	MaxConnectionsPerIP int

	// ConnectionRate and ConnectionBurst bound new connections per address.
	ConnectionRate  rate.Limit
	ConnectionBurst int

	// HandshakeTimeout bounds a TLS handshake.
	HandshakeTimeout time.Duration

	// FailedHandshakes within BanWindow ban an address for BanWindow; most
	// failures are unknown certificates.
	FailedHandshakes int
	BanWindow        time.Duration

	// RequestRate and RequestBurst bound requests per device.
	RequestRate  rate.Limit
	RequestBurst int

	// EnrollRate and EnrollBurst bound join attempts per address.
	EnrollRate  rate.Limit
	EnrollBurst int
}

// DefaultLimits suit one person's devices with room to spare.
func DefaultLimits() Limits {
	return Limits{
		MaxConnections:      256,
		MaxConnectionsPerIP: 16,
		ConnectionRate:      2,
		ConnectionBurst:     20,
		HandshakeTimeout:    10 * time.Second,
		FailedHandshakes:    10,
		BanWindow:           10 * time.Minute,
		RequestRate:         20,
		RequestBurst:        60,
		EnrollRate:          rate.Every(12 * time.Second),
		EnrollBurst:         5,
	}
}

// addressState is what the hub remembers about one remote address.
type addressState struct {
	open     int
	limiter  *rate.Limiter
	failures []time.Time
	banned   time.Time
	seen     time.Time
}

// guard tracks addresses for the listener and the enroll handler.
type guard struct {
	limits Limits
	now    func() time.Time

	mu        sync.Mutex
	open      int
	addresses map[string]*addressState
	enrolls   map[string]*rate.Limiter
	devices   map[string]*rate.Limiter
}

func newGuard(limits Limits) *guard {
	return &guard{
		limits:    limits,
		now:       time.Now,
		addresses: map[string]*addressState{},
		enrolls:   map[string]*rate.Limiter{},
		devices:   map[string]*rate.Limiter{},
	}
}

func hostOf(address net.Addr) string {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return address.String()
	}

	return host
}

func (g *guard) state(host string) *addressState {
	value := g.addresses[host]
	if value == nil {
		value = &addressState{limiter: rate.NewLimiter(g.limits.ConnectionRate, g.limits.ConnectionBurst)}
		g.addresses[host] = value
	}
	value.seen = g.now()

	return value
}

// admit decides whether a new connection from host may start a handshake.
func (g *guard) admit(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	value := g.state(host)
	switch {
	case g.now().Before(value.banned):
		return false
	case g.open >= g.limits.MaxConnections, value.open >= g.limits.MaxConnectionsPerIP:
		return false
	case !value.limiter.Allow():
		return false
	}

	g.open++
	value.open++

	return true
}

func (g *guard) release(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.open--
	if value := g.addresses[host]; value != nil {
		value.open--
	}
}

// failed records a failed handshake and bans the address after too many.
func (g *guard) failed(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	value := g.state(host)

	kept := value.failures[:0]
	for _, at := range value.failures {
		if now.Sub(at) < g.limits.BanWindow {
			kept = append(kept, at)
		}
	}
	value.failures = append(kept, now)

	if len(value.failures) >= g.limits.FailedHandshakes {
		value.banned = now.Add(g.limits.BanWindow)
		value.failures = nil
	}
}

// allowEnroll bounds join attempts per address.
func (g *guard) allowEnroll(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	limiter := g.enrolls[host]
	if limiter == nil {
		limiter = rate.NewLimiter(g.limits.EnrollRate, g.limits.EnrollBurst)
		g.enrolls[host] = limiter
	}

	return limiter.Allow()
}

// allowRequest bounds requests per device.
func (g *guard) allowRequest(deviceID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	limiter := g.devices[deviceID]
	if limiter == nil {
		limiter = rate.NewLimiter(g.limits.RequestRate, g.limits.RequestBurst)
		g.devices[deviceID] = limiter
	}

	return limiter.Allow()
}

// sweep forgets idle addresses so the maps do not grow without bound.
func (g *guard) sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	for host, value := range g.addresses {
		if value.open == 0 && now.After(value.banned) && now.Sub(value.seen) > g.limits.BanWindow {
			delete(g.addresses, host)
			delete(g.enrolls, host)
		}
	}
}

// guardedListener accepts TCP connections, applies the address limits and
// completes the TLS handshake before handing a connection to the HTTP server,
// so failed handshakes are counted per address.
type guardedListener struct {
	inner  net.Listener
	config *tls.Config
	guard  *guard

	ready  chan net.Conn
	closed chan struct{}
	once   sync.Once
	err    error

	mu    sync.Mutex
	hosts map[net.Conn]string
}

func newGuardedListener(ctx context.Context, inner net.Listener, config *tls.Config, guard *guard) *guardedListener {
	listener := &guardedListener{inner: inner, config: config, guard: guard, ready: make(chan net.Conn), closed: make(chan struct{}), hosts: map[net.Conn]string{}}
	go listener.acceptLoop(ctx)
	go listener.sweepLoop()

	return listener
}

func (l *guardedListener) acceptLoop(ctx context.Context) {
	for {
		raw, err := l.inner.Accept()
		if err != nil {
			var temporary interface{ Temporary() bool }
			if errors.As(err, &temporary) && temporary.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}

			_ = l.closeWith(err)
			return
		}

		host := hostOf(raw.RemoteAddr())
		if !l.guard.admit(host) {
			_ = raw.Close()
			continue
		}

		go l.handshake(ctx, raw, host)
	}
}

func (l *guardedListener) handshake(ctx context.Context, raw net.Conn, host string) {
	conn := tls.Server(raw, l.config)

	handshakeCtx, cancel := context.WithTimeout(ctx, l.guard.limits.HandshakeTimeout)
	err := conn.HandshakeContext(handshakeCtx)
	cancel()

	if err != nil {
		l.guard.failed(host)
		_ = conn.Close()
		l.guard.release(host)
		return
	}

	// The HTTP server needs the *tls.Conn itself for HTTP/2 and the peer
	// certificate, so the slot is released from its ConnState callback.
	l.mu.Lock()
	l.hosts[conn] = host
	l.mu.Unlock()

	select {
	case l.ready <- conn:
	case <-l.closed:
		_ = conn.Close()
		l.connClosed(conn)
	}
}

// connState releases the address slot of a connection the HTTP server is done with.
func (l *guardedListener) connState(conn net.Conn, state http.ConnState) {
	if state == http.StateClosed || state == http.StateHijacked {
		l.connClosed(conn)
	}
}

func (l *guardedListener) connClosed(conn net.Conn) {
	l.mu.Lock()
	host, ok := l.hosts[conn]
	delete(l.hosts, conn)
	l.mu.Unlock()

	if ok {
		l.guard.release(host)
	}
}

func (l *guardedListener) sweepLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-l.closed:
			return
		case <-ticker.C:
			l.guard.sweep()
		}
	}
}

func (l *guardedListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ready:
		return conn, nil
	case <-l.closed:
		if l.err != nil {
			return nil, l.err
		}
		return nil, net.ErrClosed
	}
}

func (l *guardedListener) Close() error { return l.closeWith(nil) }

// closeWith records why the listener stopped before Accept can observe it.
func (l *guardedListener) closeWith(cause error) error {
	var err error
	l.once.Do(func() {
		l.err = cause
		close(l.closed)
		err = l.inner.Close()
	})

	return err
}

func (l *guardedListener) Addr() net.Addr { return l.inner.Addr() }
