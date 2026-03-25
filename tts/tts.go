package tts

import (
	"io"
	"time"
)

// Engine synthesizes speech from text.
type Engine interface {
	// Speak synthesizes text and writes WAV audio to w.
	Speak(w io.Writer, text string, opts Options) error

	// Voices returns the available voices for this engine.
	Voices() []Voice

	// Close releases engine resources.
	Close() error
}

// Options controls synthesis parameters.
type Options struct {
	Voice     string        // voice name/ID (empty = engine default)
	Gender    string        // "male"/"female" filter (empty = any)
	Lang      string        // language (empty = engine default, usually "en-us")
	Speed     float64       // 0.5-2.0 (0 = default 1.0)
	ChunkSize time.Duration // streaming chunk target where supported; Kokoro ignores it for now
}

// Voice describes an available voice.
type Voice struct {
	ID          string
	Name        string
	Gender      string
	Accent      string
	Description string
}
