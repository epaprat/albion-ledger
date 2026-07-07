# Contributing to Albion Ledger

Thanks for your interest. Albion Ledger is a passive, ToS-safe Albion Online data
tool. This guide covers how to build it, how it is developed, and how to send a
change.

## Ground rules

- **Passive only.** The project reads the local network stream — nothing that reads
  game memory, injects, automates input, or grants a real-time competitive advantage
  (no radar/ESP/entity positions). Contributions that cross that line will not be
  accepted. See [SECURITY.md](SECURITY.md) for the full stance.
- **Your own data stays local.** Any external upload must be explicit and opt-in.
- Be respectful — see the [Code of Conduct](CODE_OF_CONDUCT.md).

## Prerequisites

- **Go** 1.26+
- **Node.js** 18+ (frontend build)
- **Wails** v2 CLI — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Packet capture privileges for live capture: BPF on macOS, Npcap on Windows,
  `cap_net_raw` on Linux.

## Build & run

```sh
git clone https://github.com/epaprat/albion-ledger.git
cd albion-ledger
scripts/setup-hooks.sh        # install the local git gates (recommended)

# Build the desktop app (live capture needs the pcap build tag)
cd cmd/albion-ledger
wails build -tags pcap
# macOS: run the app with capture privileges
sudo ./build/bin/albion-ledger.app/Contents/MacOS/albion-ledger -iface en0
```

The default (no `pcap` tag) build is pure-Go, so tests and offline replay run
anywhere without libpcap.

## Tests

```sh
go test ./...                                   # Go unit + golden tests
( cd cmd/albion-ledger/frontend && npm test )   # frontend (Vitest)
scripts/ci-local.sh                             # the full local CI (what pre-push runs)
```

Parser and handler tests run against **recorded packet fixtures**, never a live
client — CI stays deterministic and offline. A new handler or a parser fix ships
with a fixture that exercises real bytes.

## Branching (git-flow)

- `main` holds only released, tagged commits. `dev` is the integration branch.
- Cut a feature branch from `dev` (e.g. `feature/short-name`), and open your PR
  against `dev`.
- Feature branches merge into `dev` with `--no-ff` so each change stays a
  discoverable, revertable unit.
- `main` only advances by a release: a `dev → main` merge tagged `vX.Y.Z`.

## Commits & pull requests

- Write clear, imperative commit subjects that say what changed and why.
- **No AI co-author trailers** and no bot attributions in commit messages.
- Keep a PR focused on one change. Fill in the PR template checklist.
- A PR is ready when `scripts/ci-local.sh` is green and docs are updated with the
  code they describe.

## Quality gates

Enforcement runs **locally** (no CI minutes are spent on push/PR):

- `pre-commit` — repo hygiene + docs freshness
- `commit-msg` — commit message checks
- `pre-push` — the full local CI (`scripts/ci-local.sh`)

Install them once with `scripts/setup-hooks.sh`.

## How this project is governed

- **Decisions.** The project is maintained by a single maintainer; changes land
  through PR review against `dev`. Design intent is captured before implementation.
- **Quality bar.** Every change respects a small set of standing principles:
  hexagonal core (domain has no infrastructure dependencies), a registry-based
  handler pipeline (new packet types are new files, not edits to a dispatcher),
  test-first on recorded packets, bounded memory for long runs, and the passive/
  ToS-safe stance above.
- **Versioning.** Releases follow [Semantic Versioning](https://semver.org/):
  `MAJOR` for a breaking user-facing change, `MINOR` for a compatible new feature,
  `PATCH` for fixes. Pre-1.0 (`0.y.z`), minor versions may still change the surface.
  Every release is a `dev → main` merge, tagged `vX.Y.Z`, with a matching
  [CHANGELOG](CHANGELOG.md) entry and per-OS binaries attached to the GitHub Release.

## Architecture

For a map of how the code fits together — capture → parse → handler registry →
domain → UI/persistence — see [ARCHITECTURE.md](ARCHITECTURE.md).
