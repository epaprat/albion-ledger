#!/usr/bin/env bash
# capture-join.sh — record raw Albion Photon traffic (UDP 5056) to a pcap so the
# current-city / Join op code (feature 004, task T026) can be found offline.
#
# Usage:  sudo scripts/capture-join.sh [interface]
# Then:   relog AND visit/open the bank in 2-3 DIFFERENT cities, then press Ctrl+C.
# Output: captures/join-<timestamp>.pcap  (gitignored) — hand this file back for analysis.
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p captures

# Pick the interface: arg 1, else the system default route's interface.
IFACE="${1:-}"
if [ -z "$IFACE" ]; then
  IFACE="$(route -n get default 2>/dev/null | awk '/interface:/{print $2}')"
fi
if [ -z "$IFACE" ]; then
  echo "Could not auto-detect interface. List them with:  ifconfig -l" >&2
  echo "Then re-run:  sudo scripts/capture-join.sh <iface>   (e.g. en0)" >&2
  exit 1
fi

TS="$(date +%Y%m%d-%H%M%S)"
OUT="captures/join-${TS}.pcap"
LOG="captures/join-${TS}.txt"

echo "Interface : $IFACE"
echo "Writing   : $OUT"
echo "Log       : $LOG"
echo
echo ">>> NOW in game: relog, then visit + open the bank in 2-3 different cities."
echo ">>> Press Ctrl+C here when done."
echo

# -s 0 = full packets; filter to Albion's Photon UDP port so the pcap stays small
# and matches the app's own BPF (udp port 5056). tcpdump needs root.
tcpdump -i "$IFACE" -s 0 -w "$OUT" "udp port 5056" 2>"$LOG" || true

echo
echo "Saved $OUT"
echo "Send that file back (or run:  ./bin/probe-live replay $OUT --dump )"
