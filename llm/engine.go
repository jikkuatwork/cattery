package llm

import "context"

// Engine generates text from a prompt.
type Engine interface {
	Generate(ctx context.Context, prompt string, opts Options) (*Result, error)
	Close() error
}

// Options controls generation.
type Options struct {
	System    string
	MaxTokens int
	Stop      []string
}

// Result holds generation output.
type Result struct {
	Text         string
	TokensUsed   int
	FinishReason string
}
