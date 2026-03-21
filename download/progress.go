package download

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[97m"
	colorBGDim  = "\033[48;5;236m"
	colorBGDone = "\033[48;5;22m"
)

// progressWriter wraps an io.Writer and prints a progress bar to stderr.
type progressWriter struct {
	w          io.Writer
	label      string
	total      int64
	written    int64
	start      time.Time
	mu         sync.Mutex
	lastUpdate time.Time
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
	if now.Sub(pw.lastUpdate) > 80*time.Millisecond || pw.written == pw.total {
		pw.render()
		pw.lastUpdate = now
	}
	pw.mu.Unlock()
	return n, err
}

func (pw *progressWriter) render() {
	width := termWidth()

	elapsed := time.Since(pw.start).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}

	speed := float64(pw.written) / elapsed
	done := pw.written == pw.total && pw.total > 0

	var pct float64
	if pw.total > 0 {
		pct = float64(pw.written) / float64(pw.total) * 100
	}

	// Fixed-width formatting to prevent layout jumping
	// "  Label           88.1 MB / 88.1 MB   12.3 MB/s  100%  [████████████████████]"
	label := fmt.Sprintf("%-16s", truncate(pw.label, 16))
	sizes := fmt.Sprintf("%8s / %-8s", formatBytes(pw.written), formatBytes(pw.total))
	speedStr := fmt.Sprintf("%8s/s", formatBytes(int64(speed)))
	pctStr := fmt.Sprintf("%3.0f%%", pct)

	// Bar width: total width - fixed parts - padding
	// label(16) + sizes(19) + speed(10) + pct(4) + bar borders(2) + spaces(6) = 57
	barWidth := width - 57
	if barWidth < 10 {
		barWidth = 10
	}

	filled := 0
	if pw.total > 0 {
		filled = int(float64(barWidth) * pct / 100)
	}
	if filled > barWidth {
		filled = barWidth
	}

	// Build colored bar
	var bar string
	if done {
		bar = colorGreen + "["
		for i := 0; i < barWidth; i++ {
			bar += "█"
		}
		bar += "]" + colorReset
	} else {
		bar = colorDim + "[" + colorReset + colorCyan
		for i := 0; i < filled; i++ {
			bar += "█"
		}
		bar += colorDim
		for i := filled; i < barWidth; i++ {
			bar += "░"
		}
		bar += "]" + colorReset
	}

	// Color the parts
	labelColor := colorWhite
	if done {
		labelColor = colorGreen
	}

	fmt.Fprintf(os.Stderr, "\r%s%s%s  %s  %s  %s  %s",
		labelColor, label, colorReset,
		sizes, speedStr, pctStr, bar)

	if done {
		fmt.Fprintln(os.Stderr)
	}
}

func (pw *progressWriter) finish() {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if pw.written < pw.total || pw.total == 0 {
		fmt.Fprintln(os.Stderr)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
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
