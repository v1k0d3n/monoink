package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/v1k0d3n/monoink/backend/internal/engine"
	"github.com/v1k0d3n/monoink/backend/internal/paths"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// cmdPush submits a card to a running monoinkd. With -json, the card is
// read from stdin instead of flags.
func cmdPush(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	var in engine.CardInput
	var lines multiFlag
	progress := fs.Float64("progress", -1, "progress bar value 0..1 (omit for none)")
	fromJSON := fs.Bool("json", false, "read the card as JSON from stdin")
	socket := fs.String("socket", paths.ProviderSocket(), "provider socket path")
	fs.StringVar(&in.ID, "id", "", "provider id (a-z, 0-9, . _ -)")
	fs.StringVar(&in.Name, "name", "", "display name of the provider")
	fs.StringVar(&in.Title, "title", "", "card title")
	fs.Var(&lines, "line", "card line (repeatable, max 8)")
	fs.IntVar(&in.TTL, "ttl", 3600, "seconds until the card expires (max 86400)")
	fs.Parse(args)

	if *fromJSON {
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10)).Decode(&in); err != nil {
			return fmt.Errorf("read card JSON: %w", err)
		}
	} else {
		in.Lines = lines
		if *progress >= 0 {
			in.Progress = progress
		}
	}
	if in.ID == "" {
		return errors.New("-id is required")
	}

	body, _ := json.Marshal(in)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", *socket)
		}},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://monoink/v1/card", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("monoinkd is not running (or %s is not accessible): %w", *socket, err)
	}
	defer resp.Body.Close()
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println("card accepted")
	case http.StatusAccepted:
		fmt.Println("card held: approve this provider in the plugin settings to show it")
	default:
		return fmt.Errorf("rejected: %s", out["error"])
	}
	return nil
}
