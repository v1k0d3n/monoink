package sysinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic /proc/bus/input/devices entries modelled on what SteamOS
// reports (no real device names, paths or serial numbers).
const (
	keyboard = `I: Bus=0003 Vendor=1234 Product=0001 Version=0111
N: Name="Example Keyboard"
P: Phys=usb-0000:00:00.0-1/input0
S: Sysfs=/devices/pci0000:00/0000:00:00.0/usb1/1-1/1-1:1.0/0003:1234:0001.0001/input/input1
U: Uniq=SERIAL-SHOULD-BE-IGNORED
H: Handlers=sysrq kbd leds event1
B: EV=120013
`
	// The Steam Controller wireless puck: several HID interfaces that are
	// present whether or not a controller is on.
	puck = `I: Bus=0003 Vendor=28de Product=1305 Version=0111
N: Name="Valve Software Steam Controller Puck"
P: Phys=usb-0000:00:00.0-3/input2
S: Sysfs=/devices/pci0000:00/0000:00:00.0/usb1/1-3/1-3:1.2/0003:28DE:1305.000C/input/input56
U: Uniq=SERIAL-SHOULD-BE-IGNORED
H: Handlers=sysrq kbd event3 mouse1
B: EV=100017
`
	steamPad0 = `I: Bus=0003 Vendor=045e Product=028e Version=0001
N: Name="Microsoft X-Box 360 pad 0"
P: Phys=
S: Sysfs=/devices/virtual/input/input82
U: Uniq=
H: Handlers=event9 js0
B: EV=20000b
`
	steamPad1 = `I: Bus=0003 Vendor=045e Product=028e Version=0001
N: Name="Microsoft X-Box 360 pad 1"
P: Phys=
S: Sysfs=/devices/virtual/input/input83
U: Uniq=
H: Handlers=event10 js1
B: EV=20000b
`
	bluetoothPad = `I: Bus=0005 Vendor=054c Product=0ce6 Version=8100
N: Name="Wireless Controller"
P: Phys=00:00:00:00:00:00
S: Sysfs=/devices/virtual/misc/uhid/0005:054C:0CE6.0010/input/input90
U: Uniq=SERIAL-SHOULD-BE-IGNORED
H: Handlers=event21 js2
B: EV=20000b
`
	usbPad = `I: Bus=0003 Vendor=045e Product=0b12 Version=0511
N: Name="Microsoft Xbox Controller"
P: Phys=usb-0000:00:00.0-4/input0
S: Sysfs=/devices/pci0000:00/0000:00:00.0/usb1/1-4/1-4:1.0/input/input91
U: Uniq=SERIAL-SHOULD-BE-IGNORED
H: Handlers=event22 js3
B: EV=20000b
`
	deckBuiltIn = `I: Bus=0003 Vendor=28de Product=1205 Version=0111
N: Name="Steam Deck"
P: Phys=usb-0000:00:00.0-3/input2
S: Sysfs=/devices/pci0000:00/0000:00:00.0/usb3/3-3/3-3:1.2/0003:28DE:1205.0003/input/input12
U: Uniq=
H: Handlers=event13 js0
B: EV=20000b
`
)

func devices(entries ...string) string { return strings.Join(entries, "\n") }

func TestCountControllers(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"puck with controller off", devices(keyboard, puck), 0},
		{"puck controller on (Steam virtual pad)", devices(keyboard, puck, steamPad0), 1},
		{"two Steam-managed controllers", devices(puck, steamPad0, steamPad1), 2},
		{"USB pad without Steam Input", devices(keyboard, usbPad), 1},
		{"USB pad wrapped by Steam is not double-counted", devices(usbPad, steamPad0), 1},
		{"Steam Deck built-in controls are not external", devices(deckBuiltIn, steamPad0), 0},
		{"Steam Deck plus one external pad", devices(deckBuiltIn, steamPad0, steamPad1), 1},
	}
	for _, c := range cases {
		if got := countControllers(parseInputDevices(strings.NewReader(c.in))); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestBluetoothPads(t *testing.T) {
	// Bluetooth pads arrive through uhid (/devices/virtual/misc/uhid/...),
	// which is real hardware despite the "virtual" in the path.
	if got := countControllers(parseInputDevices(strings.NewReader(devices(bluetoothPad)))); got != 1 {
		t.Errorf("Bluetooth pad without Steam Input: got %d, want 1", got)
	}
	if got := countControllers(parseInputDevices(strings.NewReader(devices(bluetoothPad, steamPad0)))); got != 1 {
		t.Errorf("Bluetooth pad wrapped by Steam is not double-counted: got %d, want 1", got)
	}
}

func TestParserIgnoresSerialNumbers(t *testing.T) {
	for _, d := range parseInputDevices(strings.NewReader(devices(keyboard, puck, usbPad))) {
		for _, f := range []string{d.Name, d.Sysfs, d.Handlers, d.Vendor, d.Product} {
			if strings.Contains(f, "SERIAL") {
				t.Fatalf("serial number leaked into parsed fields: %+v", d)
			}
		}
	}
}

func TestSamplerControllers(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "proc", "bus", "input", "devices")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(devices(keyboard, puck, steamPad0)), 0o644)
	if got := (&Sampler{Root: root}).Controllers(); got != 1 {
		t.Fatalf("got %d", got)
	}
	if got := (&Sampler{Root: t.TempDir()}).Controllers(); got != 0 {
		t.Fatalf("missing file should mean 0, got %d", got)
	}
}
