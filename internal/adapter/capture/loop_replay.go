package capture

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/gopacket/gopacket/pcapgo"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// LoopReplay is a PacketSource that re-emits a recorded pcap until a target event
// count or wall-clock duration is reached, re-opening the file each pass (feature
// 023, US1). It exists so a seconds-long fixture can drive a multi-hour soak that
// stresses exactly the bounded caches/maps Principle XI caps — a correct build's
// heap returns to baseline each loop, so a climb across loops is the real leak
// signal (research.md Decision 2). Iterations() reports the loop count so a looped
// run is never silently short (FR-003).
type LoopReplay struct {
	path         string
	targetEvents int           // stop after this many emitted payloads (0 = unused)
	targetDur    time.Duration // stop after this elapsed (0 = unused)
	now          func() time.Time
	iterations   int64
	emitted      int64
	status       model.CaptureStatus
}

// NewLoopReplay loops path until targetEvents payloads are emitted or targetDur
// elapses (whichever comes first); a zero bound is ignored. At least one full pass
// always runs. now defaults to time.Now.
func NewLoopReplay(path string, targetEvents int, targetDur time.Duration) *LoopReplay {
	return &LoopReplay{
		path:         path,
		targetEvents: targetEvents,
		targetDur:    targetDur,
		now:          time.Now,
		status:       model.CaptureStatus{Interface: path},
	}
}

// Iterations returns how many full passes over the file have been started.
func (l *LoopReplay) Iterations() int { return int(atomic.LoadInt64(&l.iterations)) }

// Emitted returns how many payloads have been emitted so far.
func (l *LoopReplay) Emitted() int { return int(atomic.LoadInt64(&l.emitted)) }

// Status returns the source status.
func (l *LoopReplay) Status() model.CaptureStatus { return l.status }

// Packets streams the looped payloads, closing when a target is reached or ctx is done.
func (l *LoopReplay) Packets(ctx context.Context) (<-chan []byte, error) {
	// A run with no stop target would loop forever — reject it loudly rather than
	// hang the consumer (review 023).
	if l.targetEvents <= 0 && l.targetDur <= 0 {
		return nil, fmt.Errorf("loop replay needs a positive event or duration bound")
	}
	// Open once up front so a bad path fails loudly before the goroutine starts.
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	f.Close()

	out := make(chan []byte)
	start := l.now()
	go func() {
		defer close(out)
		for {
			if l.reached(start) {
				return
			}
			atomic.AddInt64(&l.iterations, 1)
			before := atomic.LoadInt64(&l.emitted)
			done, err := l.emitOnce(ctx, out, start)
			if err != nil || done {
				return
			}
			// A full pass that emitted nothing can never reach an event target —
			// stop instead of re-opening the file forever (review 023).
			if atomic.LoadInt64(&l.emitted) == before {
				return
			}
		}
	}()
	return out, nil
}

// emitOnce replays the file once. Returns done=true when a target/ctx stopped it
// mid-file (so the caller must not loop again).
func (l *LoopReplay) emitOnce(ctx context.Context, out chan<- []byte, start time.Time) (bool, error) {
	f, err := os.Open(l.path)
	if err != nil {
		return true, err
	}
	defer f.Close()
	reader, err := pcapgo.NewReader(f)
	if err != nil {
		return true, err
	}
	for {
		data, _, err := reader.ReadPacketData()
		if err != nil {
			return false, nil // EOF → let the outer loop decide to re-open
		}
		payload, ok := extractAlbionPayload(data)
		if !ok {
			continue
		}
		select {
		case <-ctx.Done():
			return true, nil
		case out <- payload:
			atomic.AddInt64(&l.emitted, 1)
			if l.reached(start) {
				return true, nil
			}
		}
	}
}

// reached reports whether a configured stop target has been met.
func (l *LoopReplay) reached(start time.Time) bool {
	if l.targetEvents > 0 && atomic.LoadInt64(&l.emitted) >= int64(l.targetEvents) {
		return true
	}
	if l.targetDur > 0 && l.now().Sub(start) >= l.targetDur {
		return true
	}
	return false
}
