package download

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// progressWriter wraps an io.Writer and prints a progress bar to stderr.
type progressWriter struct {
	w           io.Writer
	label       string
	total       int64
	written     int64
	start       time.Time
	mu          sync.Mutex
	lastUpdate  time.Time
}

func newProgressWriter(w io.Writer, label string, total int64) *progressWriter {
	return &progressWriter{
		w:     w,
		label: label,
		total: total,
		start: time.Now(),
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.mu.Lock()
	pw.written += int64(n)
	now := time.Now()
	// Throttle updates to 10Hz
	if now.Sub(pw.lastUpdate) > 100*time.Millisecond || pw.written == pw.total {
		pw.render()
		pw.lastUpdate = now
	}
	pw.mu.Unlock()
	return n, err
}

func (pw *progressWriter) render() {
	width := termWidth()
	elapsed := time.Since(pw.start).Seconds()
	if elapsed == 0 {
		elapsed = 0.001
	}

	speed := float64(pw.written) / elapsed
	speedStr := formatBytes(int64(speed)) + "/s"

	var pct float64
	if pw.total > 0 {
		pct = float64(pw.written) / float64(pw.total) * 100
	}

	status := fmt.Sprintf("%s  %s / %s  %s  %.0f%%",
		pw.label,
		formatBytes(pw.written),
		formatBytes(pw.total),
		speedStr,
		pct,
	)

	// Progress bar fills remaining space
	barSpace := width - len(status) - 4 // 4 for " [] "
	if barSpace < 10 {
		barSpace = 10
	}

	filled := 0
	if pw.total > 0 {
		filled = int(float64(barSpace) * float64(pw.written) / float64(pw.total))
	}
	if filled > barSpace {
		filled = barSpace
	}

	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barSpace-filled) + "]"

	fmt.Fprintf(os.Stderr, "\r%s %s", status, bar)

	if pw.written == pw.total {
		fmt.Fprintln(os.Stderr)
	}
}

func (pw *progressWriter) finish() {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if pw.written < pw.total {
		fmt.Fprintln(os.Stderr)
	}
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

func termWidth() int {
	w, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}
