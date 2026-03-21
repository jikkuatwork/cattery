// cattery — text-to-speech from the command line.
//
// Usage:
//
//	cattery "Hello, world."
//	cattery --voice bella --speed 1.2 -o greeting.wav "Hello!"
//	cattery voices
//	cattery models
//	cattery status
//	cattery download
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

	// Subcommands (info/management only)
	switch args[0] {
	case "list":
		return cmdList()
	case "status":
		return cmdStatus(args[1:])
	case "download":
		return cmdDownload(args[1:])
	case "help", "--help", "-h":
		printUsage()
		return nil
	case "version", "--version":
		fmt.Println("cattery 0.1.0")
		return nil
	}

	// Check for likely typos of subcommands before treating as text
	if len(args) == 1 && !strings.HasPrefix(args[0], "--") && looksLikeCommand(args[0]) {
		return fmt.Errorf("unknown command %q\nDid you mean one of: voices, models, status, download, help?", args[0])
	}

	// Primary action: synthesize text
	return cmdSpeak(args)
}

// --- speak (primary action) ---

func cmdSpeak(args []string) error {
	var voiceFlag string
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
				voiceFlag = args[i]
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
		return fmt.Errorf("no text provided\nUsage: cattery \"Hello, world.\"")
	}

	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q\nRun 'cattery models' to see available models", modelID)
	}

	if voiceFlag == "" {
		voiceFlag = model.DefaultVoice
	}
	voiceInfo := model.GetVoice(voiceFlag)
	if voiceInfo == nil {
		return fmt.Errorf("unknown voice %q for %s\nRun 'cattery voices --model %s' to see available voices",
			voiceFlag, model.Name, model.ID)
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

// --- list ---

func cmdList() error {
	dataDir := paths.DataDir()

	for _, model := range registry.Models {
		modelPath := paths.ModelFile(dataDir, model.ID, model.Filename)
		marker := "✗"
		if _, err := os.Stat(modelPath); err == nil {
			marker = "✓"
		}
		def := ""
		if model.ID == registry.Default {
			def = " (default)"
		}
		fmt.Printf("%s %s  %s  %s%s\n", marker, model.Name, model.ID, formatSize(model.SizeBytes), def)

		for _, v := range model.Voices {
			vMarker := " "
			vPath := paths.VoiceFile(dataDir, model.ID, v.ID)
			if _, err := os.Stat(vPath); err == nil {
				vMarker = "✓"
			}
			gender := "♀"
			if v.Gender == "male" {
				gender = "♂"
			}
			vDef := ""
			if v.ID == model.DefaultVoice {
				vDef = " *"
			}
			fmt.Printf("  %s %s %-12s %s  %s%s\n", vMarker, gender, v.Name, v.ID, v.Description, vDef)
		}
		fmt.Println()
	}

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

	fmt.Printf("  Platform:      %s/%s\n", goruntime.GOOS, goruntime.GOARCH)
	fmt.Printf("  Data dir:      %s\n", dataDir)
	fmt.Println()

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

	totalSize := dirSize(dataDir)
	fmt.Printf("  Disk usage:    %s\n", formatSize(totalSize))

	return nil
}

// --- download ---

func cmdDownload(args []string) error {
	modelID := registry.Default
	allVoices := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			i++
			if i < len(args) {
				modelID = args[i]
			}
		case "--all-voices":
			allVoices = true
		default:
			if !strings.HasPrefix(args[i], "--") {
				modelID = args[i]
			}
		}
	}

	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q\nRun 'cattery models' to see available models", modelID)
	}

	dataDir := paths.DataDir()

	defaultVoice := model.GetVoice(model.DefaultVoice)
	if defaultVoice == nil && len(model.Voices) > 0 {
		defaultVoice = &model.Voices[0]
	}

	_, err := download.Ensure(dataDir, model, defaultVoice)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  ✓ %s model ready\n", model.Name)
	fmt.Fprintf(os.Stderr, "  ✓ Voice: %s\n", defaultVoice.Name)

	if allVoices {
		for i := range model.Voices {
			v := &model.Voices[i]
			if v.ID == defaultVoice.ID {
				continue
			}
			_, err := download.Ensure(dataDir, model, v)
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
  cattery "Hello, world."                      Speak text to output.wav
  cattery --voice bella "Hi there"             Use a specific voice
  cattery --speed 1.5 -o out.wav "Fast talk"   Custom speed and output

Commands:
  list         List available models and voices
  status       Show system status and downloaded files
  download     Pre-download model and voices
  help         Show this help

Flags:
  --voice NAME     Voice name or ID (default: model-specific)
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --output, -o     Output WAV file (default: output.wav)
  --lang LANG      Phonemizer language (default: en-us)
  --model ID       Model to use (default: kokoro-82m-v1.0)

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.
`)
}

// looksLikeCommand returns true if a single-word argument looks like
// it was meant to be a subcommand rather than text to speak.
// A single lowercase word with no spaces is suspicious.
func looksLikeCommand(s string) bool {
	if strings.Contains(s, " ") {
		return false
	}
	// Known commands for fuzzy matching
	commands := []string{"list", "status", "download", "help", "version"}
	lower := strings.ToLower(s)
	for _, cmd := range commands {
		if lower == cmd {
			return true
		}
		// Simple edit distance: if >60% of chars match, probably a typo
		if len(lower) >= 3 && len(lower) <= len(cmd)+2 {
			matches := 0
			for i := 0; i < len(lower) && i < len(cmd); i++ {
				if lower[i] == cmd[i] {
					matches++
				}
			}
			if float64(matches)/float64(len(cmd)) > 0.5 {
				return true
			}
		}
	}
	return false
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
