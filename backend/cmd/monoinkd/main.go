// Command monoinkd drives the JSAUX E-Ink faceplate.
//
//	monoinkd serve                       run the display service
//	monoinkd push -id ID -title T ...    submit a provider card
//	monoinkd probe [-test-pattern]       hardware diagnostics
//	monoinkd send IMAGE                  send one image
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/ble"
	"github.com/v1k0d3n/monoink/backend/internal/display"
	"github.com/v1k0d3n/monoink/backend/internal/proto"
	"github.com/v1k0d3n/monoink/backend/internal/render"
	"github.com/v1k0d3n/monoink/backend/internal/screens"
)

var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `monoinkd %s — JSAUX E-Ink faceplate driver

Usage:
  monoinkd serve [flags]         run the display service (used by the Decky plugin)
  monoinkd push [flags]          submit a status card as a provider
  monoinkd probe [flags]         find the display, query it, optionally draw a test pattern
  monoinkd send [flags] IMAGE    dither and send a PNG/JPEG/GIF/WebP
  monoinkd version

Run "monoinkd COMMAND -h" for flags.
`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(ctx, os.Args[2:])
	case "push":
		err = cmdPush(ctx, os.Args[2:])
	case "probe":
		err = cmdProbe(ctx, os.Args[2:])
	case "send":
		err = cmdSend(ctx, os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type connectFlags struct {
	timeout time.Duration
	address string
}

func (c *connectFlags) register(fs *flag.FlagSet) {
	fs.DurationVar(&c.timeout, "timeout", 15*time.Second, "scan timeout")
	fs.StringVar(&c.address, "address", "", "only use the display with this MAC address")
}

func step(format string, a ...any) func(error) {
	start := time.Now()
	fmt.Printf("• "+format+" … ", a...)
	return func(err error) {
		if err != nil {
			fmt.Printf("FAILED (%s)\n", time.Since(start).Round(time.Millisecond))
			return
		}
		fmt.Printf("ok (%s)\n", time.Since(start).Round(time.Millisecond))
	}
}

// find locates the display: BlueZ cache/connected devices first, then LE scan.
func find(ctx context.Context, bus *ble.Bus, cf connectFlags) (ble.Candidate, error) {
	pick := func(cs []ble.Candidate) (ble.Candidate, bool) {
		var best ble.Candidate
		ok := false
		for _, c := range cs {
			if cf.address != "" && !strings.EqualFold(c.Address, cf.address) {
				continue
			}
			if !ok || c.Connected || (c.HasRSSI && c.RSSI > best.RSSI && !best.Connected) {
				best, ok = c, true
			}
		}
		return best, ok
	}

	done := step("checking devices BlueZ already knows")
	known, err := bus.Known()
	done(err)
	if err != nil {
		return ble.Candidate{}, err
	}
	for _, c := range known {
		fmt.Println("    known:", c)
	}
	if c, ok := pick(known); ok && c.Connected {
		return c, nil
	}

	done = step("scanning for %q (up to %s)", proto.AdvertisedName, cf.timeout)
	sctx, cancel := context.WithTimeout(ctx, cf.timeout)
	defer cancel()
	found, err := bus.Scan(sctx, func(c ble.Candidate) bool {
		return cf.address == "" || strings.EqualFold(c.Address, cf.address)
	})
	done(err)
	if err != nil {
		return ble.Candidate{}, err
	}
	for _, c := range found {
		fmt.Println("    seen:", c)
	}
	if c, ok := pick(found); ok {
		return c, nil
	}
	return ble.Candidate{}, errors.New("no display found: make sure the faceplate is powered and not connected to another device")
}

func open(ctx context.Context, cf connectFlags) (*ble.Bus, *ble.Link, *display.Session, error) {
	done := step("opening BlueZ on the system D-Bus")
	bus, err := ble.Open()
	done(err)
	if err != nil {
		return nil, nil, nil, err
	}
	powered, err := bus.Powered()
	fmt.Printf("    adapter %s powered=%v\n", bus.Adapter, powered)
	if err == nil && !powered {
		bus.Close()
		return nil, nil, nil, errors.New("Bluetooth is turned off")
	}

	c, err := find(ctx, bus, cf)
	if err != nil {
		bus.Close()
		return nil, nil, nil, err
	}
	fmt.Println("    using:", c)

	done = step("connecting and resolving GATT services")
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	link, err := bus.Connect(cctx, c.Path)
	done(err)
	if err != nil {
		bus.Close()
		return nil, nil, nil, err
	}
	fmt.Printf("    write MTU: %d\n", link.MTU)

	sess := display.New(link)
	done = step("querying device info")
	info, err := sess.QueryInfo(ctx)
	done(err)
	if err != nil {
		link.Close()
		bus.Close()
		return nil, nil, nil, err
	}
	fmt.Println("    device:", info)
	if !info.IsTarget() {
		fmt.Println("    WARNING: not the 648x480 monochrome faceplate this tool targets")
	}
	return bus, link, sess, nil
}

func sendImage(ctx context.Context, sess *display.Session, frame []byte) error {
	done := step("sending %d-byte frame", len(frame))
	last := -1
	err := sess.SendFrame(ctx, frame, func(n, total int) {
		if pct := n * 100 / total; pct/10 != last {
			last = pct / 10
			fmt.Printf("%d%% ", pct)
		}
	})
	done(err)
	if err == nil {
		fmt.Println("    ack mode:", sess.AckMode)
		fmt.Println("    stats:", sess.Stats)
	}
	return err
}

func cmdProbe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	var cf connectFlags
	cf.register(fs)
	pattern := fs.Bool("test-pattern", false, "send a checkerboard test pattern")
	idle := fs.Duration("idle", 0, "wait this long after connecting before sending (simulates a kept-open link)")
	sync := fs.Int("sync-every", 0, "experimental: unacknowledged writes with a sync every N packets")
	repeat := fs.Int("repeat", 1, "send the test pattern this many times")
	numbers := fs.Bool("numbers", false, "with -repeat: draw a large frame number instead of a checkerboard")
	keep := fs.Bool("keep", false, "stay connected until Ctrl-C")
	fs.Parse(args)

	bus, link, sess, err := open(ctx, cf)
	if err != nil {
		return err
	}
	defer bus.Close()
	defer link.Close()

	if *pattern {
		for i := 0; i < *repeat; i++ {
			if *idle > 0 {
				fmt.Printf("• idling %s\n", *idle)
				select {
				case <-time.After(*idle):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			cell := 40 + 20*(i%2) // alternate so the panel really redraws
			img := render.TestPattern(cell)
			if *numbers {
				c := screens.NewCanvas()
				c.Text(fmt.Sprint(i+1), screens.W/2, 80, screens.Bold, 300, screens.Center, color.Gray{})
				img = c.Gray
			}
			if err := sendImage(ctx, sess, render.Pack(img)); err != nil {
				return err
			}
			sess.SyncEvery = *sync // takes effect once the ack mode is known
		}
	}
	if *keep {
		fmt.Println("• connected; press Ctrl-C to disconnect")
		<-ctx.Done()
	}
	fmt.Println("• disconnecting")
	return nil
}

func cmdSend(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	var cf connectFlags
	cf.register(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("send needs exactly one image path")
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	frame := render.Pack(render.Dither(img))

	bus, link, sess, err := open(ctx, cf)
	if err != nil {
		return err
	}
	defer bus.Close()
	defer link.Close()
	return sendImage(ctx, sess, frame)
}
