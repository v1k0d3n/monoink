// Package ble talks to BlueZ over the system D-Bus. It needs no special
// privileges: BlueZ's default D-Bus policy lets any local user drive LE
// GATT clients, and the faceplate does not require pairing.
package ble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

const (
	bluez         = "org.bluez"
	ifaceAdapter  = "org.bluez.Adapter1"
	ifaceDevice   = "org.bluez.Device1"
	ifaceGattChar = "org.bluez.GattCharacteristic1"
	ifaceProps    = "org.freedesktop.DBus.Properties"
	ifaceObjMgr   = "org.freedesktop.DBus.ObjectManager"
)

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// Bus is a connection to BlueZ bound to one adapter.
type Bus struct {
	conn    *dbus.Conn
	Adapter dbus.ObjectPath
}

// Open connects to the system bus and picks the first BlueZ adapter.
func Open() (*Bus, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}
	b := &Bus{conn: conn}
	objs, err := b.managed()
	if err != nil {
		conn.Close()
		return nil, err
	}
	for path, ifaces := range objs {
		if _, ok := ifaces[ifaceAdapter]; ok && (b.Adapter == "" || path < b.Adapter) {
			b.Adapter = path
		}
	}
	if b.Adapter == "" {
		conn.Close()
		return nil, errors.New("no Bluetooth adapter found")
	}
	return b, nil
}

func (b *Bus) Close() error { return b.conn.Close() }

func (b *Bus) managed() (managedObjects, error) {
	var objs managedObjects
	err := b.conn.Object(bluez, "/").Call(ifaceObjMgr+".GetManagedObjects", 0).Store(&objs)
	if err != nil {
		return nil, fmt.Errorf("query BlueZ objects (is bluetoothd running?): %w", err)
	}
	return objs, nil
}

// Powered reports whether the adapter is powered on.
func (b *Bus) Powered() (bool, error) {
	v, err := b.conn.Object(bluez, b.Adapter).GetProperty(ifaceAdapter + ".Powered")
	if err != nil {
		return false, err
	}
	p, _ := v.Value().(bool)
	return p, nil
}

// Candidate is a BlueZ device object that looks like the faceplate.
type Candidate struct {
	Path      dbus.ObjectPath
	Address   string
	Name      string
	RSSI      int16
	HasRSSI   bool
	Connected bool
	Info      *proto.DeviceInfo // from advertising data, if present
}

func (c Candidate) String() string {
	s := fmt.Sprintf("%s %q", c.Address, c.Name)
	if c.HasRSSI {
		s += fmt.Sprintf(" rssi=%d", c.RSSI)
	}
	if c.Connected {
		s += " connected"
	}
	if c.Info != nil {
		s += " [" + c.Info.String() + "]"
	}
	return s
}

func (b *Bus) candidate(path dbus.ObjectPath, props map[string]dbus.Variant) (Candidate, bool) {
	if !strings.HasPrefix(string(path), string(b.Adapter)+"/") {
		return Candidate{}, false
	}
	str := func(k string) string { s, _ := props[k].Value().(string); return s }
	c := Candidate{Path: path, Address: str("Address"), Name: str("Name")}
	if c.Name == "" {
		c.Name = str("Alias")
	}
	c.Connected, _ = props["Connected"].Value().(bool)
	if r, ok := props["RSSI"].Value().(int16); ok {
		c.RSSI, c.HasRSSI = r, true
	}
	if md, ok := props["ManufacturerData"].Value().(map[uint16]dbus.Variant); ok {
		for _, v := range md {
			if raw, ok := v.Value().([]byte); ok {
				if info, ok := proto.FindDeviceInfo(raw); ok {
					c.Info = &info
				}
			}
		}
	}
	match := proto.IsDisplayName(c.Name) || c.Info != nil
	if uuids, ok := props["UUIDs"].Value().([]string); ok {
		for _, u := range uuids {
			if strings.EqualFold(u, proto.ServiceUUID) && proto.IsDisplayName(c.Name) {
				match = true
			}
		}
	}
	return c, match
}

