package main

import (
	"testing"

	"github.com/jikkuatwork/cattery/registry"
)

func TestParseSelectionArgsSupportsVerbAliases(t *testing.T) {
	kind, modelRef, err := parseSelectionArgs([]string{"listen", "--model", "1"})
	if err != nil {
		t.Fatalf("parseSelectionArgs error: %v", err)
	}
	if kind != registry.KindSTT {
		t.Fatalf("kind = %q, want %q", kind, registry.KindSTT)
	}
	if modelRef != "1" {
		t.Fatalf("modelRef = %q, want %q", modelRef, "1")
	}
}

func TestResolveModelRejectsAmbiguousIndex(t *testing.T) {
	model, err := resolveModel("", "1", true)
	if err == nil {
		t.Fatalf("resolveModel error = nil, model = %#v", model)
	}
}

func TestLooksLikeCommandIncludesNewVerbs(t *testing.T) {
	if !looksLikeCommand("speak") {
		t.Fatal("expected speak to look like a command")
	}
	if !looksLikeCommand("listen") {
		t.Fatal("expected listen to look like a command")
	}
}
