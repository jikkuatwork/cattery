package main

import (
	"strings"
	"testing"

	"github.com/jikkuatwork/cattery/registry"
)

func TestParseSelectionArgs(t *testing.T) {
	kind, modelRef, err := parseSelectionArgs([]string{"stt", "--model", "1"})
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
	if !looksLikeCommand("tts") {
		t.Fatal("expected tts to look like a command")
	}
	if !looksLikeCommand("stt") {
		t.Fatal("expected stt to look like a command")
	}
	if !looksLikeCommand("keys") {
		t.Fatal("expected keys to look like a command")
	}
}

func TestDisplayCommandNamesHideAdvancedAndAliases(t *testing.T) {
	got := strings.Join(displayCommandNames(), ",")
	if strings.Contains(got, "speak") || strings.Contains(got, "listen") {
		t.Fatalf("display commands leaked hidden aliases: %q", got)
	}
	if strings.Contains(got, "keys") || strings.Contains(got, "list") {
		t.Fatalf("display commands leaked advanced commands: %q", got)
	}
}

func TestWantsAdvancedHelpSupportsShortFlag(t *testing.T) {
	if !wantsAdvancedHelp([]string{"-a"}) {
		t.Fatal("expected -a to enable advanced help")
	}
	if !wantsAdvancedHelp([]string{"--advanced"}) {
		t.Fatal("expected --advanced to enable advanced help")
	}
}