// Known returns faceplates BlueZ already knows about (cached from a recent
// scan or currently connected). Connected devices stop advertising, so this
// must be checked before scanning.
func (b *Bus) Known() ([]Candidate, error) {
	objs, err := b.managed()
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for path, ifaces := range objs {
		if props, ok := ifaces[ifaceDevice]; ok {
			if c, ok := b.candidate(path, props); ok {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (b *Bus) deviceProps(path dbus.ObjectPath) (map[string]dbus.Variant, error) {
	var props map[string]dbus.Variant
	err := b.conn.Object(bluez, path).Call(ifaceProps+".GetAll", 0, ifaceDevice).Store(&props)
	return props, err
}

// Scan runs LE discovery until ctx ends or stop returns true for a seen
// faceplate (stop may be nil to scan for the full duration). Discovery is always stopped before returning so we don't compete
// with Steam's own Bluetooth use longer than needed.
func (b *Bus) Scan(ctx context.Context, stop func(Candidate) bool) ([]Candidate, error) {
	// InterfacesAdded is emitted on "/", so it can't be namespaced to the adapter.
	added := []dbus.MatchOption{dbus.WithMatchInterface(ifaceObjMgr), dbus.WithMatchMember("InterfacesAdded")}
	changed := []dbus.MatchOption{dbus.WithMatchPathNamespace(b.Adapter),
		dbus.WithMatchInterface(ifaceProps), dbus.WithMatchMember("PropertiesChanged")}
	for _, m := range [][]dbus.MatchOption{added, changed} {
		if err := b.conn.AddMatchSignal(m...); err != nil {
			return nil, err
		}
		defer b.conn.RemoveMatchSignal(m...)
	}
	sigs := make(chan *dbus.Signal, 128)
	b.conn.Signal(sigs)
	defer b.conn.RemoveSignal(sigs)

	adapter := b.conn.Object(bluez, b.Adapter)
	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("le")}
	if err := adapter.Call(ifaceAdapter+".SetDiscoveryFilter", 0, filter).Err; err != nil {
		return nil, fmt.Errorf("set discovery filter: %w", err)
	}
	if err := adapter.Call(ifaceAdapter+".StartDiscovery", 0).Err; err != nil {
		if !isDBusErr(err, "org.bluez.Error.InProgress") {
			return nil, fmt.Errorf("start discovery: %w", err)
		}
	}
	defer adapter.Call(ifaceAdapter+".StopDiscovery", 0)

	found := map[dbus.ObjectPath]Candidate{}
	consider := func(path dbus.ObjectPath) bool {
		props, err := b.deviceProps(path)
		if err != nil {
			return false
		}
		c, ok := b.candidate(path, props)
		if ok {
			found[path] = c
		}
		return ok && stop != nil && stop(c)
	}
	// Devices already in the cache won't produce InterfacesAdded, but a
	// cached entry may be stale, so only stop early on fresh signal data.
	if known, err := b.Known(); err == nil {
		for _, c := range known {
			found[c.Path] = c
		}
	}
	for {
		select {
		case <-ctx.Done():
			return collect(found), nil
		case s := <-sigs:
			var path dbus.ObjectPath
			switch s.Name {
			case ifaceObjMgr + ".InterfacesAdded":
				if len(s.Body) > 0 {
					path, _ = s.Body[0].(dbus.ObjectPath)
				}
			case ifaceProps + ".PropertiesChanged":
				if len(s.Body) > 0 && s.Body[0] == ifaceDevice {
					path = s.Path
				}
			}
			if path != "" && consider(path) {
				return collect(found), nil
			}
		}
	}
}

func collect(m map[dbus.ObjectPath]Candidate) []Candidate {
	out := make([]Candidate, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	return out
}

func isDBusErr(err error, name string) bool {
	var de dbus.Error
	if errors.As(err, &de) {
		return de.Name == name
	}
	return strings.Contains(err.Error(), name)
}

// waitProp polls a boolean property until it is true or ctx ends.
func (b *Bus) waitProp(ctx context.Context, path dbus.ObjectPath, prop string) error {
	obj := b.conn.Object(bluez, path)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if v, err := obj.GetProperty(prop); err == nil {
			if ok, _ := v.Value().(bool); ok {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %w", prop, ctx.Err())
		case <-t.C:
		}
	}
}
