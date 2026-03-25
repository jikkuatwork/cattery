package stt

import (
	"io"
	"time"
)

// Engine transcribes audio to text.
type Engine interface {
	// Transcribe reads audio from r and returns the transcription.
	Transcribe(r io.Reader, opts Options) (*Result, error)

	// SampleRate returns the expected input sample rate.
	SampleRate() int

	// Close releases engine resources.
	Close() error
}

// Options controls transcription parameters.
type Options struct {
	Lang      string        // language hint
	ChunkSize time.Duration // target streaming window size
}

// Result holds transcription output.
type Result struct {
	Text     string  // transcribed text
	Duration float64 // audio duration in seconds
	Elapsed  float64 // processing time in seconds
	RTF      float64 // elapsed / duration
}
