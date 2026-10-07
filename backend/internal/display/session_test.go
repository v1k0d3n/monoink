package display

import (
	"bytes"
	"context"
	"testing"

	"github.com/v1k0d3n/monoink/backend/internal/proto"
)

// fakeLink emulates the firmware: answers info queries and optionally acks
// each data packet via notification.
type fakeLink struct {
	ackNotify bool
	queries   int
	packets   [][]byte
	ch        chan []byte
}

func newFake(ack bool) *fakeLink { return &fakeLink{ackNotify: ack, ch: make(chan []byte, 8)} }

func (f *fakeLink) Notifications() <-chan []byte { return f.ch }
func (f *fakeLink) Drain() {
	for len(f.ch) > 0 {
		<-f.ch
	}
}
func (f *fakeLink) Write(_ context.Context, b []byte, _ bool) error {
	switch b[1] {
	case proto.CmdQueryInfo:
		f.queries++
		f.ch <- []byte{0x91, 0x00, 0x01, 0x00, 0x02, 0x88, 0x01, 0xE0, 0x00, 0x6C, 0x40, 0x19}
	case proto.CmdSendData:
		f.packets = append(f.packets, append([]byte(nil), b...))
		if f.ackNotify {
			f.ch <- []byte{0x91, 0x02, 0x00, 0x19}
		}
	}
	return nil
}

func TestSendFrame(t *testing.T) {
	for _, ack := range []bool{true, false} {
		f := newFake(ack)
		s := New(f)
		frame := bytes.Repeat([]byte{0x55}, proto.FrameSize)
		if err := s.SendFrame(context.Background(), frame, nil); err != nil {
			t.Fatal(err)
		}
		if want := (proto.FrameSize + 107) / 108; len(f.packets) != want {
			t.Fatalf("ack=%v: sent %d packets, want %d", ack, len(f.packets), want)
		}
		last := f.packets[len(f.packets)-1]
		if last[2] != 0x01 || last[3] != 0x67 || len(last) != 113 {
			t.Fatalf("last packet header/len wrong: % x len=%d", last[:4], len(last))
		}
		wantMode := AckWrite
		if ack {
			wantMode = AckNotify
		}
		if s.AckMode != wantMode {
			t.Errorf("ack=%v: mode %v, want %v", ack, s.AckMode, wantMode)
		}
	}
}

func TestEveryFrameIsPrecededByQuery(t *testing.T) {
	f := newFake(false)
	s := New(f)
	frame := make([]byte, proto.FrameSize)
	for i := 0; i < 3; i++ {
		if err := s.SendFrame(context.Background(), frame, nil); err != nil {
			t.Fatal(err)
		}
	}
	if f.queries != 3 {
		t.Fatalf("firmware only draws frames that follow a query: got %d queries for 3 frames", f.queries)
	}
}
