package capture

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// countOnePass returns how many Albion payloads a single replay of path emits.
func countOnePass(t *testing.T, path string) int {
	t.Helper()
	ch, err := NewReplay(path).Packets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for range ch {
		n++
	}
	return n
}

// A short fixture is looped to reach an event target well above one pass → the
// loop count rises above 1 and the emitted total reaches the target (FR-003).
func TestLoopReplayReachesEventTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.pcap")
	if err := WriteSyntheticFixture(path); err != nil {
		t.Fatal(err)
	}
	perPass := countOnePass(t, path)
	if perPass == 0 {
		t.Fatal("fixture emitted zero payloads")
	}
	target := perPass*3 + 1 // forces at least 4 passes

	lr := NewLoopReplay(path, target, 0)
	ch, err := lr.Packets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for range ch {
		got++
	}
	if got < target {
		t.Errorf("emitted %d, want >= target %d", got, target)
	}
	if lr.Iterations() < 4 {
		t.Errorf("iterations = %d, want >= 4 for a %d-target over %d/pass", lr.Iterations(), target, perPass)
	}
	if lr.Emitted() != got {
		t.Errorf("Emitted() = %d, drained %d", lr.Emitted(), got)
	}
}

// ctx cancellation stops the loop promptly without draining the whole target.
func TestLoopReplayHonorsCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.pcap")
	if err := WriteSyntheticFixture(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// huge target so only cancel can stop it
	lr := NewLoopReplay(path, 1<<30, 0)
	ch, err := lr.Packets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// drain; the channel must close after cancel rather than run to the 1<<30 target.
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop after cancel")
	}
}

// A duration bound stops the loop even when the event bound is unset.
func TestLoopReplayDurationBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.pcap")
	if err := WriteSyntheticFixture(path); err != nil {
		t.Fatal(err)
	}
	lr := NewLoopReplay(path, 0, 50*time.Millisecond)
	ch, err := lr.Packets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for range ch {
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("duration-bounded loop ran too long: %v", elapsed)
	}
	if lr.Iterations() < 1 {
		t.Error("at least one pass should run")
	}
}
