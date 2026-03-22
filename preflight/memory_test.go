package preflight

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLinuxMemAvailableMB(t *testing.T) {
	data := []byte("MemTotal: 2048000 kB\nMemAvailable: 1048576 kB\n")

	if got, want := parseLinuxMemAvailableMB(data), 1024; got != want {
		t.Fatalf("parseLinuxMemAvailableMB() = %d, want %d", got, want)
	}
}

func TestAutoChunkSizeTable(t *testing.T) {
	tests := []struct {
		name      string
		available int
		want      time.Duration
	}{
		{name: "unknown", available: -1, want: DefaultChunkSize},
		{name: "512mb", available: 512, want: 10 * time.Second},
		{name: "1gb", available: 1024, want: 15 * time.Second},
		{name: "2gb", available: 2048, want: 20 * time.Second},
		{name: "4gb", available: 4096, want: 30 * time.Second},
		{name: "8gb", available: 8192, want: 45 * time.Second},
		{name: "over8gb", available: 16384, want: 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AutoChunkSize(tc.available); got != tc.want {
				t.Fatalf("AutoChunkSize(%d) = %s, want %s", tc.available, got, tc.want)
			}
		})
	}
}

func TestParseChunkSizeOverrideAcceptsDurationsAndBareSeconds(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{value: "15", want: 15 * time.Second},
		{value: "15s", want: 15 * time.Second},
		{value: "1m", want: 60 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			got, err := ParseChunkSizeOverride("--chunk-size", tc.value)
			if err != nil {
				t.Fatalf("ParseChunkSizeOverride(%q) error = %v", tc.value, err)
			}
			if got != tc.want {
				t.Fatalf("ParseChunkSizeOverride(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseChunkSizeOverrideRejectsOutOfRange(t *testing.T) {
	_, err := ParseChunkSizeOverride("--chunk-size", "61s")
	if err == nil {
		t.Fatal("ParseChunkSizeOverride(61s) error = nil")
	}
	if !strings.Contains(err.Error(), "10s and 60s") {
		t.Fatalf("error = %q, want bounds message", err)
	}
}

func TestResolveChunkSizePrecedence(t *testing.T) {
	explicit, err := ResolveChunkSize("20s", "--chunk-size", "45s")
	if err != nil {
		t.Fatalf("ResolveChunkSize(explicit) error = %v", err)
	}
	if explicit.Source != ChunkSizeSourceExplicit {
		t.Fatalf("explicit source = %q, want %q", explicit.Source, ChunkSizeSourceExplicit)
	}
	if explicit.Duration != 20*time.Second {
		t.Fatalf("explicit duration = %s, want 20s", explicit.Duration)
	}

	envOnly, err := ResolveChunkSize("", "--chunk-size", "45")
	if err != nil {
		t.Fatalf("ResolveChunkSize(env) error = %v", err)
	}
	if envOnly.Source != ChunkSizeSourceEnv {
		t.Fatalf("env source = %q, want %q", envOnly.Source, ChunkSizeSourceEnv)
	}
	if envOnly.Duration != 45*time.Second {
		t.Fatalf("env duration = %s, want 45s", envOnly.Duration)
	}
}

func TestWarnLowMemoryChunkSizePrintsOnce(t *testing.T) {
	lowMemoryWarningOnce = sync.Once{}

	var buf bytes.Buffer
	resolution := ChunkSizeResolution{
		Duration:          MinChunkSize,
		Source:            ChunkSizeSourceAuto,
		AvailableMemoryMB: 512,
	}

	WarnLowMemoryChunkSize(&buf, resolution)
	WarnLowMemoryChunkSize(&buf, resolution)

	got := strings.TrimSpace(buf.String())
	lines := strings.Split(got, "\n")
	if len(lines) != 1 {
		t.Fatalf("warning lines = %d, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "using minimum 10s chunk size") {
		t.Fatalf("warning = %q", lines[0])
	}
}

func TestGuardMemoryErrorNormalizesKnownFailures(t *testing.T) {
	err := GuardMemoryError("speech synthesis", func() error {
		return fmt.Errorf("std::bad_alloc")
	})
	if err == nil {
		t.Fatal("GuardMemoryError(error) = nil")
	}
	if got, want := err.Error(), "out of memory during speech synthesis"; got != want {
		t.Fatalf("GuardMemoryError(error) = %q, want %q", got, want)
	}

	err = GuardMemoryError("transcription", func() error {
		panic("cannot allocate memory")
	})
	if err == nil {
		t.Fatal("GuardMemoryError(panic) = nil")
	}
	if got, want := err.Error(), "out of memory during transcription"; got != want {
		t.Fatalf("GuardMemoryError(panic) = %q, want %q", got, want)
	}
}

func TestGuardMemoryErrorRepanicsUnknownPanics(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("GuardMemoryError() did not re-panic")
		}
	}()

	_ = GuardMemoryError("transcription", func() error {
		panic("boom")
	})
}
