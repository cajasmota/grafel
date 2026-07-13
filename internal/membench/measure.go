package membench

import (
	"runtime"
	"sync"
	"time"
)

// Sample is a heap measurement snapshot in bytes.
type Sample struct {
	HeapAlloc uint64 // bytes of allocated heap objects still reachable
	HeapInuse uint64 // bytes in in-use heap spans (closer to RSS pressure)
}

// snapshot forces a GC then reads a settled heap sample. Used for
// before/after (retained) measurements where we want the quiescent value.
func snapshot() Sample {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Sample{HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse}
}

// peakSampler polls HeapInuse/HeapAlloc on an interval and records the maximum
// observed, so we can capture the transient peak of a running pipeline that is
// released before it returns.
type peakSampler struct {
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	peakIn   uint64
	peakAll  uint64
	interval time.Duration
}

func startPeakSampler(interval time.Duration) *peakSampler {
	p := &peakSampler{stop: make(chan struct{}), done: make(chan struct{}), interval: interval}
	go p.run()
	return p
}

func (p *peakSampler) observe() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	p.mu.Lock()
	if m.HeapInuse > p.peakIn {
		p.peakIn = m.HeapInuse
	}
	if m.HeapAlloc > p.peakAll {
		p.peakAll = m.HeapAlloc
	}
	p.mu.Unlock()
}

func (p *peakSampler) run() {
	defer close(p.done)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			p.observe() // final read
			return
		case <-t.C:
			p.observe()
		}
	}
}

// Stop halts sampling and returns the peak HeapInuse / HeapAlloc seen.
func (p *peakSampler) Stop() (peakInuse, peakAlloc uint64) {
	close(p.stop)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peakIn, p.peakAll
}

// mb converts bytes to whole megabytes for reporting.
func mb(b uint64) uint64 { return b / (1024 * 1024) }
