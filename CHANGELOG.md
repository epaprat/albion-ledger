# Changelog

All notable changes to Albion Ledger are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project uses
[Semantic Versioning](https://semver.org/). Pre-1.0, minor versions may still
change the public surface.

## [0.1.0] — 2026-07-07

First tagged release. A passive, ToS-safe desktop tool that reads the local
Albion Online network stream — no memory reading, no injection, no automation —
and surfaces your own assets, earnings, and market data.

### Added

- **Live earnings** — an activity flow of silver, loot, gather, and fame with a
  session summary, and a headline silver/hour figure that answers "how much am I
  earning right now?" at a glance.
- **Zone analytics** — per-zone earning rates over selectable time windows, so
  you can see where your time actually pays.
- **Holdings** — your inventory and every city bank, valued, grouped by city and
  tab, with freshness/stale indicators; bank overview syncs whole cities from the
  in-game K screen without opening each tab.
- **Valuation** — a layered price model: live marketplace prices where seen, a
  persisted server estimate otherwise, and an optional external market fallback.
- **Destiny Board** — your full skill tree decoded from the achievement stream,
  including maxed skills shown correctly at login, organized by gear slot.
- **CSV export** — every dataset (holdings, flow, zones, market, destiny board)
  exports to Excel-compatible CSV, one dataset or all at once.
- **Designed throughout** — sortable and filterable tables, a considered state for
  every screen (empty, loading, stale, error, encrypted), keyboard access, visible
  focus, and system light/dark support.

### Notes

- macOS is the currently supported platform; capture needs elevated privileges
  (run under `sudo`) to read the network interface.
- Own-account data stays local. No external upload happens without opt-in.

[0.1.0]: https://github.com/epaprat/albion-ledger/releases/tag/v0.1.0
