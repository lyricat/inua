package hypr

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// Handler receives store output. Calls are made from the store goroutine, one
// at a time; implementations must not block for long.
type Handler interface {
	// OnSnapshot is called after a refresh produced a state different from
	// the previous one.
	OnSnapshot(compositor.Snapshot)
	// OnEvent is called for every raw event, before any refresh it causes.
	OnEvent(Event)
	// OnConnection reports the event stream going up (err == nil) or down.
	OnConnection(connected bool, err error)
}

// Store keeps a Snapshot in sync with Hyprland.
//
// Each session connects the event socket first and only then queries the
// initial state, so a change racing the query is seen as an event and causes
// another refresh. Bursts of events are merged into one refresh.
type Store struct {
	Client   *Client
	Handler  Handler
	Debounce time.Duration // delay between first state event and refresh

	MinBackoff, MaxBackoff time.Duration

	refreshReq chan struct{}

	mu     sync.Mutex
	snap   compositor.Snapshot
	urgent map[string]bool
}

func NewStore(c *Client, h Handler) *Store {
	return &Store{
		Client:     c,
		Handler:    h,
		Debounce:   15 * time.Millisecond,
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		refreshReq: make(chan struct{}, 1),
		urgent:     map[string]bool{},
	}
}

// Refresh asks for a re-query; backs refreshMonitors() and friends.
func (s *Store) Refresh() {
	select {
	case s.refreshReq <- struct{}{}:
	default:
	}
}

// Run blocks until ctx ends, reconnecting with backoff after failures.
func (s *Store) Run(ctx context.Context) error {
	backoff := s.MinBackoff
	for {
		start := time.Now()
		err := s.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.Handler.OnConnection(false, err)
		if time.Since(start) > s.MaxBackoff {
			backoff = s.MinBackoff
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		backoff = min(backoff*2, s.MaxBackoff)
	}
}

func (s *Store) session(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan Event, 256)
	up := make(chan struct{})
	errc := make(chan error, 1)
	go func() { errc <- readEvents(ctx, s.Client.Paths.Event, func() { close(up) }, events) }()

	select {
	case <-up:
	case err := <-errc:
		return err
	}
	if err := s.refresh(ctx); err != nil {
		return err
	}
	s.Handler.OnConnection(true, nil)

	var pending <-chan time.Time
	for {
		select {
		case ev := <-events:
			s.track(ev)
			s.Handler.OnEvent(ev)
			if IsStateEvent(ev.Name) && pending == nil {
				pending = time.After(s.Debounce)
			}
		case <-pending:
			pending = nil
			if err := s.refresh(ctx); err != nil {
				return err
			}
		case <-s.refreshReq:
			pending = nil
			if err := s.refresh(ctx); err != nil {
				return err
			}
		case err := <-errc:
			return err
		}
	}
}

// track updates state that Hyprland only reports through events.
func (s *Store) track(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Name {
	case "urgent":
		s.urgent[NormalizeAddress(ev.Data)] = true
	case "activewindowv2", "closewindow":
		delete(s.urgent, NormalizeAddress(ev.Data))
	}
}

func (s *Store) refresh(ctx context.Context) error {
	q, err := s.Client.Query(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	snap, err := BuildSnapshot(q, s.urgent)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	changed := !reflect.DeepEqual(snap, s.snap)
	s.snap = snap
	s.mu.Unlock()
	if changed {
		s.Handler.OnSnapshot(snap)
	}
	return nil
}
