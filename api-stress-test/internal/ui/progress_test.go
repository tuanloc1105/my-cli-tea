package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressStartStop(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, 100, false, 0)
	p.Start()
	time.Sleep(50 * time.Millisecond)
	p.Stop()
	// Should not panic or deadlock
}

func TestProgressAdd(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, 100, false, 0)
	p.Start()
	p.Add(10)
	p.Add(20)
	if got := p.completed.Load(); got != 30 {
		t.Errorf("completed = %d, want 30", got)
	}
	p.Stop()
}

func TestProgressStopClearsLine(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf, 10, false, 0)
	p.Start()
	p.Add(5)
	time.Sleep(600 * time.Millisecond) // Wait for at least one render
	p.Stop()
	out := buf.String()
	// After stop, the output should contain \r (carriage return for clearing)
	if len(out) > 0 && !strings.Contains(out, "\r") {
		t.Error("expected \\r in output after Stop()")
	}
}
