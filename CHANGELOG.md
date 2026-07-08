# Changelog

All notable changes to Albion Ledger are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project uses
[Semantic Versioning](https://semver.org/). Pre-1.0, minor versions may still
change the public surface.

## [0.1.2] — 2026-07-08

Money in, money out — this release makes the tool answer "what am I worth?" and
"did that trade pay off?" on top of the existing holdings and earnings views.

### Added

- **Net worth** — a single figure combining your liquid silver (wallet) and the
  valued total of everything you hold. Shown in the Holdings header alongside the
  wallet balance. When the wallet has not been seen yet, the total says so
  instead of pretending you have zero silver.
- **Trade ledger** — a new Trades screen that itemizes your marketplace activity
  as profit and loss. It captures all four trade types (instant sell, sell order,
  instant buy, buy order) plus the in-game mailbox, and breaks every transaction
  into gross, sales tax, setup fee, and net so the real number is never hidden.
  Income and Expense are labeled and filterable, sorted most-recent-first, each
  row showing the item icon, readable name, and the time it happened.
- Trade ledger joins the CSV export set (holdings, flow, zones, market, destiny
  board, and now trades).

### Fixed

- The default/open bank tab is now captured when you press K, without opening the
  bank — previously that first tab's contents were silently dropped, which made
  net worth read low against the in-game estimate.
- Photon packet reassembly now dedupes retransmitted fragments, so large
  responses (such as a full mailbox sync) no longer arrive incomplete.

### Changed

- The Holdings header now explains how its total is derived (our prices vs. the
  game's own estimate vs. unpriced items), so the difference from the in-game
  figure is understandable rather than mysterious.

[0.1.2]: https://github.com/epaprat/albion-ledger/releases/tag/v0.1.2

## [0.1.1] — 2026-07-07

Repository and release-process maturation — no application feature change.

### Added

- Open-source documentation for external contributors: a rewritten README, a
  CONTRIBUTING guide (build, test, git-flow, and a governance summary), a
  Contributor Covenant code of conduct, a security policy, an architecture
  overview, and GitHub issue/PR templates.
- A tag-triggered release workflow that builds per-OS binaries and attaches them
  to the GitHub Release: macOS (live-capture), Linux (experimental), and Windows
  (build-only preview).

[0.1.1]: https://github.com/epaprat/albion-ledger/releases/tag/v0.1.1

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
