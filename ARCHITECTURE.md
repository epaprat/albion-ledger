# Architecture

Albion Ledger is a passive network sniffer for Albion Online. It reads the local
Photon UDP stream, decodes the game's own messages, and turns them into a desktop
UI over the player's market and account data. Nothing is written to the game and
no memory is read — the only input is packets already on the wire.

## Data flow

```
                 ┌───────────────────────── capture goroutine ─────────────────────────┐
  UDP packets ──▶ PacketSource ──▶ Photon parse ──▶ classify ──▶ handler registry ──▶ domain
   (libpcap)      (adapter)        (Protocol16/18)  (probe/codes) (per-message handler)  models
                                                                          │
                                                                          ▼
                                                              Service (view state)
                                                                   │          │
                                                     Wails bindings│          │typed events
                                                     (request/resp)│          │(streaming)
                                                                   ▼          ▼
                                                                 Vue UI    SQLite (local-first)
```

- **Capture** is the only OS-specific part. A `PacketSource` port is implemented by a
  build-tagged libpcap adapter (`-tags pcap`) for live capture, and by a replay adapter
  for recorded `.pcap` fixtures. The default build is pure-Go and needs no libpcap, so
  tests and offline replay run anywhere.
- **Photon parse** decodes the Protocol16/18 wire format into a message (operation or
  event) with a parameter map. Every read is bounds-checked; a malformed or encrypted
  packet is dropped, not fatal.
- **Classify** maps a message code to a category using a data-driven `codes.json`, so a
  patch that renumbers a code is a data edit, not a source change.
- **Handler registry** dispatches each category to its own handler, which parses exactly
  one message into one domain model. Adding a packet type is a new handler file plus one
  registration — never an edit to the dispatcher.
- **Domain models** (market, holdings, flow, spec, valuation, zones) are plain data with
  no infrastructure dependencies.
- **Service → UI** crosses the Go↔frontend boundary through Wails **bindings** (request/
  response) and typed **events** (streaming updates). Only explicit DTOs cross; domain
  and pcap types never leak to the UI.
- **Persistence** is local-first: SQLite is the durable source of truth. Any external
  upload is opt-in and batched.

## Packages

| Package | Responsibility |
| ------- | -------------- |
| `internal/photon` | Photon framing + Protocol16/18 deserialization (bounds-checked, fuzzed) |
| `internal/adapter/capture` | `PacketSource` adapters (libpcap live / replay) + wire field extractors |
| `internal/domain/model` | Core model/DTO types + message categories |
| `internal/domain/probe` | Message classifier (code → category) |
| `internal/app` | Capture-time pipeline: classify → per-category handler registry → service sink |
| `internal/codes`, `internal/catalog`, `internal/locations` | Data-driven maps (message codes, item catalog, zone names) |
| `internal/valuation` | Layered item pricing (live market > server estimate > external) |
| `internal/holdings` | Inventory + bank aggregation, grouping, freshness |
| `internal/flow`, `internal/zonestats`, `internal/loot` | Activity flow, per-zone rates, loot correlation |
| `internal/specboard`, `internal/specenum` | Destiny Board skill tree + login-board (E:1) decode |
| `internal/pending` | The bounded pending-map structure used across handlers |
| `internal/adapter/wails` | The Service: view state, bindings, typed events, CSV export |
| `internal/adapter/store` | SQLite persistence |
| `cmd/albion-ledger` | Wails desktop entrypoint + Vue frontend (`frontend/`) |
| `cmd/probe` | A CLI that reports which data categories can be sniffed and how completely |

## Guiding principles

- **Ports & adapters.** The domain core depends on interfaces it owns; capture, UI, and
  persistence are adapters. The parser is testable offline without any of them.
- **Open/closed handler pipeline.** New packet types extend by adding files, not by
  editing shared dispatch code.
- **Test-first on recorded packets.** Parser and handler tests run against captured byte
  fixtures — deterministic and offline. A parser fix starts with a fixture that
  reproduces it.
- **Untrusted input.** Every packet may be truncated, malformed, encrypted, or hostile;
  the parser fails-and-drops rather than crashing.
- **Bounded memory.** Long-running structures are capped so a multi-hour session keeps a
  flat memory footprint.
- **ToS-safe & passive.** Read-only network capture; market data and the player's own
  account data only; no radar/ESP or real-time advantage.

For how to build, test, and contribute, see [CONTRIBUTING.md](CONTRIBUTING.md).
