# Security Policy

## Supported versions

Albion Ledger is pre-1.0 and under active development. Security fixes target the
**latest release** and the `dev` branch. Older tagged releases are not
maintained.

| Version | Supported |
| ------- | --------- |
| latest release / `dev` | ✅ |
| older tags | ❌ |

## Reporting a vulnerability

Please **do not** open a public issue for a security vulnerability.

Report it privately through
[GitHub Security Advisories](https://github.com/epaprat/albion-ledger/security/advisories/new),
which lets us discuss and fix the issue before any public disclosure. If you
cannot use advisories, contact the maintainer directly.

Please include:

- what the issue is and where it lives (file/path or feature),
- how to reproduce it, and
- the impact you foresee.

You can expect an initial acknowledgement, and we will keep you updated as we
work on a fix. Coordinated disclosure is appreciated.

## Scope and safety stance

Albion Ledger is **strictly passive**: it only reads the local game network
stream. It performs **no** memory reading, process injection, input automation,
or anything that alters the game, and it deliberately excludes radar/ESP, entity
positions, and any feature that grants a real-time competitive advantage.

Relevant to security and privacy:

- **Your own data stays local by default.** Any external upload (for example a
  market-data contribution) is explicit, opt-in, and free of personal
  identifiers.
- Live capture requires elevated network-capture privileges (BPF / Npcap /
  `cap_net_raw`). Only grant these from a trusted build.
- Captured packets are untrusted input; the parser is written to fail-and-drop a
  bad packet rather than crash. Parser/robustness issues are in scope for a
  report.
