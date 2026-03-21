// cattery — text-to-speech from the command line.
//
// Usage:
//
//	cattery say "Hello, world."
//	cattery say --voice bella --speed 1.2 --output greeting.wav "Hello!"
//	cattery models
//	cattery voices
//	cattery voices --model kokoro-82m-v1.0
//	cattery status
//	cattery download kokoro-82m-v1.0
package main

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/kodeman/cattery/audio"
	"github.com/kodeman/cattery/download"
	"github.com/kodeman/cattery/engine"
	"github.com/kodeman/cattery/paths"
	"github.com/kodeman/cattery/phonemize"
	"github.com/kodeman/cattery/registry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "say":
		return cmdSay(args[1:])
	case "models":
		return cmdModels()
	case "voices":
		return cmdVoices(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "download":
		return cmdDownload(args[1:])
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		// Bare text without "say" — treat as implicit say
		return cmdSay(args)
	}
}

// --- say ---

func cmdSay(args []string) error {
	voice := registry.DefaultVoice
	speed := 1.0
	output := "output.wav"
	lang := "en-us"
	modelID := registry.Default
	var textParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--voice":
			i++
			if i < len(args) {
				voice = args[i]
			}
		case "--speed":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%f", &speed)
			}
		case "--output", "-o":
			i++
			if i < len(args) {
				output = args[i]
			}
		case "--lang":
			i++
			if i < len(args) {
				lang = args[i]
			}
		case "--model":
			i++
			if i < len(args) {
				modelID = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown flag: %s\nRun 'cattery help' for usage", args[i])
			}
			textParts = append(textParts, args[i])
		}
	}

	text := strings.Join(textParts, " ")
	if text == "" {
		return fmt.Errorf("no text provided\nUsage: cattery say \"Hello, world.\"")
	}

	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q\nRun 'cattery models' to see available models", modelID)
	}

	voiceInfo := model.GetVoice(voice)
	if voiceInfo == nil {
		return fmt.Errorf("unknown voice %q for %s\nRun 'cattery voices' to see available voices", voice, model.Name)
	}

	if !phonemize.Available() {
		return fmt.Errorf("espeak-ng not found\n\nInstall it:\n  Linux:  sudo apt install espeak-ng\n  macOS:  brew install espeak-ng")
	}

	dataDir := paths.DataDir()
	files, err := download.Ensure(dataDir, model, voiceInfo)
	if err != nil {
		return err
	}

	if err := engine.Init(files.ORTLib); err != nil {
		return fmt.Errorf("init ORT: %w", err)
	}
	defer engine.Shutdown()

	eng, err := engine.New(files.ModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	defer eng.Close()

	p := &phonemize.EspeakPhonemizer{Voice: lang}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		return fmt.Errorf("phonemize: %w", err)
	}

	tokens := engine.Tokenize(phonemes)
	if len(tokens) == 0 {
		return fmt.Errorf("no tokens produced — text may not contain speakable content")
	}

	style, err := engine.LoadVoice(files.VoicePath, len(tokens))
	if err != nil {
		return fmt.Errorf("load voice: %w", err)
	}

	t0 := time.Now()
	samples, err := eng.Synthesize(tokens, style, float32(speed))
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}
	elapsed := time.Since(t0)

	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := audio.WriteWAV(f, samples, model.SampleRate); err != nil {
		return fmt.Errorf("write wav: %w", err)
	}

	duration := float64(len(samples)) / float64(model.SampleRate)
	rtf := elapsed.Seconds() / duration
	fmt.Fprintf(os.Stderr, "  %s (%s) → %s (%.1fs, %.2fx realtime)\n",
		voiceInfo.Name, model.Name, output, duration, rtf)

	return nil
}

// --- models ---

func cmdModels() error {
	dataDir := paths.DataDir()

	fmt.Println("Available models:")
	fmt.Println()
	for _, model := range registry.Models {
		modelPath := paths.ModelFile(dataDir, model.ID, model.Filename)
		marker := "  "
		status := fmt.Sprintf("not downloaded (%s)", formatSize(model.SizeBytes))
		if info, err := os.Stat(modelPath); err == nil {
			marker = "✓ "
			status = fmt.Sprintf("downloaded (%s)", formatSize(info.Size()))
		}

		def := ""
		if model.ID == registry.Default {
			def = " (default)"
		}
		fmt.Printf("  %s%-20s %s%s\n", marker, model.Name, model.ID, def)
		fmt.Printf("    %s\n", model.Description)
		fmt.Printf("    %d voices, %dkHz, %s\n", len(model.Voices), model.SampleRate/1000, status)
		fmt.Println()
	}
	return nil
}

// --- voices ---

func cmdVoices(args []string) error {
	modelID := registry.Default
	for i := 0; i < len(args); i++ {
		if args[i] == "--model" && i+1 < len(args) {
			modelID = args[i+1]
			i++
		}
	}

	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q\nRun 'cattery models' to see available models", modelID)
	}

	dataDir := paths.DataDir()

	fmt.Printf("Voices for %s (%s):\n\n", model.Name, model.ID)

	byAccent := model.VoicesByAccent()
	// Stable accent ordering
	accents := []string{"American", "British", "European", "Asian", "Other"}
	for _, accent := range accents {
		voices, ok := byAccent[accent]
		if !ok {
			continue
		}
		fmt.Printf("  %s:\n", accent)
		for _, v := range voices {
			marker := "  "
			vPath := paths.VoiceFile(dataDir, model.ID, v.ID)
			if _, err := os.Stat(vPath); err == nil {
				marker = "✓ "
			}
			gender := "♀"
			if v.Gender == "male" {
				gender = "♂"
			}
			def := ""
			if v.ID == registry.DefaultVoice {
				def = " (default)"
			}
			fmt.Printf("    %s%-12s %s %-12s %s%s\n", marker, v.Name, gender, v.ID, v.Description, def)
		}
		fmt.Println()
	}

	fmt.Println("  Use --voice with name or ID: cattery say --voice bella \"Hello\"")
	return nil
}

