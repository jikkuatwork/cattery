package server

import (
	"bytes"
	"testing"
	"time"
)

func TestResolveServerChunkSizePrefersConfig(t *testing.T) {
	var stderr bytes.Buffer

	got, err := resolveServerChunkSize(20*time.Second, "45s", &stderr)
	if err != nil {
		t.Fatalf("resolveServerChunkSize() error = %v", err)
	}
	if got != 20*time.Second {
		t.Fatalf("chunk size = %s, want 20s", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestResolveServerChunkSizeUsesEnvWhenConfigUnset(t *testing.T) {
	got, err := resolveServerChunkSize(0, "45", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("resolveServerChunkSize() error = %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("chunk size = %s, want 45s", got)
	}
}
