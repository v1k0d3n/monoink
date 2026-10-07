# Contributors

## People

- **Brandon B. Jozsa** ([@v1k0d3n](https://github.com/v1k0d3n)): creator and
  maintainer.

Contributed to monoink? Add yourself to this list in your pull request, in
the format above.

## Acknowledgements

- **[Claude Code](https://claude.com/claude-code)** (Opus 5.5) wrote most of
  the code and documentation, working with the maintainer. Commits it
  co-authored carry a `Co-Authored-By: Claude` line.
- **[Decky Loader](https://decky.xyz)**, the plugin system monoink runs in.
- **[Open-Meteo](https://open-meteo.com)**, for free weather and geocoding
  without an account or API key.
- **The Go fonts** ([golang.org/x/image/font/gofont](https://go.dev/blog/go-fonts)),
  used for everything drawn on the display.
- **[pixel-faceplate](https://github.com/hodapp/pixel-faceplate)**, an
  independent project for JSAUX's Dot Matrix faceplate, whose user guide
  helped shape monoink's install guide.

monoink is not affiliated with JSAUX. Its Bluetooth protocol support was
learned from JSAUX's distributed software; no JSAUX code is included (see
[NOTICE](../NOTICE)).

## How to contribute

Everyone is welcome, whether or not you write code.

- **Found a problem?** [Open an issue](https://github.com/v1k0d3n/monoink/issues/new/choose)
  with the **Something isn't working** form. A photo of the display and the
  plugin's status line help a lot.
- **Have an idea?** Use the **Idea or feature request** form. If it's about
  showing information from another app or service, check first whether a
  [custom card](providers.md) can do it without changing the plugin.
- **Tried it on a different device?** Reports from a Steam Deck or other
  SteamOS hardware are especially useful; open an issue even if everything
  worked.
- **Want to change the code?** See [development.md](development.md) for how
  to build and test (you only need podman or docker), then open a pull
  request. CI builds an installable zip for every pull request, so changes
  can be tried on a real device before they're merged.

Contributions must keep the project's [principles](development.md#principles):
no sudo, no data access without the user opting in, and integrations as
providers rather than built into the core.
