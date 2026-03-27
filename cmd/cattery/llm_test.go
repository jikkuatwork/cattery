package main

import "testing"

func TestResolveLLMPromptFromArgs(t *testing.T) {
	got, err := resolveLLMPrompt([]string{"hello", "world"}, false)
	if err != nil {
		t.Fatalf("resolveLLMPrompt error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("resolveLLMPrompt = %q, want %q", got, "hello world")
	}
}

func TestResolveLLMPromptRejectsMissingPrompt(t *testing.T) {
	_, err := resolveLLMPrompt(nil, false)
	if err == nil {
		t.Fatal("resolveLLMPrompt error = nil")
	}
}

func TestResolveLLMPromptRejectsPromptAndStdin(t *testing.T) {
	_, err := resolveLLMPrompt([]string{"hello"}, true)
	if err == nil {
		t.Fatal("resolveLLMPrompt error = nil")
	}
}
