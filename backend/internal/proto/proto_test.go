package proto

import (
	"bytes"
	"testing"
)

func TestIsDisplayName(t *testing.T) {
	for name, want := range map[string]bool{
		"JSAUX E-INK":     true,
		" jsaux  e-ink ":  true,
		"JSAUX_E-INK":     true,
		"JSAUX E-INK Pro": false,
		"":                false,
	} {
		if got := IsDisplayName(name); got != want {
			t.Errorf("IsDisplayName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestQueryInfoCommand(t *testing.T) {
	if got := QueryInfoCommand(DisplayAB); !bytes.Equal(got, []byte{0xAC, 0x00, 0x02, 0xCA}) {
		t.Fatalf("got % x", got)
	}
}

// Golden bytes match the vendor reference implementation's packet layout.
func TestDataPacket(t *testing.T) {
	got, err := DataPacket(0x0102, []byte{0xFF, 0x00}, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xAC, 0x02, 0x01, 0x02, 0xFF, 0x00, 0x00, 0x00, 0xCA}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x, want % x", got, want)
	}
	if _, err := DataPacket(0x10000, nil, 4); err == nil {
		t.Error("index overflow accepted")
	}
	if _, err := DataPacket(0, make([]byte, 5), 4); err == nil {
		t.Error("oversized payload accepted")
	}
	if _, err := DataPacket(0, nil, 0); err == nil {
		t.Error("zero packet size accepted")
	}
}

func TestDataAck(t *testing.T) {
	cases := []struct {
		in   []byte
		want AckStatus
	}{
		{nil, AckMissing},
		{[]byte{0x91, 0x00, 0x00, 0x19}, AckMissing}, // other command
		{[]byte{0x91, 0x02, 0x00, 0x19}, AckSuccess},
		{[]byte{0x91, 0x02, 0x01, 0x19}, AckRejected},
		{[]byte{0x91, 0x02, 0x05, 0x19}, AckInvalid},
		{[]byte{0x91, 0x02, 0x00}, AckInvalid},
	}
	for _, c := range cases {
		if got := DataAck(c.in); got != c.want {
			t.Errorf("DataAck(% x) = %v, want %v", c.in, got, c.want)
		}
	}
}

var infoFrame = []byte{0x91, 0x00, 0x01, 0x00, 0x02, 0x88, 0x01, 0xE0, 0x00, 0x6C, 0x57, 0x19}

func TestParseDeviceInfo(t *testing.T) {
	p, ok := ParseResponse(infoFrame, CmdQueryInfo)
	if !ok {
		t.Fatal("frame rejected")
	}
	info, err := ParseDeviceInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	want := DeviceInfo{DualScreen: false, Width: 648, Height: 480, PacketSize: 108, Battery: 87}
	if info != want {
		t.Fatalf("got %+v, want %+v", info, want)
	}
	if !info.IsTarget() {
		t.Error("expected target device")
	}
}

func TestFindDeviceInfo(t *testing.T) {
	adv := append([]byte{0x12, 0x34}, infoFrame...)
	info, ok := FindDeviceInfo(adv)
	if !ok || info.Width != 648 {
		t.Fatalf("got %+v ok=%v", info, ok)
	}
	if _, ok := FindDeviceInfo([]byte{0x91, 0x00, 0x01}); ok {
		t.Error("short data accepted")
	}
}
