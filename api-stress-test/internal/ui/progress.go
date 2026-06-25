package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Progress tracks and displays live progress during a stress test.
type Progress struct {
	w              io.Writer
	total          int64 // 0 for duration mode (unknown total)
	completed      atomic.Int64
	startTime      time.Time
	done           chan struct{}
	wg             sync.WaitGroup
	isDurationMode bool
	duration       time.Duration
}

// NewProgress creates a live progress display.
// Set total to 0 for duration mode (unknown total).
func NewProgress(w io.Writer, total int64, isDurationMode bool, duration time.Duration) *Progress {
	return &Progress{
		w:              w,
		total:          total,
		startTime:      time.Now(),
		done:           make(chan struct{}),
		isDurationMode: isDurationMode,
		duration:       duration,
	}
}

// Add adds n to the completed count. Thread-safe.
func (p *Progress) Add(n int64) {
	p.completed.Add(n)
}

// Start begins the progress display goroutine. Call Stop() when done.
func (p *Progress) Start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		var lastCount int64
		var lastTime time.Time = p.startTime

		for {
			select {
			case <-ticker.C:
				now := time.Now()
				current := p.completed.Load()
				elapsed := now.Sub(p.startTime)

				// Calculate instant req/s from last 500ms window
				dt := now.Sub(lastTime).Seconds()
				var instantRPS float64
				if dt > 0 {
					instantRPS = float64(current-lastCount) / dt
				}
				lastCount = current
				lastTime = now

				p.render(current, elapsed, instantRPS)
			case <-p.done:
				// Clear the progress line
				fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", 80))
				return
			}
		}
	}()
}

// Stop ends the progress display and waits for the goroutine to exit.
func (p *Progress) Stop() {
	close(p.done)
	p.wg.Wait()
}

func (p *Progress) render(completed int64, elapsed time.Duration, rps float64) {
	if p.isDurationMode {
		// Duration mode: show elapsed/total time
		pct := float64(elapsed) / float64(p.duration) * 100
		if pct > 100 {
			pct = 100
		}
		bar := renderBar(pct, 20)
		fmt.Fprintf(p.w, "\r%s %3.0f%% | %d reqs | %.0f req/s | %.1fs/%.1fs",
			bar, pct, completed, rps, elapsed.Seconds(), p.duration.Seconds())
	} else {
		// Fixed request mode
		pct := float64(completed) / float64(p.total) * 100
		if pct > 100 {
			pct = 100
		}
		bar := renderBar(pct, 20)
		fmt.Fprintf(p.w, "\r%s %3.0f%% | %d/%d | %.0f req/s | %.1fs elapsed",
			bar, pct, completed, p.total, rps, elapsed.Seconds())
	}
}

func renderBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", empty) + "]"
}
