package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestResolveCommandChunkSizeWithEnvPrefersFlag(t *testing.T) {
	var stderr bytes.Buffer

	got, err := resolveCommandChunkSizeWithEnv("20s", "45s", &stderr)
	if err != nil {
		t.Fatalf("resolveCommandChunkSizeWithEnv() error = %v", err)
	}
	if got != 20*time.Second {
		t.Fatalf("chunk size = %s, want 20s", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestResolveCommandChunkSizeWithEnvUsesEnvWhenFlagMissing(t *testing.T) {
	got, err := resolveCommandChunkSizeWithEnv("", "45", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("resolveCommandChunkSizeWithEnv() error = %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("chunk size = %s, want 45s", got)
	}
}

func TestResolveCommandChunkSizeWithEnvRejectsInvalidEnv(t *testing.T) {
	_, err := resolveCommandChunkSizeWithEnv("", "abc", &bytes.Buffer{})
	if err == nil {
		t.Fatal("resolveCommandChunkSizeWithEnv() error = nil")
	}
	if !strings.Contains(err.Error(), "CATTERY_CHUNK_SIZE") {
		t.Fatalf("error = %q, want CATTERY_CHUNK_SIZE", err)
	}
}
