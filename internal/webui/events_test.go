package webui

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSource struct {
	calls   atomic.Int32
	changes chan bool
}

func (s *fakeSource) Changed(context.Context) (bool, error) {
	s.calls.Add(1)

	select {
	case changed := <-s.changes:
		return changed, nil
	default:
		return false, nil
	}
}

func newFakeBroker(source *fakeSource) *broker {
	return newBroker(func(context.Context) (ChangeSource, error) { return source, nil }, 5*time.Millisecond)
}

func TestBrokerPublishCoalescesPendingSignals(t *testing.T) {
	b := newFakeBroker(&fakeSource{})
	changes, unsubscribe := b.subscribe()
	defer unsubscribe()

	b.publish()
	b.publish()
	b.publish()

	select {
	case <-changes:
	default:
		t.Fatal("subscriber got no signal")
	}
	select {
	case <-changes:
		t.Fatal("subscriber got a backlog instead of one coalesced signal")
	default:
	}
}

func TestBrokerPollsOnlyWhileSomeoneListens(t *testing.T) {
	source := &fakeSource{changes: make(chan bool, 1)}
	b := newFakeBroker(source)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.run(ctx)

	time.Sleep(40 * time.Millisecond)
	if calls := source.calls.Load(); calls != 0 {
		t.Fatalf("source polled %d times with no subscribers", calls)
	}

	changes, unsubscribe := b.subscribe()
	defer unsubscribe()
	source.changes <- true

	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("no signal after the source reported a change")
	}
}

func TestBrokerRetriesAfterSourceErrors(t *testing.T) {
	var attempts atomic.Int32
	source := &fakeSource{changes: make(chan bool, 1)}
	b := newBroker(func(context.Context) (ChangeSource, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("database busy")
		}
		return source, nil
	}, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.run(ctx)

	changes, unsubscribe := b.subscribe()
	defer unsubscribe()
	source.changes <- true

	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("broker did not recover after a failed source")
	}
}

// readEvents collects event names from an SSE stream until it ends.
func readEvents(t *testing.T, response *http.Response, events chan<- string) {
	t.Helper()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if name, found := strings.CutPrefix(scanner.Text(), "event: "); found {
			events <- name
		}
	}
	close(events)
}

func TestEventsStreamsChangesAndClosesOnShutdown(t *testing.T) {
	source := &fakeSource{changes: make(chan bool, 1)}
	b := newFakeBroker(source)
	mux := http.NewServeMux()
	api{broker: b}.register(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var running sync.WaitGroup
	running.Add(1)
	go func() {
		defer running.Done()
		b.run(ctx)
	}()

	response, err := http.Get(server.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}

	events := make(chan string, 8)
	go readEvents(t, response, events)

	expect := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}

	expect("ready")

	source.changes <- true
	expect("change")

	// A mutation in this process signals directly.
	b.publish()
	expect("change")

	// Stopping the server closes open streams instead of holding shutdown.
	cancel()
	running.Wait()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("stream sent another event after shutdown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream stayed open after shutdown")
	}
}