// --- status ---

func cmdStatus(args []string) error {
	modelID := registry.Default
	for i := 0; i < len(args); i++ {
		if args[i] == "--model" && i+1 < len(args) {
			modelID = args[i+1]
			i++
		}
	}

	dataDir := paths.DataDir()
	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q", modelID)
	}

	fmt.Println("cattery status")
	fmt.Println()

	// Platform
	fmt.Printf("  Platform:      %s/%s\n", goruntime.GOOS, goruntime.GOARCH)
	fmt.Printf("  Data dir:      %s\n", dataDir)
	fmt.Println()

	// Dependencies
	if phonemize.Available() {
		fmt.Println("  espeak-ng:     ✓ installed")
	} else {
		fmt.Println("  espeak-ng:     ✗ not found (required)")
	}

	ortLib := findORTStatus(dataDir)
	if ortLib != "" {
		fmt.Printf("  ONNX Runtime:  ✓ %s\n", ortLib)
	} else {
		fmt.Println("  ONNX Runtime:  ✗ not downloaded")
	}
	fmt.Println()

	// Models
	fmt.Println("  Models:")
	for _, m := range registry.Models {
		modelPath := paths.ModelFile(dataDir, m.ID, m.Filename)
		marker := "✗"
		sizeStr := fmt.Sprintf("need %s", formatSize(m.SizeBytes))
		if info, err := os.Stat(modelPath); err == nil {
			marker = "✓"
			sizeStr = formatSize(info.Size())
		}
		def := ""
		if m.ID == registry.Default {
			def = " *"
		}
		fmt.Printf("    %s %-20s %s%s\n", marker, m.Name, sizeStr, def)

		// Count downloaded voices for this model
		dlVoices := 0
		for _, v := range m.Voices {
			vPath := paths.VoiceFile(dataDir, m.ID, v.ID)
			if _, err := os.Stat(vPath); err == nil {
				dlVoices++
			}
		}
		fmt.Printf("      Voices: %d / %d downloaded\n", dlVoices, len(m.Voices))
	}
	fmt.Println()

	// Disk usage
	totalSize := dirSize(dataDir)
	fmt.Printf("  Disk usage:    %s\n", formatSize(totalSize))

	return nil
}

// --- download ---

func cmdDownload(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("specify a model to download\nUsage: cattery download kokoro-82m-v1.0\nRun 'cattery models' to see available models")
	}

	modelID := args[0]
	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q\nRun 'cattery models' to see available models", modelID)
	}

	// Parse --voices flag to download all voices
	allVoices := false
	for _, a := range args[1:] {
		if a == "--all-voices" {
			allVoices = true
		}
	}

	dataDir := paths.DataDir()

	// Download model + default voice
	defaultVoice := model.GetVoice(registry.DefaultVoice)
	if defaultVoice == nil && len(model.Voices) > 0 {
		defaultVoice = &model.Voices[0]
	}

	_, err := download.Ensure(dataDir, model, defaultVoice)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  ✓ %s model downloaded\n", model.Name)
	fmt.Fprintf(os.Stderr, "  ✓ Voice: %s\n", defaultVoice.Name)

	if allVoices {
		for _, v := range model.Voices {
			if v.ID == defaultVoice.ID {
				continue
			}
			_, err := download.Ensure(dataDir, model, &v)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ Voice %s: %v\n", v.Name, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  ✓ Voice: %s\n", v.Name)
		}
	}

	return nil
}

// --- help ---

func printUsage() {
	fmt.Fprintf(os.Stderr, `cattery — text-to-speech from the command line

Usage:
  cattery say "Hello, world."                  Speak text
  cattery say --voice bella "Hi there"         Use a specific voice
  cattery say --speed 1.5 --output out.wav "." Custom speed and output

Commands:
  say          Synthesize text to audio (default if omitted)
  models       List available models
  voices       List available voices
  status       Show system status and downloaded files
  download     Pre-download a model and voices
  help         Show this help

Flags (for say):
  --voice NAME     Voice name or ID (default: heart)
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --output FILE    Output WAV file (default: output.wav)
  --lang LANG      Phonemizer language (default: en-us)
  --model ID       Model to use (default: kokoro-82m-v1.0)

Flags (for voices, status):
  --model ID       Show voices/status for a specific model

Flags (for download):
  --all-voices     Download all voices, not just the default

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.
`)
}

// --- helpers ---

func findORTStatus(dataDir string) string {
	ortDir := paths.ORTLib(dataDir)
	patterns := []string{"libonnxruntime.so*", "libonnxruntime*.dylib"}
	for _, p := range patterns {
		matches, _ := filepath.Glob(filepath.Join(ortDir, p))
		if len(matches) > 0 {
			return filepath.Base(matches[0])
		}
	}
	return ""
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
