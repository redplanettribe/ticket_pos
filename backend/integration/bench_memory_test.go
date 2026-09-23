package integration

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The memory instruments the opt-in export benchmarks share (#530, #661, ADR
// 0075): a poll of the Go runtime's peak while a download runs, and the kernel's
// resident-set high-water mark, which is what an instance's memory limit is
// actually enforced against.

// resetPeakRSS resets the kernel's resident-set high-water mark for
// this process ("5" to clear_refs, Linux 4.0 and later). Best effort: where the
// file is absent the next reading is the process's lifetime peak, or nothing.
func resetPeakRSS() {
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0)
}

// peakRSS reads this process's resident-set high-water mark in bytes,
// or zero where /proc does not report one.
func peakRSS() uint64 {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			if err != nil {
				return 0
			}
			return kb << 10
		}
	}
	return 0
}

// sampleMemory polls the Go runtime until the returned function is
// called, and that function reports the highest live heap and the highest Sys it
// saw. ReadMemStats stops the world for microseconds; at this interval that is
// noise beside a build measured in seconds, and a coarser interval would risk
// polling either side of the peak.
func sampleMemory() func() (peakHeap, peakSys uint64) {
	var (
		mu         sync.Mutex
		heap, sys  uint64
		done       = make(chan struct{})
		finished   = make(chan struct{})
		readSample = func() {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			mu.Lock()
			heap, sys = max(heap, m.HeapAlloc), max(sys, m.Sys)
			mu.Unlock()
		}
	)
	go func() {
		defer close(finished)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				readSample()
			}
		}
	}()
	return func() (uint64, uint64) {
		close(done)
		<-finished
		readSample()
		mu.Lock()
		defer mu.Unlock()
		return heap, sys
	}
}

func mib(bytes uint64) float64 { return float64(bytes) / (1 << 20) }

func benchEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q is not a positive number", name, raw)
	}
	return n
}
