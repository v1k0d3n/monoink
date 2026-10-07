// Package conn owns the BLE connection to the faceplate. A single goroutine
// (Run) performs every Bluetooth operation, so there are no cross-process
// locks: callers submit frames and scan requests over channels.
//
// While enabled, the manager never gives up: failures move it to Backoff
// and it retries with exponential delay (2s → 60s).
package conn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/v1k0d3n/monoink/backend/internal/ble"
	"github.com/v1k0d3n/monoink/backend/internal/display"
	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

type State string

const (
	Off        State = "off"        // disabled by the user
	Searching  State = "searching"  // looking for the display
	Connecting State = "connecting" // establishing the GATT link
	Connected  State = "connected"  // link up and idle
	Standby    State = "standby"    // healthy, connects only to send
	Backoff    State = "backoff"    // last attempt failed; will retry
)

// Status is a point-in-time view for the UI.
type Status struct {
	State     State             `json:"state"`
	Reason    string            `json:"reason,omitempty"`
	Address   string            `json:"address,omitempty"`
	Name      string            `json:"name,omitempty"`
	Info      *proto.DeviceInfo `json:"info,omitempty"`
	Sending   bool              `json:"sending"`
	Progress  int               `json:"progress"` // percent of current frame
	LastFrame time.Time         `json:"last_frame,omitempty"`
	RetryAt   time.Time         `json:"retry_at,omitempty"`
	AckMode   string            `json:"ack_mode,omitempty"`
}

// Config is what the manager needs from user settings.
type Config struct {
	Enabled       bool
	Address       string // empty = any faceplate
	KeepConnected bool
}

// Bus and Link abstract BlueZ so the state machine can be tested.
type Bus interface {
	Powered() (bool, error)
	Known() ([]ble.Candidate, error)
	Scan(ctx context.Context, stop func(ble.Candidate) bool) ([]ble.Candidate, error)
	Connect(ctx context.Context, path dbus.ObjectPath) (Link, error)
	Close() error
}

type Link interface {
	display.Link
	Connected() bool
	Close() error
}

// Dialer opens a Bus. BlueZ is used by default.
type Dialer func() (Bus, error)

type bluezBus struct{ *ble.Bus }

func (b bluezBus) Connect(ctx context.Context, p dbus.ObjectPath) (Link, error) {
	l, err := b.Bus.Connect(ctx, p)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// BlueZ dials the system BlueZ daemon.
func BlueZ() (Bus, error) {
	b, err := ble.Open()
	if err != nil {
		return nil, err
	}
	return bluezBus{b}, nil
}

var (
	ErrDisabled = errors.New("display connection is turned off")
	ErrBusy     = errors.New("a frame is already being sent")
)

// Timeouts, overridable in tests.
var (
	scanTimeout    = 12 * time.Second
	connectTimeout = 30 * time.Second
	healthEvery    = 10 * time.Second
	infoEvery      = 10 * time.Minute
	minBackoff     = 2 * time.Second
	maxBackoff     = 60 * time.Second
)

// syncEvery pipelines data packets as unacknowledged writes with an
// acknowledged barrier every N packets. Measured on hardware: ~2.9s per
// frame versus ~6s fully acknowledged (and far less sensitive to other
// Bluetooth activity such as Steam's controller scanning).
const syncEvery = 16

type job struct {
	ctx   context.Context
	frame []byte
	done  chan error
}

type scanReq struct {
	done chan scanResult
}

type scanResult struct {
	found []ble.Candidate
	err   error
}

// Manager is the single owner of the BLE connection.
type Manager struct {
	dial        Dialer
	log         *slog.Logger
	OnChange    func(Status) // called (outside locks) after every status change
	OnConnected func(address string)

	mu        sync.Mutex
	cfg       Config
	st        Status
	wake      chan struct{}
	reconnect chan struct{}
	jobs      chan *job
	scans     chan scanReq
	sending   bool
	lastAddr  string

	// Owned by Run.
	bus      Bus
	link     Link
	sess     *display.Session
	failures int
	retryAt  time.Time
	infoAt   time.Time
	safeMode bool // pipelining failed once; use acknowledged writes only
}

func New(dial Dialer, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		dial:      dial,
		log:       log,
		st:        Status{State: Off},
		wake:      make(chan struct{}, 1),
		reconnect: make(chan struct{}, 1),
		jobs:      make(chan *job),
		scans:     make(chan scanReq),
	}
}

// Configure updates the desired behaviour and wakes the owner loop.
func (m *Manager) Configure(c Config) {
	m.mu.Lock()
	changed := m.cfg != c
	if c.Address != m.cfg.Address {
		m.lastAddr = c.Address
	}
	m.cfg = c
	m.mu.Unlock()
	if changed {
		m.poke()
	}
}

