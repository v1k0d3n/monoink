// Package proto implements the JSAUX E-Ink faceplate BLE wire protocol.
//
// The device exposes a Nordic-UART-style GATT service. The host writes
// command frames (0xAC <cmd> ... 0xCA) to the write characteristic and the
// device answers with notification frames (0x91 <cmd> ... 0x19).
package proto

import (
	"errors"
	"fmt"
	"strings"
)

// GATT UUIDs (lower case, as BlueZ reports them).
const (
	ServiceUUID = "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
	WriteUUID   = "6e400002-b5a3-f393-e0a9-e50e24dcca9e"
	NotifyUUID  = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"
)

// Frame delimiters.
const (
	HeaderCmd byte = 0xAC
	FooterCmd byte = 0xCA
	HeaderRsp byte = 0x91
	FooterRsp byte = 0x19
)

// Command codes.
const (
	CmdQueryInfo byte = 0x00
	CmdSendData  byte = 0x02
	CmdOTA       byte = 0x04
)

// Display targets for CmdQueryInfo.
const (
	DisplayA  byte = 0x00
	DisplayB  byte = 0x01
	DisplayAB byte = 0x02
)

// Geometry of the 5.83" monochrome faceplate panel.
const (
	Width     = 648
	Height    = 480
	FrameSize = Width / 8 * Height // 38880 bytes at 1 bpp
)

// AdvertisedName is the BLE local name the faceplate advertises.
const AdvertisedName = "JSAUX E-INK"

// IsDisplayName reports whether a BLE name belongs to the faceplate,
// tolerating case, surrounding space and underscores (legacy firmware).
func IsDisplayName(name string) bool {
	n := strings.ToUpper(strings.ReplaceAll(name, "_", " "))
	return strings.Join(strings.Fields(n), " ") == AdvertisedName
}

// QueryInfoCommand builds the device-info request frame.
func QueryInfoCommand(display byte) []byte {
	return []byte{HeaderCmd, CmdQueryInfo, display, FooterCmd}
}

// ParseResponse returns the payload of a complete response frame for cmd.
func ParseResponse(frame []byte, cmd byte) ([]byte, bool) {
	if len(frame) < 4 || frame[0] != HeaderRsp || frame[1] != cmd || frame[len(frame)-1] != FooterRsp {
		return nil, false
	}
	return frame[2 : len(frame)-1], true
}

// AckStatus classifies a data-packet acknowledgement notification.
type AckStatus int

const (
	AckMissing AckStatus = iota
	AckSuccess
	AckRejected
	AckInvalid
)

func (s AckStatus) String() string {
	return [...]string{"missing", "success", "rejected", "invalid"}[s]
}

// DataAck classifies a notification received after a data packet.
func DataAck(frame []byte) AckStatus {
	if len(frame) < 2 || frame[0] != HeaderRsp || frame[1] != CmdSendData {
		return AckMissing
	}
	payload, ok := ParseResponse(frame, CmdSendData)
	switch {
	case ok && len(payload) == 1 && payload[0] == 0x00:
		return AckSuccess
	case ok && len(payload) == 1 && payload[0] == 0x01:
		return AckRejected
	default:
		return AckInvalid
	}
}

// DataPacket builds one display-data frame, zero-padding payload to size.
func DataPacket(index int, payload []byte, size int) ([]byte, error) {
	if index < 0 || index > 0xFFFF {
		return nil, errors.New("packet index is outside the protocol range")
	}
	if size <= 0 || len(payload) > size {
		return nil, errors.New("invalid display payload length")
	}
	pkt := make([]byte, 0, size+5)
	pkt = append(pkt, HeaderCmd, CmdSendData, byte(index>>8), byte(index))
	pkt = append(pkt, payload...)
	pkt = append(pkt, make([]byte, size-len(payload))...)
	return append(pkt, FooterCmd), nil
}

// DeviceInfo is the decoded CmdQueryInfo response.
type DeviceInfo struct {
	DualScreen bool
	ColorMode  byte // 0=2 colours, 1=3, 2=4, 4=6, 8=7
	Width      int
	Height     int
	PacketSize int // payload bytes per data packet
	Battery    int // percent
}

// Colors returns the number of colours the panel supports.
func (d DeviceInfo) Colors() int {
	switch d.ColorMode {
	case 1:
		return 3
	case 2:
		return 4
	case 4:
		return 6
	case 8:
		return 7
	}
	return 2
}

// IsTarget reports whether the device is the single 648x480 mono faceplate.
func (d DeviceInfo) IsTarget() bool {
	return !d.DualScreen && d.ColorMode == 0 && d.Width == Width && d.Height == Height
}

func (d DeviceInfo) String() string {
	screens := "single"
	if d.DualScreen {
		screens = "dual"
	}
	return fmt.Sprintf("%dx%d %s-screen %d-colour, packet %dB, battery %d%%",
		d.Width, d.Height, screens, d.Colors(), d.PacketSize, d.Battery)
}

// ParseDeviceInfo decodes a 9-byte query-info payload.
func ParseDeviceInfo(p []byte) (DeviceInfo, error) {
	if len(p) != 9 {
		return DeviceInfo{}, fmt.Errorf("device info payload is %d bytes, want 9", len(p))
	}
	bat := int(p[8])
	if bat > 100 {
		bat = 100
	}
	return DeviceInfo{
		DualScreen: p[0] == 0x00,
		ColorMode:  p[1],
		Width:      int(p[2])<<8 | int(p[3]),
		Height:     int(p[4])<<8 | int(p[5]),
		PacketSize: int(p[6])<<8 | int(p[7]),
		Battery:    bat,
	}, nil
}

// FindDeviceInfo locates an embedded query-info response (e.g. inside BLE
// manufacturer data) and decodes it.
func FindDeviceInfo(raw []byte) (DeviceInfo, bool) {
	for i := 0; i+12 <= len(raw); i++ {
		if raw[i] != HeaderRsp || raw[i+1] != CmdQueryInfo {
			continue
		}
		if p, ok := ParseResponse(raw[i:i+12], CmdQueryInfo); ok {
			if info, err := ParseDeviceInfo(p); err == nil {
				return info, true
			}
		}
		return DeviceInfo{}, false
	}
	return DeviceInfo{}, false
}
