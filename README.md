# Albion Ledger

**Albion Ledger Client** — a cross-platform, **ToS-safe** desktop tool for Albion Online. It
passively reads the local Photon network stream to surface market data and your own account
data, and turns it into a clear earnings (profit/loss) ledger.

> **Status:** early work in progress. Architecture and scope are defined; implementation is starting.

## What it does

- **Market data** — sell/buy orders, price history, gold prices.
- **Your own assets** — bank, inventory, equipment, and character specs.
- **Activity** — loot, gathering/fishing, silver and fame gains.
- **Valuation (EMV)** — every item priced from live market data (server estimate as fallback).
- **Earnings / P&L** — net +/- silver per session / hour / 24h / day, with rate breakdowns
  (silver/hr, loot value/hr, gather value/hr).

### Not in scope

No radar, ESP, entity positions, or anything that grants a real-time competitive advantage.
This tool is strictly passive (reads network traffic only — no memory reading, no injection, no
automation) and limited to market data and your own account data.

## Tech

- **Go** for packet capture (libpcap/Npcap) and Photon (Protocol16/18) parsing.
- **Wails** desktop shell with a web UI.
- **SQLite** local-first store; data is uploaded later in batches.
- Single self-contained binary per OS — Windows, macOS, Linux.

## Development

```sh
git clone <repo-url>
cd albion-ledger
scripts/setup-hooks.sh   # activate local git gates (lint/hygiene/CI before push)
```

Packet capture requires capture privileges: Npcap on Windows, BPF permissions on macOS,
`cap_net_raw` on Linux. Installers will bundle this step.

### Sniff probe

A measurement CLI that reports which data categories can be sniffed and how completely:

```sh
go build -o bin/probe ./cmd/probe
./bin/probe genfixture testdata/fixtures/synthetic.pcap   # deterministic offline sample
./bin/probe replay testdata/fixtures/synthetic.pcap       # → coverage report (text or --json)
```

Live capture (reads the local game stream) needs the libpcap build tag:

```sh
go build -tags pcap -o bin/probe ./cmd/probe
sudo ./bin/probe live                                     # passive; market + own-account data only
```

Capture stores default to `captures/` (created automatically, gitignored), so
local session databases never clutter the repo root.

### Game-change agility

The data the game can change between patches lives in editable files under `data/`:

- `data/items.json` — item index → name/tier/enchant catalog (generated from ao-bin-dumps)
- `data/codes.json` — message code → category map

Both are bundled (embedded) defaults and can be reloaded at runtime without recompiling, so a patch
that shifts item names or message codes is fixed by editing a file, not the source.

The default build is pure-Go (no libpcap) so tests and replay run anywhere.

Validated against live traffic: all target categories (market sell/buy/history,
gold, inventory, equipment, bank, character spec, loot, gather, silver, fame,
item value) are captured, and the market stream is unencrypted.

### Quality gates

Enforcement runs **locally** (no CI minutes burned):

- `pre-commit` — repo hygiene + docs freshness
- `commit-msg` — commit message checks
- `pre-push` — full local CI (`scripts/ci-local.sh`)

## License

MIT — see [LICENSE](LICENSE).
