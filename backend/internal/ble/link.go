package ble

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

// Link is an open GATT connection to the faceplate with notifications on.
type Link struct {
	bus      *Bus
	Device   dbus.ObjectPath
	writeCh  dbus.ObjectPath
	notifyCh dbus.ObjectPath
	MTU      uint16

	sigs     chan *dbus.Signal
	match    []dbus.MatchOption
	notify   chan []byte
	done     chan struct{}
	closeOne sync.Once
}

// Connect connects to a device, waits for GATT service resolution, locates
// the UART characteristics and enables notifications.
func (b *Bus) Connect(ctx context.Context, path dbus.ObjectPath) (*Link, error) {
	dev := b.conn.Object(bluez, path)
	if err := dev.CallWithContext(ctx, ifaceDevice+".Connect", 0).Err; err != nil &&
		!isDBusErr(err, "org.bluez.Error.AlreadyConnected") {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := b.waitProp(ctx, path, ifaceDevice+".ServicesResolved"); err != nil {
		dev.Call(ifaceDevice+".Disconnect", 0)
		return nil, err
	}
	l := &Link{bus: b, Device: path, notify: make(chan []byte, 32), done: make(chan struct{})}
	objs, err := b.managed()
	if err != nil {
		return nil, err
	}
	for p, ifaces := range objs {
		props, ok := ifaces[ifaceGattChar]
		if !ok || !strings.HasPrefix(string(p), string(path)+"/") {
			continue
		}
		uuid, _ := props["UUID"].Value().(string)
		switch strings.ToLower(uuid) {
		case proto.WriteUUID:
			l.writeCh = p
			l.MTU, _ = props["MTU"].Value().(uint16)
		case proto.NotifyUUID:
			l.notifyCh = p
		}
	}
	if l.writeCh == "" || l.notifyCh == "" {
		dev.Call(ifaceDevice+".Disconnect", 0)
		return nil, fmt.Errorf("device %s lacks the e-ink UART service", path)
	}

	l.match = []dbus.MatchOption{
		dbus.WithMatchObjectPath(l.notifyCh),
		dbus.WithMatchInterface(ifaceProps),
		dbus.WithMatchMember("PropertiesChanged"),
	}
	if err := b.conn.AddMatchSignal(l.match...); err != nil {
		return nil, err
	}
	l.sigs = make(chan *dbus.Signal, 64)
	b.conn.Signal(l.sigs)
	go l.pump()

	if err := b.conn.Object(bluez, l.notifyCh).CallWithContext(ctx, ifaceGattChar+".StartNotify", 0).Err; err != nil {
		l.Close()
		return nil, fmt.Errorf("enable notifications: %w", err)
	}
	return l, nil
}

func (l *Link) pump() {
	for {
		select {
		case <-l.done:
			return
		case s, ok := <-l.sigs:
			if !ok {
				return
			}
			if s.Path != l.notifyCh || len(s.Body) < 2 {
				continue
			}
			changed, _ := s.Body[1].(map[string]dbus.Variant)
			if v, ok := changed["Value"]; ok {
				if b, ok := v.Value().([]byte); ok {
					select {
					case l.notify <- append([]byte(nil), b...):
					default: // drop if nobody is listening
					}
				}
			}
		}
	}
}

// Notifications delivers raw notification payloads from the device.
func (l *Link) Notifications() <-chan []byte { return l.notify }

// Drain discards any queued notifications.
func (l *Link) Drain() {
	for {
		select {
		case <-l.notify:
		default:
			return
		}
	}
}

// Write sends data to the write characteristic. withResponse selects an
// ATT Write Request (acknowledged) instead of a Write Command.
func (l *Link) Write(ctx context.Context, data []byte, withResponse bool) error {
	typ := "command"
	if withResponse {
		typ = "request"
	}
	opts := map[string]dbus.Variant{"type": dbus.MakeVariant(typ)}
	return l.bus.conn.Object(bluez, l.writeCh).CallWithContext(ctx, ifaceGattChar+".WriteValue", 0, data, opts).Err
}

// Connected reports whether BlueZ still considers the link up.
func (l *Link) Connected() bool {
	v, err := l.bus.conn.Object(bluez, l.Device).GetProperty(ifaceDevice + ".Connected")
	if err != nil {
		return false
	}
	ok, _ := v.Value().(bool)
	return ok
}

// Close stops notifications and disconnects.
func (l *Link) Close() error {
	var err error
	l.closeOne.Do(func() {
		conn := l.bus.conn
		conn.Object(bluez, l.notifyCh).Call(ifaceGattChar+".StopNotify", 0)
		if l.sigs != nil {
			conn.RemoveSignal(l.sigs)
			conn.RemoveMatchSignal(l.match...)
		}
		close(l.done)
		err = conn.Object(bluez, l.Device).Call(ifaceDevice+".Disconnect", 0).Err
	})
	return err
}
