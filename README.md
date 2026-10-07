# monoink

An open, privacy-respecting driver for the JSAUX E-Ink faceplate (5.83",
648×480, monochrome, Bluetooth LE) on SteamOS.

> Status: early development. The backend works on real hardware; the Decky
> plugin is next.

## Goals

- **One self-contained binary.** `monoinkd` is a static Go program with no
  Python, system libraries or package installs, so SteamOS updates can't
  break it.
- **No sudo, no system changes.** It talks to BlueZ over D-Bus as the normal
  user. The display needs no pairing. Nothing is written outside the
  plugin's own settings and cache directories.
- **Configured from Gaming Mode** via a Decky plugin, with an optional local
  web page for Desktop Mode.
- **Private by default.** The core reads only what its screens need (see
  below) and makes no network requests unless you enable a feature that
  needs one.
- **Optional integrations are separate programs.** Anything that wants to put
  a status card on the display pushes it to a local socket, and is held until
  you approve that provider.

## Screens

Clock, calendar, weather, performance (CPU/GPU/RAM/temperatures), current or
last-played Steam game with cover art, photo frame, provider cards, and a
combined dashboard.

## What it reads and contacts

| Source | When |
| --- | --- |
| BlueZ (system D-Bus) | always: finding and talking to the display |
| `/proc`, `/sys` | performance screen: CPU, memory, temperatures, GPU load |
| Steam's local files (`steamapps/*.acf`, `userdata/*/config/localconfig.vdf`, `appcache/librarycache`) | game screen: name, playtime, cached cover art |
| Your chosen photo folder | photo frame |
| `api.open-meteo.com`, `geocoding-api.open-meteo.com` | only after you set a weather location |
| `cdn.akamai.steamstatic.com` | only if you enable "download missing cover art" |

## Building

Only `podman` (preinstalled on SteamOS) or `docker` is needed; the Go
toolchain runs in a container.

```bash
cd backend
./build.sh test
./build.sh build      # → backend/out/monoinkd
```

## Command line

```bash
monoinkd probe -test-pattern     # find the display, show its info, draw a test pattern
monoinkd send picture.jpg        # dither and display an image
monoinkd serve                   # run the service (normally started by the Decky plugin)
monoinkd push -id my-tool -title "Build" -line "main: passing"   # provider card
```

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
