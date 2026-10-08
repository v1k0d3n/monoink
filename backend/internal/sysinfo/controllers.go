package sysinfo

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// inputDevice is the subset of a /proc/bus/input/devices entry used to
// recognize game controllers. Serial numbers (U: Uniq=) are never read.
type inputDevice struct {
	Name     string
	Sysfs    string
	Handlers string
	Vendor   string
	Product  string
}

func (d inputDevice) joystick() bool {
	for _, h := range strings.Fields(d.Handlers) {
		if strings.HasPrefix(h, "js") {
			return true
		}
	}
	return false
}

// uinput: a software-created input device (e.g. Steam Input's virtual
// pads). Bluetooth controllers arrive through uhid, under
// /devices/virtual/misc/uhid/, and are real hardware, so only the
// /devices/virtual/input/ prefix counts here.
func (d inputDevice) uinput() bool { return strings.HasPrefix(d.Sysfs, "/devices/virtual/input/") }

// steamVirtual: Steam Input presents each controller it manages to games
// as a virtual Xbox 360 pad ("Microsoft X-Box 360 pad 0", "… 1", …),
// created when the controller connects and removed when it disconnects.
// On SteamOS this is the only sign that a Steam Controller connected
// through the wireless puck is on; the puck's own devices never change.
func (d inputDevice) steamVirtual() bool {
	return d.uinput() && d.joystick() &&
		(strings.HasPrefix(d.Name, "Microsoft X-Box 360 pad") || strings.Contains(d.Name, "Steam Virtual Gamepad"))
}

// builtIn: a Steam Deck's own controls, which Steam also wraps in a
// virtual pad but which aren't an external controller.
func (d inputDevice) builtIn() bool {
	return !d.uinput() && d.joystick() &&
		(strings.Contains(d.Name, "Steam Deck") || (d.Vendor == "28de" && d.Product == "1205"))
}

func parseInputDevices(r io.Reader) []inputDevice {
	var out []inputDevice
	var cur inputDevice
	flush := func() {
		if cur.Name != "" || cur.Sysfs != "" {
			out = append(out, cur)
		}
		cur = inputDevice{}
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "I: "):
			for _, f := range strings.Fields(line[3:]) {
				k, v, _ := strings.Cut(f, "=")
				switch k {
				case "Vendor":
					cur.Vendor = strings.ToLower(v)
				case "Product":
					cur.Product = strings.ToLower(v)
				}
			}
		case strings.HasPrefix(line, "N: Name="):
			cur.Name = strings.Trim(strings.TrimPrefix(line, "N: Name="), `"`)
		case strings.HasPrefix(line, "S: Sysfs="):
			cur.Sysfs = strings.TrimPrefix(line, "S: Sysfs=")
		case strings.HasPrefix(line, "H: Handlers="):
			cur.Handlers = strings.TrimPrefix(line, "H: Handlers=")
		}
	}
	flush()
	return out
}

// countControllers returns how many external game controllers are on.
//
// Steam's virtual pads count the controllers Steam manages (all of them
// on SteamOS); physical gamepads (USB/Bluetooth) cover controllers Steam
// isn't handling. A Bluetooth pad can appear as both, so the larger of
// the two counts is used rather than their sum.
func countControllers(devs []inputDevice) int {
	var steam, physical, builtin int
	for _, d := range devs {
		switch {
		case d.steamVirtual():
			steam++
		case d.builtIn():
			builtin++
		case !d.uinput() && d.joystick():
			physical++
		}
	}
	return max(steam-builtin, physical, 0)
}

// Controllers reports the number of external game controllers currently
// connected, or 0 if it can't tell.
func (s *Sampler) Controllers() int {
	f, err := os.Open(filepath.Join(s.Root, "proc", "bus", "input", "devices"))
	if err != nil {
		return 0
	}
	defer f.Close()
	return countControllers(parseInputDevices(f))
}
