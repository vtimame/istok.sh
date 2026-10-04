package webui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	changePollInterval = 500 * time.Millisecond
	eventHeartbeat     = 25 * time.Second
)

// ChangeSource reports whether the database changed since the last check.
type ChangeSource interface {
	Changed(ctx context.Context) (bool, error)
}

// broker turns database changes into notifications for every open event
// stream. One watcher polls per process however many browser tabs listen,
// and only while at least one does.
type broker struct {
	newSource func(context.Context) (ChangeSource, error)
	interval  time.Duration

	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
	done        chan struct{}
	closeOnce   sync.Once
}

// newChangeBroker watches the read model's database for commits.
func newChangeBroker(readModel ReadModel) *broker {
	return newBroker(func(ctx context.Context) (ChangeSource, error) {
		return readModel.NewChangeWatcher(ctx)
	}, changePollInterval)
}

func newBroker(newSource func(context.Context) (ChangeSource, error), interval time.Duration) *broker {
	return &broker{
		newSource:   newSource,
		interval:    interval,
		subscribers: make(map[chan struct{}]struct{}),
		done:        make(chan struct{}),
	}
}

// subscribe returns a channel that receives a signal after changes. It holds
// at most one pending signal, so a slow client gets a single "changed"
// instead of a backlog.
func (b *broker) subscribe() (<-chan struct{}, func()) {
	channel := make(chan struct{}, 1)

	b.mu.Lock()
	b.subscribers[channel] = struct{}{}
	b.mu.Unlock()

	return channel, func() {
		b.mu.Lock()
		delete(b.subscribers, channel)
		b.mu.Unlock()
	}
}

// publish notifies every subscriber without blocking. Writes made by this
// process do not move data_version, so mutation handlers call it directly.
func (b *broker) publish() {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for channel := range b.subscribers {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func (b *broker) listening() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.subscribers) > 0
}

// run polls for changes until ctx ends, then closes every event stream so the
// HTTP server can shut down instead of waiting on long-lived responses.
func (b *broker) run(ctx context.Context) {
	defer b.close()

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	var source ChangeSource
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if !b.listening() {
			// A fresh source after idle time starts from the current version,
			// so changes nobody watched are not replayed.
			source = nil
			continue
		}

		if source == nil {
			var err error
			if source, err = b.newSource(ctx); err != nil {
				continue
			}
		}

		changed, err := source.Changed(ctx)
		if err != nil {
			source = nil
			continue
		}
		if changed {
			b.publish()
		}
	}
}

func (b *broker) close() {
	b.closeOnce.Do(func() { close(b.done) })
}

// events streams change notifications as Server-Sent Events. Clients refetch
// what they show on each "change"; the payload carries no data on purpose.
func (a api) events(w http.ResponseWriter, r *http.Request) {
	if a.broker == nil {
		writeError(w, http.StatusNotFound, "not_found", "events are not available")
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	changes, unsubscribe := a.broker.subscribe()
	defer unsubscribe()

	controller := http.NewResponseController(w)
	send := func(message string) bool {
		if _, err := io.WriteString(w, message); err != nil {
			return false
		}
		return controller.Flush() == nil
	}

	if !send("retry: 3000\nevent: ready\ndata: {}\n\n") {
		return
	}

	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()

	for {
		var message string
		select {
		case <-r.Context().Done():
			return
		case <-a.broker.done:
			return
		case <-changes:
			message = "event: change\ndata: {}\n\n"
		case <-heartbeat.C:
			message = fmt.Sprintf(": ping %d\n\n", time.Now().Unix())
		}

		if !send(message) {
			return
		}
	}
}
