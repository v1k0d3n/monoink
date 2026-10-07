# Development

## Layout

| Path | What |
| --- | --- |
| `backend/` | `monoinkd`, a static Go program: Bluetooth (BlueZ over D-Bus), rendering, scheduling, the local API. |
| `src/` | The Decky panel (TypeScript/React). |
| `main.py` | Decky's required Python entry point. Standard library only; it just supervises `monoinkd` and forwards panel requests. |
| `Containerfile` | The build environment: Go, Node, pnpm and zip at pinned versions. |
| `package.sh` | Tests and builds everything into `out/monoink.zip`, inside that environment. |
| `.github/workflows/` | CI, releases and AI review (below). |

## Principles

These are requirements for every change, and the review workflow checks
for them:

- No sudo, and no writes outside the plugin's own settings/data folders.
- Every file the core reads and every network request it makes is listed in
  the README, and anything that reveals something about the user is opt-in.
- The core never reads other apps' data or credentials; integrations are
  [providers](providers.md).
- `main.py` uses only the Python standard library; `monoinkd` stays
  `CGO_ENABLED=0`.

## Building and testing locally

Only podman (preinstalled on SteamOS) or docker is needed. `package.sh`
builds the image from `Containerfile` on first use and runs everything
inside it, so every contributor, CI and the release workflow use the same
toolchain:

```bash
./package.sh            # all tests and checks, then out/monoink.zip
./package.sh test       # tests and checks only
./package.sh shell      # a shell inside the build environment
./package.sh run pnpm install   # any command inside it
```

Build outputs are owned by your user, and tool caches are kept in `.cache/`
(gitignored) so repeat builds are fast. With Go 1.26+ and Node 22 already
installed, `./package.sh --native` skips the container.

To change a toolchain version, edit `Containerfile` (and `packageManager` in
`package.json` for pnpm, `go` in `backend/go.mod` for Go).

Useful hardware checks (stop the plugin's connection first by turning
**Enabled** off in the panel, since only one program can connect):

```bash
backend/out/monoinkd probe                              # find and query the display
backend/out/monoinkd probe -test-pattern                # draw a checkerboard
backend/out/monoinkd probe -test-pattern -numbers -repeat 3 -idle 3s   # must end on "3"
```

## Continuous integration

| Workflow | When | What |
| --- | --- | --- |
| **CI** (`ci.yml`) | every push to `main` and every pull request | gofmt, `go vet`, Go tests with the race detector, TypeScript type-check and build, `main.py` compile check, then a full package built with `Containerfile`. The installable zip is attached to the run as an artifact (*monoink-plugin-…*) for 14 days, so changes can be tried on a device before release. |
| **Release** (`release.yml`) | pushing a `v*` tag | Builds and tests with `Containerfile`, then publishes a GitHub release with `monoink-vX.Y.Z.zip`, its SHA-256, install instructions and generated notes. `v0.*` and `-suffix` tags are marked pre-release. |
| **Claude Code Review** (`claude-review.yml`) | pull requests from branches in this repository | AI review against the principles above, with inline comments. |
| **Claude** (`claude.yml`) | a comment containing `@claude` by someone with write access | Answers questions, reviews (`@claude review`), or implements a request (`@claude implement this`) on a new branch with a pull request for a human to review and merge. |

### One-time setup for the Claude workflows

1. Create an API key at [console.anthropic.com](https://console.anthropic.com).
2. In the repository: **Settings → Secrets and variables → Actions → New
   repository secret**, name `ANTHROPIC_API_KEY`.
3. Install the Claude GitHub app on the repository
   ([github.com/apps/claude](https://github.com/apps/claude)), or run
   `/install-github-app` from Claude Code.

Without the secret, CI and releases work normally; only the Claude
workflows fail.

### Pull requests from forks

GitHub doesn't give secrets to workflows triggered by pull requests from
forks, so automatic AI review only runs for branches inside this repository.
For a community pull request, a maintainer comments `@claude review`; that
runs in this repository's trusted context and reads the pull request
through GitHub's API rather than executing its code. CI (tests and build)
runs on every pull request, including forks, without access to secrets.

## Releasing

1. Update `"version"` in `package.json` (e.g. `0.2.0`) and commit.
2. Tag and push:

   ```bash
   git tag v0.2.0
   git push origin main v0.2.0
   ```

3. The Release workflow publishes the zip. Check the Releases page.
