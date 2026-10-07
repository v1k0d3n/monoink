// Package display speaks the faceplate protocol over an open BLE link.
package display

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

// Link is the transport the session needs; *ble.Link satisfies it.
type Link interface {
	Write(ctx context.Context, data []byte, withResponse bool) error
	Notifications() <-chan []byte
	Drain()
}

// AckMode records how the firmware confirms data packets.
type AckMode int

const (
	AckUnknown AckMode = iota
	AckNotify          // device sends 91 02 00 19 per packet
	AckWrite           // only the ATT write response confirms delivery
)

func (m AckMode) String() string { return [...]string{"unknown", "notify", "write-response"}[m] }

// Session wraps a link with protocol state.
type Session struct {
	link    Link
	Info    *proto.DeviceInfo
	AckMode AckMode

	// SyncEvery > 0 sends data packets as unacknowledged ATT write
	// commands, with an acknowledged write every SyncEvery packets (and
	// for the last one) as a flow-control barrier. Only used once the
	// firmware is known not to ack via notifications.
	SyncEvery int

	// Stats describes the most recent SendFrame, for diagnostics.
	Stats FrameStats
}

// FrameStats breaks down where a frame transfer spent its time.
type FrameStats struct {
	Packets   int
	Total     time.Duration
	Writing   time.Duration // inside ATT writes
	Waiting   time.Duration // waiting for ack notifications
	SlowWrite time.Duration // slowest single write
}

func (f FrameStats) String() string {
	return fmt.Sprintf("%d packets in %s (writes %s, ack waits %s, slowest write %s)",
		f.Packets, f.Total.Round(time.Millisecond), f.Writing.Round(time.Millisecond),
		f.Waiting.Round(time.Millisecond), f.SlowWrite.Round(time.Millisecond))
}

func New(link Link) *Session { return &Session{link: link} }

// waitFor returns the first notification accepted by match, or nil on timeout.
func (s *Session) waitFor(ctx context.Context, d time.Duration, match func([]byte) bool) ([]byte, error) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
			return nil, nil
		case n := <-s.link.Notifications():
			if match(n) {
				return n, nil
			}
		}
	}
}

// QueryInfo asks the device for its geometry, packet size and battery.
func (s *Session) QueryInfo(ctx context.Context) (proto.DeviceInfo, error) {
	s.link.Drain()
	if err := s.link.Write(ctx, proto.QueryInfoCommand(proto.DisplayAB), true); err != nil {
		return proto.DeviceInfo{}, fmt.Errorf("write query: %w", err)
	}
	rsp, err := s.waitFor(ctx, 5*time.Second, func(b []byte) bool {
		_, ok := proto.ParseResponse(b, proto.CmdQueryInfo)
		return ok
	})
	if err != nil {
		return proto.DeviceInfo{}, err
	}
	if rsp == nil {
		return proto.DeviceInfo{}, errors.New("device did not answer the info query within 5s")
	}
	p, _ := proto.ParseResponse(rsp, proto.CmdQueryInfo)
	info, err := proto.ParseDeviceInfo(p)
	if err != nil {
		return info, err
	}
	s.Info = &info
	return info, nil
}

// SendFrame transmits a packed 1 bpp frame. progress (optional) is called
// after each packet with (sent, total).
func (s *Session) SendFrame(ctx context.Context, frame []byte, progress func(int, int)) error {
	if s.Info == nil {
		if _, err := s.QueryInfo(ctx); err != nil {
			return err
		}
	}
	size := s.Info.PacketSize
	if size <= 0 {
		return fmt.Errorf("device reported packet size %d", size)
	}
	total := (len(frame) + size - 1) / size
	s.Stats = FrameStats{Packets: total}
	start := time.Now()
	defer func() { s.Stats.Total = time.Since(start) }()
	s.link.Drain()
	for i := 0; i < total; i++ {
		end := min((i+1)*size, len(frame))
		pkt, err := proto.DataPacket(i, frame[i*size:end], size)
		if err != nil {
			return err
		}
		withResponse := s.AckMode != AckWrite || s.SyncEvery <= 0 ||
			(i+1)%s.SyncEvery == 0 || i == total-1
		ws := time.Now()
		if err := s.link.Write(ctx, pkt, withResponse); err != nil {
			return fmt.Errorf("packet %d/%d: %w", i+1, total, err)
		}
		wd := time.Since(ws)
		s.Stats.Writing += wd
		s.Stats.SlowWrite = max(s.Stats.SlowWrite, wd)
		if s.AckMode != AckWrite {
			// The first packet decides: firmware that acks via notification
			// answers well within 500ms; afterwards wait up to 5s.
			wait := 500 * time.Millisecond
			if s.AckMode == AckNotify {
				wait = 5 * time.Second
			}
			as := time.Now()
			rsp, err := s.waitFor(ctx, wait, func(b []byte) bool { return proto.DataAck(b) != proto.AckMissing })
			s.Stats.Waiting += time.Since(as)
			if err != nil {
				return err
			}
			switch proto.DataAck(rsp) {
			case proto.AckSuccess:
				s.AckMode = AckNotify
			case proto.AckRejected:
				return fmt.Errorf("device rejected packet %d/%d", i+1, total)
			case proto.AckInvalid:
				return fmt.Errorf("invalid ack for packet %d/%d: % x", i+1, total, rsp)
			default:
				s.AckMode = AckWrite
			}
		}
		if progress != nil {
			progress(i+1, total)
		}
	}
	return nil
}
