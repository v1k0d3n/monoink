package conn

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/v1k0d3n/monoink/backend/internal/ble"
	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

func init() {
	scanTimeout = 50 * time.Millisecond
	healthEvery = 20 * time.Millisecond
	minBackoff = 10 * time.Millisecond
	maxBackoff = 40 * time.Millisecond
}

type fakeBus struct {
	present  atomic.Bool // display advertising / reachable
	links    atomic.Int32
	lastLink atomic.Pointer[fakeLink]
}

var cand = ble.Candidate{Path: "/org/bluez/hci0/dev_X", Address: "00:11:22:33:44:55", Name: proto.AdvertisedName}

func (b *fakeBus) Powered() (bool, error)          { return true, nil }
func (b *fakeBus) Close() error                    { return nil }
func (b *fakeBus) Known() ([]ble.Candidate, error) { return nil, nil }
func (b *fakeBus) Scan(ctx context.Context, stop func(ble.Candidate) bool) ([]ble.Candidate, error) {
	if b.present.Load() {
		return []ble.Candidate{cand}, nil
	}
	<-ctx.Done()
	return nil, nil
}
func (b *fakeBus) Connect(ctx context.Context, p dbus.ObjectPath) (Link, error) {
	if !b.present.Load() {
		return nil, errors.New("le-connection-abort-by-local")
	}
	b.links.Add(1)
	l := &fakeLink{ch: make(chan []byte, 4)}
	l.up.Store(true)
	b.lastLink.Store(l)
	return l, nil
}

type fakeLink struct {
	up      atomic.Bool
	packets atomic.Int32
	acked   atomic.Int32
	ch      chan []byte
}

func (l *fakeLink) Notifications() <-chan []byte { return l.ch }
func (l *fakeLink) Drain()                       {}
func (l *fakeLink) Connected() bool              { return l.up.Load() }
func (l *fakeLink) Close() error                 { l.up.Store(false); return nil }
func (l *fakeLink) Write(_ context.Context, b []byte, withResponse bool) error {
	if !l.up.Load() {
		return errors.New("not connected")
	}
	if b[1] == proto.CmdQueryInfo {
		l.ch <- []byte{0x91, 0x00, 0x01, 0x00, 0x02, 0x88, 0x01, 0xE0, 0x00, 0x6C, 0x50, 0x19}
	} else {
		l.packets.Add(1)
		if withResponse {
			l.acked.Add(1)
		}
	}
	return nil
}

func waitState(t *testing.T, m *Manager, want State) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.Status(); st.State == want {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state %q never reached; last %+v", want, m.Status())
	return Status{}
}

func start(t *testing.T, bus *fakeBus, cfg Config) *Manager {
	m := New(func() (Bus, error) { return bus, nil }, nil)
	m.Configure(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go m.Run(ctx)
	return m
}

func TestRecoversWithoutUserAction(t *testing.T) {
	bus := &fakeBus{}
	m := start(t, bus, Config{Enabled: true, KeepConnected: true})

	st := waitState(t, m, Backoff)
	if st.Reason == "" || st.RetryAt.IsZero() {
		t.Errorf("backoff should explain itself: %+v", st)
	}

	bus.present.Store(true) // display switched on
	st = waitState(t, m, Connected)
	if err := m.Send(context.Background(), make([]byte, proto.FrameSize)); err != nil {
		t.Fatal(err)
	}
	if err := m.Send(context.Background(), make([]byte, proto.FrameSize)); err != nil {
		t.Fatal(err)
	}
	l := bus.lastLink.Load()
	if l.packets.Load() != 720 || l.acked.Load() >= 720 {
		t.Errorf("second frame should be pipelined: %d packets, %d acked", l.packets.Load(), l.acked.Load())
	}
	if st.Info == nil || st.Info.Battery != 80 {
		t.Errorf("info not populated: %+v", st)
	}

	bus.lastLink.Load().up.Store(false) // display went to sleep
	deadline := time.Now().Add(3 * time.Second)
	for bus.links.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := bus.links.Load(); n < 2 {
		t.Fatalf("expected a reconnect, got %d links", n)
	}
	waitState(t, m, Connected)
}

func TestSendInStandbyConnectsThenReleases(t *testing.T) {
	bus := &fakeBus{}
	bus.present.Store(true)
	m := start(t, bus, Config{Enabled: true, KeepConnected: false})
	waitState(t, m, Standby)

	if err := m.Send(context.Background(), make([]byte, proto.FrameSize)); err != nil {
		t.Fatal(err)
	}
	l := bus.lastLink.Load()
	if l.packets.Load() != 360 {
		t.Errorf("sent %d packets", l.packets.Load())
	}
	if l.up.Load() {
		t.Error("standby mode should disconnect after sending")
	}
	if st := waitState(t, m, Standby); st.LastFrame.IsZero() {
		t.Error("last frame time not recorded")
	}
}

func TestDisabled(t *testing.T) {
	bus := &fakeBus{}
	bus.present.Store(true)
	m := start(t, bus, Config{Enabled: false})
	waitState(t, m, Off)
	if err := m.Send(context.Background(), nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("got %v", err)
	}
	m.Configure(Config{Enabled: true, KeepConnected: true})
	waitState(t, m, Connected)
}
