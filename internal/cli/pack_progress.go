package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// packProgress renders a single-line, multi-layer packing status to an
// io.Writer. It is safe for concurrent byte updates from pack goroutines.
//
// Output format (redrawn ~4x/sec on a terminal):
//
//	Packing: agent 124MB, deps 0B, project 5MB, build-cache 890MB
//
// Layers appear in the order they are registered. A layer's counter stops
// updating once MarkDone is called, but its final byte total remains visible
// until Stop is called, at which point the line is cleared.
type packProgress struct {
	out         io.Writer
	interactive bool

	mu     sync.Mutex
	layers []*layerProgress // insertion order
	index  map[string]*layerProgress

	stopCh  chan struct{}
	doneCh  chan struct{}
	stopped atomic.Bool
}

type layerProgress struct {
	name  string
	bytes atomic.Int64 // uncompressed bytes processed so far
	done  atomic.Bool
}

// newPackProgress creates a progress renderer writing to out. When interactive
// is false (non-TTY, quiet mode, or tests), rendering is disabled and all
// methods become no-ops — bytes are still tracked but nothing is printed.
func newPackProgress(out io.Writer, interactive bool) *packProgress {
	return &packProgress{
		out:         out,
		interactive: interactive,
		index:       make(map[string]*layerProgress),
	}
}

// Register adds a layer to the tracker. Must be called before Start and
// before any Progress callbacks fire.
func (p *packProgress) Register(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.index[name]; ok {
		return
	}
	lp := &layerProgress{name: name}
	p.layers = append(p.layers, lp)
	p.index[name] = lp
}

// Progress returns a callback suitable for PackOptions.Progress that updates
// the named layer's byte total. Returns nil if interactive is false, so
// callers can skip wrapping entirely.
func (p *packProgress) Progress(name string) func(int64) {
	if !p.interactive {
		return nil
	}
	p.mu.Lock()
	lp := p.index[name]
	p.mu.Unlock()
	if lp == nil {
		return nil
	}
	return func(b int64) { lp.bytes.Store(b) }
}

// MarkDone signals that a layer's packing has finished. Its byte total stops
// updating; the final value remains displayed.
func (p *packProgress) MarkDone(name string) {
	p.mu.Lock()
	lp := p.index[name]
	p.mu.Unlock()
	if lp != nil {
		lp.done.Store(true)
	}
}

// Start begins rendering. No-op if not interactive or no layers registered.
func (p *packProgress) Start() {
	if !p.interactive || len(p.layers) == 0 {
		return
	}
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	go p.loop()
}

// Stop halts rendering and clears the progress line. Safe to call multiple
// times and safe to call even if Start was not called.
func (p *packProgress) Stop() {
	if p.stopCh == nil {
		return
	}
	if !p.stopped.CompareAndSwap(false, true) {
		return
	}
	close(p.stopCh)
	<-p.doneCh
	// Erase the line so subsequent output starts from a clean column.
	fmt.Fprint(p.out, "\r\033[2K")
}

func (p *packProgress) loop() {
	defer close(p.doneCh)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	// Draw once immediately so the user sees something before the first tick.
	p.draw()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.draw()
		}
	}
}

func (p *packProgress) draw() {
	var sb strings.Builder
	sb.WriteString("\r\033[2KPacking: ")
	for i, lp := range p.layers {
		if i > 0 {
			sb.WriteString(", ")
		}
		b := lp.bytes.Load()
		marker := ""
		if lp.done.Load() {
			marker = "✓"
		}
		fmt.Fprintf(&sb, "%s %s%s", lp.name, formatSize(int(b)), marker)
	}
	fmt.Fprint(p.out, sb.String())
}
