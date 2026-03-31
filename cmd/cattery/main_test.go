package main

import (
	"io"
	"os"
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
	if !looksLikeCommand("llm") {
		t.Fatal("expected llm to look like a command")
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

func TestDefaultUsageMentionsLLMWithoutAdvancedFlags(t *testing.T) {
	output := captureStderr(t, printDefaultUsage)
	if !strings.Contains(output, "cattery llm \"What is the capital of France?\"") {
		t.Fatalf("default help missing llm usage: %q", output)
	}
	if !strings.Contains(output, "llm         Local text generation") {
		t.Fatalf("default help missing llm command: %q", output)
	}
	if strings.Contains(output, "--model REF      LLM model index or ID") {
		t.Fatalf("default help leaked advanced llm flags: %q", output)
	}
}

func TestAdvancedUsageMentionsLLMFlags(t *testing.T) {
	output := captureStderr(t, printAdvancedUsage)
	for _, want := range []string{
		"llm         Local text generation",
		"--system TEXT    System prompt",
		"--stdin          Read prompt from stdin",
		"--output, -o     Output text file (default: stdout)",
		"--model REF      LLM model index or ID (default: 1)",
		"--max-tokens N   Maximum output tokens",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("advanced help missing %q in %q", want, output)
		}
	}
}

func TestAdvancedUsageServeFlagsAreUpdated(t *testing.T) {
	output := captureStderr(t, printAdvancedUsage)
	if strings.Contains(output, "--tts-model") {
		t.Fatalf("advanced help should not mention --tts-model: %q", output)
	}
	if strings.Contains(output, "--stt-model") {
		t.Fatalf("advanced help should not mention --stt-model: %q", output)
	}
	if !strings.Contains(output, "--memory SIZE    Memory budget for engine co-residency (e.g. 8G)") {
		t.Fatalf("advanced help missing --memory flag: %q", output)
	}
}

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "512", want: 512 * (1 << 20)},
		{input: "512M", want: 512 * (1 << 20)},
		{input: "4G", want: 4 * (1 << 30)},
		{input: "8g", want: 8 * (1 << 30)},
	}

	for _, tt := range tests {
		got, err := parseMemorySize(tt.input)
		if err != nil {
			t.Fatalf("parseMemorySize(%q) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseMemorySize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseMemorySizeRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "0", "-1G", "4T", "abc"} {
		if _, err := parseMemorySize(input); err == nil {
			t.Fatalf("parseMemorySize(%q) error = nil", input)
		}
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	os.Stderr = w
	defer func() {
		os.Stderr = orig
	}()

	fn()
	_ = w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	return string(data)
}
