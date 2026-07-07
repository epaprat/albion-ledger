package wailsadapter

// CSV export (feature 013): thin dialog/IO shell over the pure internal/export
// package. buildDataset maps a dataset key to its snapshot getter — the same
// mutex-copied views the UI renders, so exports are consistent by construction
// and never touch the capture goroutine (contract §4.1). Dialogs need the wails
// context, handed over once at OnStartup via SetUIContext.

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/epaprat/albion-ledger/internal/export"
)

// realUser resolves the human behind the process. The app runs under sudo (pcap
// needs root), so os.UserHomeDir() is root's home — dialogs would default to
// /var/root and written files would be root-owned. SUDO_USER names the real user.
func realUser() (*user.User, bool) {
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		return nil, false
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, false
	}
	return u, true
}

// defaultExportDir picks the save-dialog starting point: the real user's
// Documents folder when running under sudo, else the process user's.
func defaultExportDir() string {
	home := ""
	if u, ok := realUser(); ok {
		home = u.HomeDir
	} else if h, err := os.UserHomeDir(); err == nil {
		home = h
	}
	if home == "" {
		return ""
	}
	docs := filepath.Join(home, "Documents")
	if fi, err := os.Stat(docs); err == nil && fi.IsDir() {
		return docs
	}
	return home
}

// chownToRealUser hands a root-written export back to the human user so they
// can open/move/delete it without privileges. Best-effort.
func chownToRealUser(path string) {
	u, ok := realUser()
	if !ok {
		return
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return
	}
	_ = os.Chown(path, uid, gid)
}

// DatasetKeys is the fixed export order (contract §4.4) — no dataset is ever
// silently skipped.
var DatasetKeys = []string{"holdings", "flow", "zones", "market", "spec"}

// ExportResult reports one dataset export back to the UI (data-model.md).
type ExportResult struct {
	Dataset  string `json:"dataset"`
	Path     string `json:"path"`
	Rows     int    `json:"rows"`
	Canceled bool   `json:"canceled"`
	Err      string `json:"err"`
}

// SetUIContext hands the wails runtime context to the service (OnStartup, once);
// the native dialogs need it.
func (s *Service) SetUIContext(ctx context.Context) {
	s.mu.Lock()
	s.uiCtx = ctx
	s.mu.Unlock()
}

func (s *Service) uiContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uiCtx
}

// buildDataset resolves a dataset key to its contract §2 header+rows using the
// existing snapshot getters. window applies to zones only; empty falls back to
// "session" inside ZoneStats.
func (s *Service) buildDataset(key, window string) ([]string, [][]string, error) {
	switch key {
	case "holdings":
		h, r := export.HoldingsRows(s.ListHoldings())
		return h, r, nil
	case "flow":
		h, r := export.FlowRows(s.ListFlow())
		return h, r, nil
	case "zones":
		h, r := export.ZoneRows(s.ZoneStats(window))
		return h, r, nil
	case "market":
		h, r := export.MarketRows(s.ListItems())
		return h, r, nil
	case "spec":
		h, r := export.SpecRows(s.Spec().Masteries)
		return h, r, nil
	}
	return nil, nil, fmt.Errorf("unknown dataset %q", key)
}

// writeDataset renders one dataset to path. On a write error any partial file
// is removed (contract §4.3).
func (s *Service) writeDataset(key, window, path string) ExportResult {
	header, rows, err := s.buildDataset(key, window)
	if err != nil {
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	f, err := os.Create(path)
	if err != nil {
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	if err := export.Encode(f, header, rows); err != nil {
		f.Close()
		os.Remove(path)
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	chownToRealUser(path)
	return ExportResult{Dataset: key, Path: path, Rows: len(rows)}
}

// ExportDataset saves one dataset via the native save dialog. Cancel returns
// {Canceled:true} — no file, no error (contract §3).
func (s *Service) ExportDataset(key, window string) ExportResult {
	// Validate the key BEFORE opening a dialog — an unknown key must not prompt.
	if _, _, err := s.buildDataset(key, window); err != nil {
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	ctx := s.uiContext()
	if ctx == nil {
		return ExportResult{Dataset: key, Err: "UI not ready"}
	}
	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		DefaultDirectory: defaultExportDir(),
		DefaultFilename:  export.Filename(key, time.Now()),
		Title:            "Export " + key + " as CSV",
		Filters:          []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil {
		return ExportResult{Dataset: key, Err: err.Error()}
	}
	if path == "" {
		return ExportResult{Dataset: key, Canceled: true}
	}
	return s.writeDataset(key, window, path)
}

// ExportAll saves every dataset into one user-chosen directory with a single
// timestamp. Each dataset is independent — one failure never stops the rest
// (contract §4.4); directory cancel returns all-canceled.
func (s *Service) ExportAll(window string) []ExportResult {
	ctx := s.uiContext()
	if ctx == nil {
		return allErr("UI not ready")
	}
	dir, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		DefaultDirectory: defaultExportDir(),
		Title:            "Export all datasets (CSV per dataset)",
	})
	if err != nil {
		return allErr(err.Error())
	}
	if dir == "" {
		out := make([]ExportResult, 0, len(DatasetKeys))
		for _, key := range DatasetKeys {
			out = append(out, ExportResult{Dataset: key, Canceled: true})
		}
		return out
	}
	return s.exportAllTo(dir, window, time.Now())
}

// exportAllTo is the dialog-free core of ExportAll (testable directly).
func (s *Service) exportAllTo(dir, window string, stamp time.Time) []ExportResult {
	out := make([]ExportResult, 0, len(DatasetKeys))
	for _, key := range DatasetKeys {
		path := filepath.Join(dir, export.Filename(key, stamp))
		out = append(out, s.writeDataset(key, window, path))
	}
	return out
}

func allErr(msg string) []ExportResult {
	out := make([]ExportResult, 0, len(DatasetKeys))
	for _, key := range DatasetKeys {
		out = append(out, ExportResult{Dataset: key, Err: msg})
	}
	return out
}
