package download

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const progressBarWidth = 40

// barStyle holds shared layout settings so all bars align.
type barStyle struct {
	labelWidth int
}

// bar is a single progress bar line.
type bar struct {
	label   string
	total   int64
	current int64
	isBytes bool
	style   *barStyle
	mu      sync.Mutex
	last    time.Time
	done    bool
}

func newBar(label string, total int64, isBytes bool, style *barStyle) *bar {
	return &bar{
		label:   label,
		total:   total,
		isBytes: isBytes,
		style:   style,
	}
}

// Write implements io.Writer so the bar can wrap download streams.
func (b *bar) Write(p []byte) (int, error) {
	n := len(p)
	b.mu.Lock()
	b.current += int64(n)
	now := time.Now()
	if now.Sub(b.last) > 80*time.Millisecond || b.current >= b.total {
		b.render()
		b.last = now
	}
	b.mu.Unlock()
	return n, nil
}

// set updates the current value (for count bars).
func (b *bar) set(v int64) {
	b.mu.Lock()
	b.current = v
	b.render()
	b.mu.Unlock()
}

// finish marks the bar complete and prints a newline.
func (b *bar) finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.done {
		b.current = b.total
		b.render()
		fmt.Fprintln(os.Stderr)
		b.done = true
	}
}

func (b *bar) render() {
	// Label padded to shared width
	label := fmt.Sprintf("%-*s", b.style.labelWidth, b.label)

	// Progress bar: [===>                  ]
	filled := 0
	if b.total > 0 {
		filled = int(float64(progressBarWidth) * float64(b.current) / float64(b.total))
	}
	if filled > progressBarWidth {
		filled = progressBarWidth
	}

	var barStr [progressBarWidth]byte
	for i := range barStr {
		if i < filled {
			barStr[i] = '='
		} else {
			barStr[i] = ' '
		}
	}
	if filled > 0 && filled < progressBarWidth {
		barStr[filled-1] = '>'
	}

	green := "\033[32m"
	reset := "\033[0m"

	// Counters: right-aligned, fixed-width so layout never shifts
	var counters string
	if b.isBytes {
		tot := fmtMB(b.total)
		w := len(tot)
		counters = fmt.Sprintf("%*s / %s", w, fmtMB(b.current), tot)
	} else {
		tot := fmt.Sprintf("%d", b.total)
		w := len(tot)
		counters = fmt.Sprintf("%*d / %s", w, b.current, tot)
	}

	fmt.Fprintf(os.Stderr, "\r%s [%s%s%s] %s", label, green, string(barStr[:]), reset, counters)
}

// fmtMB formats bytes as "XXmb" with no decimal.
func fmtMB(b int64) string {
	mb := (b + 1<<19) >> 20 // round to nearest MB
	return fmt.Sprintf("%dMB", mb)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
