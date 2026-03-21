// cattery — text-to-speech from the command line.
//
// Usage:
//
//	cattery "Hello, world."
//	cattery -voice af_bella -speed 1.2 -o greeting.wav "Hello!"
//	cattery -voices
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/kodeman/cattery/audio"
	"github.com/kodeman/cattery/download"
	"github.com/kodeman/cattery/engine"
	"github.com/kodeman/cattery/phonemize"
)

func main() {
	log.SetFlags(0)

	voice := flag.String("voice", "af_heart", "voice name (e.g. af_heart, af_bella, am_adam)")
	speed := flag.Float64("speed", 1.0, "speech speed (0.5-2.0)")
	output := flag.String("o", "output.wav", "output WAV file")
	dataDir := flag.String("data", defaultDataDir(), "data directory for models and runtime")
	lang := flag.String("lang", "en-us", "espeak-ng voice/language for phonemization")
	listVoices := flag.Bool("voices", false, "list available voices")
	flag.Parse()

	if *listVoices {
		printVoices(*dataDir)
		return
	}

	text := strings.Join(flag.Args(), " ")
	if text == "" {
		fmt.Fprintln(os.Stderr, "Usage: cattery [flags] \"text to speak\"")
		fmt.Fprintln(os.Stderr, "       cattery -voices")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Ensure all files are downloaded
	files, err := download.Ensure(*dataDir, *voice, func(msg string) {
		fmt.Fprintln(os.Stderr, msg)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Check phonemizer
	if !phonemize.Available() {
		log.Fatal("espeak-ng not found. Install it:\n  apt install espeak-ng  (Linux)\n  brew install espeak-ng (macOS)")
	}

	// Initialize ORT
	if err := engine.Init(files.OrtLib); err != nil {
		log.Fatal("init: ", err)
	}
	defer engine.Shutdown()

	// Load model
	eng, err := engine.New(files.Model)
	if err != nil {
		log.Fatal("load model: ", err)
	}
	defer eng.Close()

	// Phonemize
	p := &phonemize.EspeakPhonemizer{Voice: *lang}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		log.Fatal("phonemize: ", err)
	}

	// Tokenize
	tokens := engine.Tokenize(phonemes)
	if len(tokens) == 0 {
		log.Fatal("no tokens produced from input text")
	}

	// Load voice
	style, err := engine.LoadVoice(files.Voice, len(tokens))
	if err != nil {
		log.Fatal("load voice: ", err)
	}

	// Synthesize
	samples, err := eng.Synthesize(tokens, style, float32(*speed))
	if err != nil {
		log.Fatal("synthesize: ", err)
	}

	// Write WAV
	f, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if err := audio.WriteWAV(f, samples, engine.SampleRate); err != nil {
		log.Fatal("write wav: ", err)
	}

	duration := float64(len(samples)) / float64(engine.SampleRate)
	fmt.Fprintf(os.Stderr, "Written %.1fs of audio to %s\n", duration, *output)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cattery"
	}
	return filepath.Join(home, ".cattery")
}

func printVoices(dataDir string) {
	voiceDir := filepath.Join(dataDir, "voices")
	entries, err := os.ReadDir(voiceDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "No voices downloaded yet. Run cattery once to download the default voice.")
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".bin") {
			fmt.Println(strings.TrimSuffix(name, ".bin"))
		}
	}
}
