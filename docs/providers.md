# Custom cards ("providers")

A **card** is a small status panel that another program puts on your
e-ink display: a title, a few lines of text, and an optional progress bar.
The program that sends it is called a **provider**.

```
┌──────────────────────────────────────────────┐
│ NIGHTLY BUILD                   Wed, Oct 7 ▮ │
├──────────────────────────────────────────────┤
│ Build pipeline                               │
│ main: passing                                │
│ release: 3 jobs queued                       │
│                                              │
│ [███████████████████░░░░░░░░░░░░░░░░░░]      │
│                                Updated 14:05 │
└──────────────────────────────────────────────┘
```

Providers are how monoink supports integrations (build status, smart-home
sensors, chat notifications, AI usage meters, …) **without** the plugin
itself ever reading other apps' files, passwords or accounts. A provider is
a separate script or program that you choose to run; it decides what to
send, and monoink only displays what it receives.

## How it works

1. A provider sends a card to monoink's local socket.
2. **The first time** a provider sends anything, monoink holds the card and
   lists the provider under **Providers** in the plugin as *Waiting for
   approval*. Nothing is displayed yet.
3. You tap **Approve**. From then on its cards appear on the **Provider
   card** screen. Turn that screen on under **Screens** (or pin it).
4. Optional: turn on **Show new cards immediately** to interrupt the
   rotation for two minutes whenever a fresh card arrives.
5. Cards expire on their own (after one hour unless the provider asks for
   something else, at most 24 hours). **Revoke** removes a provider and its
   card at any time.

When several approved providers have live cards, the most recently updated
one is shown.

## Sending a card

All examples run as your normal user (the `deck` user on a Steam Machine or
Steam Deck), from Desktop Mode's Konsole or from a script. The plugin must
be installed and enabled.

### Easiest: the `monoinkd push` command

The plugin includes a small command for this:

```bash
~/homebrew/plugins/monoink/bin/monoinkd push \
  -id my-first-card \
  -name "Hello" \
  -title "It works!" \
  -line "This card came from a script" \
  -line "Approve me in the plugin"
```

The first run prints `card held: approve this provider in the plugin
settings to show it`. Approve **Hello** in the plugin, run the command
again, and it prints `card accepted`.

All options:

| Option | Meaning |
| --- | --- |
| `-id` | **Required.** A stable identifier for your provider: lowercase letters, digits, `.`, `_`, `-`; at most 48 characters. Approval is remembered per id. |
| `-name` | The name shown in the plugin and in the card's header (max 32 characters). Defaults to the id. |
| `-title` | The large heading (max 80 characters). |
| `-line` | One line of text (max 100 characters). Repeat for up to 8 lines. |
| `-progress` | A progress bar from `0` (empty) to `1` (full), e.g. `0.42`. Omit for no bar. |
| `-ttl` | Seconds until the card disappears. Default `3600` (1 hour), maximum `86400` (24 hours). |
| `-json` | Read the whole card as JSON from standard input instead (format below). |

Text longer than the limits is cut off, not rejected. Long lines are
shortened with "…" to fit the display.

### With `curl`

Any language that can talk HTTP over a Unix socket works. `curl` is
preinstalled on SteamOS:

```bash
curl --unix-socket "/run/user/$(id -u)/monoink/providers.sock" \
  -H 'Content-Type: application/json' \
  -d '{"id":"my-first-card","name":"Hello","title":"It works!","lines":["Sent with curl"]}' \
  http://monoink/v1/card
```

### From Python (standard library only)

```python
import http.client, json, os, socket

class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("monoink", timeout=10)
        self.path = path
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.path)

def push_card(card):
    conn = UnixConnection(f"/run/user/{os.getuid()}/monoink/providers.sock")
    conn.request("POST", "/v1/card", json.dumps(card), {"Content-Type": "application/json"})
    resp = conn.getresponse()
    return resp.status, json.loads(resp.read())

print(push_card({
    "id": "weather-station",
    "name": "Backyard",
    "title": "Backyard sensors",
    "lines": ["Temperature 21.4 °C", "Humidity 48%", "Rain gauge 0 mm"],
    "ttl_seconds": 900,
}))
```

## Card format

`POST /v1/card` with a JSON body:

```json
{
  "id": "ci-status",
  "name": "CI",
  "title": "Nightly build",
  "lines": ["main: passing", "release: 3 jobs queued"],
  "progress": 0.6,
  "ttl_seconds": 3600
}
```

| Field | Type | Required | Limits |
| --- | --- | --- | --- |
| `id` | string | yes | `^[a-z0-9][a-z0-9._-]{0,47}$` |
| `name` | string | no | 32 characters |
| `title` | string | no | 80 characters |
| `lines` | array of strings | no | 8 lines × 100 characters |
| `progress` | number | no | clamped to 0–1 |
| `ttl_seconds` | integer | no | default 3600, max 86400 |

Responses:

| Status | Body | Meaning |
| --- | --- | --- |
| `200` | `{"status":"accepted"}` | Shown (or will be, when the card screen comes up). |
| `202` | `{"status":"pending_approval"}` | Held until you approve the provider in the plugin. |
| `400` | `{"error":"…"}` | Invalid request, e.g. a malformed id. |

`GET /v1/health` returns `{"status":"ok","version":"…"}`, which is handy to
check whether monoink is running.

## Keeping a card up to date

A card is a snapshot. To keep it current, send it again whenever the
information changes, and pick a `ttl_seconds` a little longer than your
update interval so a stalled script doesn't leave stale information
on screen.

On SteamOS, a systemd **user** timer is a tidy way to run a script
periodically without touching the system. Save your script as
`~/.local/bin/my-card.sh` (and `chmod +x` it), then create two files.

`~/.config/systemd/user/my-card.service`:

```ini
[Unit]
Description=Push my card to the e-ink display

[Service]
Type=oneshot
ExecStart=%h/.local/bin/my-card.sh
```

`~/.config/systemd/user/my-card.timer`:

```ini
[Unit]
Description=Update my e-ink card every 5 minutes

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
```

Enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable --now my-card.timer
```

Remove it later with `systemctl --user disable --now my-card.timer` and
delete the two files. Everything lives in your home folder and survives
SteamOS updates.

## Security and privacy

- The socket is only accessible to your own user account (permissions
  `0600` inside `/run/user/<uid>/monoink/`). Other users on the machine and
  other devices on your network cannot send cards.
- Approval is per `id`. A provider that changes its id must be approved
  again.
- Providers can only **send** cards. They cannot read your settings, change
  what's on the display otherwise, or talk to the Bluetooth device.
- monoink never contacts a provider or reads anything it owns. What a
  provider reads to build its card is up to that provider, so only run
  scripts you trust, the same as any other program.
