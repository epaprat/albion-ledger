// Command probe captures (or replays) the game's Photon stream and reports,
// with measured numbers, which target data categories can be sniffed and how
// completely. ToS-safe (passive, no positions/radar). See specs/001-sniff-probe.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/adapter/store"
	"github.com/epaprat/albion-ledger/internal/app"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/report"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "replay":
		err = cmdRun(os.Args[2:], model.SourceReplay)
	case "live":
		err = cmdRun(os.Args[2:], model.SourceLive)
	case "report":
		err = cmdReport(os.Args[2:])
	case "reconcile":
		err = cmdReconcile(os.Args[2:])
	case "genfixture":
		err = cmdGenFixture(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `albion-ledger probe — measure what we can sniff

usage:
  probe replay <file.pcap> [--db probe.db] [--json]
  probe live [--iface NAME] [--db probe.db] [--json]   (requires -tags pcap)
  probe report --session <id> [--db probe.db] [--json]
  probe reconcile --session <id> --category C --ingame V --captured V --result pass|fail [--notes N] [--db probe.db]
  probe genfixture <out.pcap>
`)
}

func nowMS() int64 { return time.Now().UnixMilli() }

func cmdRun(args []string, kind model.SourceKind) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dbPath := fs.String("db", "probe.db", "local store path")
	iface := fs.String("iface", "", "capture interface (live)")
	asJSON := fs.Bool("json", false, "emit JSON report")
	dump := fs.Bool("dump", false, "print param table for each first-seen code (discovery, to stderr)")

	// For replay the pcap file is the first positional, before any flags.
	replayFile := ""
	if kind == model.SourceReplay {
		if len(args) < 1 || strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("replay needs a pcap file: probe replay <file.pcap> [flags]")
		}
		replayFile, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	var src port.PacketSource
	var iface2 string
	var err error
	if kind == model.SourceReplay {
		iface2 = replayFile
		src = capture.NewReplay(iface2)
	} else {
		if src, err = capture.NewLive(*iface); err != nil {
			return err
		}
		iface2 = *iface
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	sess := model.CaptureSession{
		ID: uuid.NewString(), StartedAt: nowMS(), SourceKind: kind, Interface: iface2,
	}
	if s := src.Status(); s.GameServer != "" {
		sess.GameServer = s.GameServer
	}

	// Live capture runs until Ctrl+C; replay ends at EOF. Either way a graceful
	// cancel lets the runner finish, store, and print the report.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if kind == model.SourceLive {
		fmt.Fprintln(os.Stderr, "capturing… play the game, then press Ctrl+C to stop and see the report")
	}

	runner := app.NewRunner(probe.DefaultThresholds())
	if *dump {
		seen := map[string]bool{}
		runner.OnMessage = func(kind probe.Kind, code int, params map[byte]interface{}) {
			key := fmt.Sprintf("%s:%d", dumpKind(kind), code)
			if seen[key] {
				return
			}
			seen[key] = true
			fmt.Fprintf(os.Stderr, "DUMP %-8s | %s\n", key, formatParams(params))
		}
	}
	res, err := runner.Run(ctx, src, db, sess, nowMS)
	if err != nil {
		return err
	}
	sess.EndedAt = nowMS()
	if s := src.Status(); s.GameServer != "" {
		sess.GameServer = s.GameServer
	}

	_ = db.SaveUnhandled(context.Background(), sess.ID, res.Unhandled)
	rep := report.Build(sess, res.Totals, res.Coverage, nil)
	fmt.Fprintf(os.Stderr, "session %s stored in %s\n", sess.ID, *dbPath)
	if err := emit(rep, *asJSON); err != nil {
		return err
	}
	printTopUnhandled(res.Unhandled, 20)
	return nil
}

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	dbPath := fs.String("db", "probe.db", "local store path")
	sessionID := fs.String("session", "", "session id")
	asJSON := fs.Bool("json", false, "emit JSON report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" {
		return fmt.Errorf("--session required")
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()

	sess, totals, err := db.LoadSession(ctx, *sessionID)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	cov, err := db.LoadCoverage(ctx, *sessionID)
	if err != nil {
		return err
	}
	notes, err := db.LoadReconciliations(ctx, *sessionID)
	if err != nil {
		return err
	}
	return emit(report.Build(sess, totals, cov, notes), *asJSON)
}

func cmdReconcile(args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	dbPath := fs.String("db", "probe.db", "local store path")
	sessionID := fs.String("session", "", "session id")
	category := fs.String("category", "", "category")
	ingame := fs.String("ingame", "", "in-game value")
	captured := fs.String("captured", "", "captured value")
	result := fs.String("result", "", "pass|fail")
	notes := fs.String("notes", "", "notes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" || *category == "" || (*result != "pass" && *result != "fail") {
		return fmt.Errorf("--session, --category and --result(pass|fail) required")
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	err = db.AddReconciliation(context.Background(), model.ReconciliationNote{
		SessionID: *sessionID, Category: model.Category(*category),
		CapturedValue: *captured, IngameValue: *ingame, Result: *result,
		Notes: *notes, CreatedAt: nowMS(),
	})
	if err == nil {
		fmt.Println("reconciliation recorded")
	}
	return err
}

func cmdGenFixture(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("genfixture needs an output path")
	}
	if err := capture.WriteSyntheticFixture(args[0]); err != nil {
		return err
	}
	fmt.Println("wrote", args[0])
	return nil
}

// printTopUnhandled lists the most frequent decoded-but-unclassified codes, so
// we can discover the real codes an activity fires and map them. E=event,
// R=response, Q=request.
func printTopUnhandled(m map[string]int, n int) {
	if len(m) == 0 {
		return
	}
	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	fmt.Printf("\n  Top unhandled codes (kind:code → count):\n")
	for i := 0; i < n && i < len(list); i++ {
		fmt.Printf("  %-10s %d\n", list[i].k, list[i].v)
	}
}

func dumpKind(k probe.Kind) string {
	switch k {
	case probe.KindResponse:
		return "R"
	case probe.KindRequest:
		return "Q"
	default:
		return "E"
	}
}

// formatParams renders a param table as "key=type(value)" sorted by key, with
// values truncated, so we can read which index holds which field.
func formatParams(params map[byte]interface{}) string {
	keys := make([]int, 0, len(params))
	for k := range params {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d=%s", k, shortVal(params[byte(k)])))
	}
	return strings.Join(parts, " ")
}

func shortVal(v interface{}) string {
	switch t := v.(type) {
	case string:
		if len(t) > 24 {
			t = t[:24] + "…"
		}
		return fmt.Sprintf("str(%q)", t)
	case []string:
		return fmt.Sprintf("[]str(n=%d)", len(t))
	case []byte:
		return fmt.Sprintf("[]byte(n=%d)", len(t))
	case []int32:
		return fmt.Sprintf("[]i32(n=%d)", len(t))
	case []interface{}:
		return fmt.Sprintf("[]any(n=%d)", len(t))
	case map[interface{}]interface{}:
		return fmt.Sprintf("map(n=%d)", len(t))
	default:
		s := fmt.Sprintf("%T(%v)", v, v)
		if len(s) > 32 {
			s = s[:32] + "…"
		}
		return s
	}
}

func emit(rep report.CoverageReport, asJSON bool) error {
	if asJSON {
		b, err := rep.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Print(rep.Text())
	return nil
}