// Reconnect drops any current link and retries immediately.
func (m *Manager) Reconnect() {
	select {
	case m.reconnect <- struct{}{}:
	default:
	}
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Status returns the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.st
	if st.Info != nil {
		info := *st.Info
		st.Info = &info
	}
	return st
}

func (m *Manager) update(fn func(*Status)) {
	m.mu.Lock()
	fn(&m.st)
	st := m.st
	cb := m.OnChange
	m.mu.Unlock()
	if cb != nil {
		cb(st)
	}
}

func (m *Manager) setState(s State, reason string) {
	m.update(func(st *Status) {
		st.State, st.Reason = s, reason
		if s != Backoff {
			st.RetryAt = time.Time{}
		}
	})
}

// Send transmits a packed frame, connecting first if necessary. Only one
// frame may be in flight; callers should coalesce.
func (m *Manager) Send(ctx context.Context, frame []byte) error {
	m.mu.Lock()
	if m.sending {
		m.mu.Unlock()
		return ErrBusy
	}
	m.sending = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.sending = false
		m.mu.Unlock()
	}()

	j := &job{ctx: ctx, frame: frame, done: make(chan error, 1)}
	select {
	case m.jobs <- j:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-j.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Scan lists nearby faceplates (plus a connected one, which can't advertise).
func (m *Manager) Scan(ctx context.Context) ([]ble.Candidate, error) {
	req := scanReq{done: make(chan scanResult, 1)}
	select {
	case m.scans <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-req.done:
		return r.found, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Run owns the connection until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	defer m.closeAll()
	for {
		cfg := m.config()
		var timer <-chan time.Time
		switch {
		case !cfg.Enabled:
			m.drop()
			m.failures = 0
			m.setState(Off, "Turned off")
		case m.link != nil:
			timer = time.After(healthEvery)
		case cfg.KeepConnected:
			if wait := time.Until(m.retryAt); wait > 0 {
				timer = time.After(wait)
			} else {
				m.connect(ctx, cfg)
				continue
			}
		default:
			if m.Status().State != Backoff {
				m.setState(Standby, "Connects when there is something to show")
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			m.retryAt = time.Time{}
		case j := <-m.jobs:
			m.handleJob(ctx, cfg, j)
		case <-m.reconnect:
			m.drop()
			m.failures, m.retryAt = 0, time.Time{}
			if !cfg.KeepConnected && cfg.Enabled {
				m.connect(ctx, cfg) // verify reachability once, then idle
				if m.link != nil {
					m.drop()
					m.setState(Standby, "Connects when there is something to show")
				}
			}
		case r := <-m.scans:
			found, err := m.scan(ctx, cfg)
			r.done <- scanResult{found, err}
		case <-timer:
			m.health(ctx)
		}
	}
}

func (m *Manager) handleJob(ctx context.Context, cfg Config, j *job) {
	if !cfg.Enabled {
		j.done <- ErrDisabled
		return
	}
	if m.link == nil {
		m.connect(ctx, cfg)
		if m.link == nil {
			j.done <- fmt.Errorf("display unavailable: %s", m.Status().Reason)
			return
		}
	}
	m.update(func(st *Status) { st.Sending, st.Progress = true, 0 })
	last := -1
	err := m.sess.SendFrame(j.ctx, j.frame, func(n, total int) {
		if p := n * 100 / total; p/5 != last {
			last = p / 5
			m.update(func(st *Status) { st.Progress = p })
		}
	})
	m.update(func(st *Status) {
		st.Sending = false
		st.AckMode = m.sess.AckMode.String()
		if err == nil {
			st.LastFrame, st.Progress = time.Now(), 100
		}
	})
	m.log.Info("frame transfer", "stats", m.sess.Stats.String())
	if err != nil && m.sess.SyncEvery > 0 && !m.safeMode {
		m.log.Warn("pipelined send failed; falling back to acknowledged writes", "err", err)
		m.safeMode = true
	}
	if err != nil {
		m.log.Warn("send failed", "err", err)
		m.fail("Sending failed: " + err.Error())
	} else if !cfg.KeepConnected {
		m.drop()
		m.setState(Standby, "Connects when there is something to show")
	}
	j.done <- err
}

// connect finds the display and opens a session. On failure it schedules
// a retry and leaves m.link nil.
func (m *Manager) connect(ctx context.Context, cfg Config) {
	if m.bus == nil {
		bus, err := m.dial()
		if err != nil {
			m.fail("Bluetooth service unavailable: " + err.Error())
			return
		}
		m.bus = bus
	}
	if on, err := m.bus.Powered(); err != nil {
		m.resetBus()
		m.fail("Bluetooth adapter unavailable")
		return
	} else if !on {
		m.fail("Bluetooth is turned off")
		return
	}

	m.setState(Searching, "Looking for the display")
	c, err := m.find(ctx, cfg)
	if ctx.Err() != nil {
		return // shutting down; not a failure
	}
	if err != nil {
		m.fail(err.Error())
		return
	}

	m.update(func(st *Status) {
		st.State, st.Reason = Connecting, "Connecting"
		st.Address, st.Name = c.Address, c.Name
		if c.Info != nil {
			st.Info = c.Info
		}
	})
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	link, err := m.bus.Connect(cctx, c.Path)
	if ctx.Err() != nil {
		if link != nil {
			link.Close()
		}
		return
	}
	if err != nil {
		m.fail("Could not connect: " + friendly(err))
		return
	}
	sess := display.New(link)
	info, err := sess.QueryInfo(cctx)
	if err != nil {
		link.Close()
		m.fail("Display did not respond: " + friendly(err))
		return
	}
	if !m.safeMode {
		sess.SyncEvery = syncEvery // applies once the ack mode is known
	}
	m.link, m.sess, m.failures, m.infoAt = link, sess, 0, time.Now()
	m.mu.Lock()
	m.lastAddr = c.Address
	m.mu.Unlock()
	m.log.Info("connected", "address", c.Address, "info", info.String())
	m.update(func(st *Status) {
		st.State, st.Reason, st.Info, st.RetryAt = Connected, "", &info, time.Time{}
	})
	if m.OnConnected != nil {
		m.OnConnected(c.Address)
	}
}

func (m *Manager) find(ctx context.Context, cfg Config) (ble.Candidate, error) {
	m.mu.Lock()
	prefer := cfg.Address
	if prefer == "" {
		prefer = m.lastAddr
	}
	m.mu.Unlock()
	match := func(c ble.Candidate) bool {
		return cfg.Address == "" || strings.EqualFold(c.Address, cfg.Address)
	}

	// A connected display doesn't advertise; take it straight from BlueZ.
	known, err := m.bus.Known()
	if err != nil {
		m.resetBus()
		return ble.Candidate{}, fmt.Errorf("Bluetooth service error: %s", friendly(err))
	}
	for _, c := range known {
		if c.Connected && match(c) {
			return c, nil
		}
	}

	sctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	found, err := m.bus.Scan(sctx, match)
	if err != nil {
		return ble.Candidate{}, fmt.Errorf("Scan failed: %s", friendly(err))
	}
	var best *ble.Candidate
	for i, c := range found {
		if !match(c) {
			continue
		}
		if strings.EqualFold(c.Address, prefer) {
			return c, nil
		}
		if best == nil || (c.HasRSSI && (!best.HasRSSI || c.RSSI > best.RSSI)) {
			best = &found[i]
		}
	}
	if best == nil {
		if cfg.Address != "" {
			return ble.Candidate{}, fmt.Errorf("Display %s not found — is it powered on?", cfg.Address)
		}
		return ble.Candidate{}, errors.New("Display not found — is it powered on and in range?")
	}
	return *best, nil
}

func (m *Manager) scan(ctx context.Context, cfg Config) ([]ble.Candidate, error) {
	if m.bus == nil {
		bus, err := m.dial()
		if err != nil {
			return nil, err
		}
		m.bus = bus
	}
	sctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	found, err := m.bus.Scan(sctx, nil)
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (m *Manager) health(ctx context.Context) {
	if m.link == nil {
		return
	}
	if !m.link.Connected() {
		m.log.Info("link lost")
		m.drop()
		m.retryAt = time.Time{} // reconnect right away the first time
		m.setState(Backoff, "Connection lost; reconnecting")
		return
	}
	if time.Since(m.infoAt) >= infoEvery {
		qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		info, err := m.sess.QueryInfo(qctx)
		if err != nil {
			m.fail("Display stopped responding")
			return
		}
		m.infoAt = time.Now()
		m.update(func(st *Status) { st.Info = &info })
	}
}

func (m *Manager) fail(reason string) {
	m.drop()
	delay := minBackoff << min(m.failures, 6)
	if delay > maxBackoff {
		delay = maxBackoff
	}
	m.failures++
	m.retryAt = time.Now().Add(delay)
	m.log.Info("connection attempt failed", "reason", reason, "retry_in", delay)
	m.update(func(st *Status) {
		st.State, st.Reason, st.RetryAt, st.Sending = Backoff, reason, m.retryAt, false
	})
}

func (m *Manager) drop() {
	if m.link != nil {
		m.link.Close()
		m.link, m.sess = nil, nil
	}
}

func (m *Manager) resetBus() {
	m.drop()
	if m.bus != nil {
		m.bus.Close()
		m.bus = nil
	}
}

func (m *Manager) closeAll() {
	m.resetBus()
	m.setState(Off, "Stopped")
}

// friendly strips D-Bus error prefixes into something readable.
func friendly(err error) string {
	var de dbus.Error
	if errors.As(err, &de) {
		name := de.Name[strings.LastIndex(de.Name, ".")+1:]
		if len(de.Body) > 0 {
			if s, ok := de.Body[0].(string); ok && s != "" {
				return s
			}
		}
		return name
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}
