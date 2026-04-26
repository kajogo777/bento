package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPackProgressNonInteractiveIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	p := newPackProgress(&buf, false)
	p.Register("agent")
	// Progress returns nil in non-interactive mode so callers can skip
	// wrapping the writer entirely.
	if got := p.Progress("agent"); got != nil {
		t.Fatal("Progress should return nil in non-interactive mode")
	}
	p.Start()
	p.MarkDone("agent")
	p.Stop()
	if buf.Len() != 0 {
		t.Fatalf("non-interactive mode should not write anything, got %q", buf.String())
	}
}

func TestPackProgressInteractiveRenders(t *testing.T) {
	var buf syncBuffer
	p := newPackProgress(&buf, true)
	p.Register("agent")
	p.Register("deps")
	p.Start()

	// Simulate a packing goroutine reporting bytes.
	cb := p.Progress("agent")
	if cb == nil {
		t.Fatal("Progress returned nil in interactive mode")
	}
	cb(1 << 20) // 1 MiB
	cb(5 << 20) // 5 MiB
	p.MarkDone("deps")

	// Wait for at least one tick to render.
	time.Sleep(350 * time.Millisecond)
	p.Stop()

	out := buf.String()
	if !strings.Contains(out, "Packing:") {
		t.Fatalf("expected Packing prefix, got %q", out)
	}
	if !strings.Contains(out, "agent") || !strings.Contains(out, "deps") {
		t.Fatalf("expected both layer names, got %q", out)
	}
	if !strings.Contains(out, "5.0MB") && !strings.Contains(out, "5MB") {
		t.Fatalf("expected agent byte total of 5 MiB, got %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Fatalf("expected done marker for deps layer, got %q", out)
	}
}

func TestPackProgressStopIsIdempotent(t *testing.T) {
	var buf syncBuffer
	p := newPackProgress(&buf, true)
	p.Register("a")
	p.Start()
	p.Stop()
	p.Stop() // must not panic
}

func TestPackProgressConcurrentUpdates(t *testing.T) {
	var buf syncBuffer
	p := newPackProgress(&buf, true)
	for _, name := range []string{"agent", "deps", "project"} {
		p.Register(name)
	}
	p.Start()

	// Hammer byte updates concurrently across three goroutines. With
	// -race this would detect any unsynchronized access.
	var wg sync.WaitGroup
	for _, name := range []string{"agent", "deps", "project"} {
		cb := p.Progress(name)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int64(0); i < 1000; i++ {
				cb(i * (1 << 20))
			}
		}()
	}
	wg.Wait()
	p.Stop()
}

// syncBuffer is a concurrency-safe bytes.Buffer for tests.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
