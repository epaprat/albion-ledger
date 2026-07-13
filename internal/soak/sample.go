package soak

import "runtime"

// Sample takes one live-heap measurement at elapsedMS. It forces a GC first so
// HeapInuse reflects *retained* memory (an unbounded structure), not garbage that
// the next collection would reclaim — this is what makes a growth trend meaningful
// (research.md Decision 1). Kept out of soak.go so the analyzer stays pure/testable
// without touching the runtime.
func Sample(x int64) HeapSample {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return HeapSample{
		X:              x,
		HeapInuseBytes: int64(m.HeapInuse),
		HeapAllocBytes: int64(m.HeapAlloc),
	}
}
