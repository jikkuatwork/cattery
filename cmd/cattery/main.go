// cattery — text-to-speech from the command line.
//
// Usage:
//
//	cattery "Hello, world."
//	cattery -voice af_bella -speed 1.2 -o greeting.wav "Hello!"
//	cattery voices
//	cattery status
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

	// Subcommands
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return cmdStatus()
		case "voices":
			return cmdVoices()
		case "help", "-h", "--help":
			printUsage()
			return nil
		}
	}

	// Parse flags manually to allow subcommands + positional text
	voice := "af_heart"
	speed := 1.0
	output := "output.wav"
	lang := "en-us"
	modelID := registry.Default
	var textParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-voice", "--voice":
			i++
			if i < len(args) {
				voice = args[i]
			}
		case "-speed", "--speed":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%f", &speed)
			}
		case "-o", "--output":
			i++
			if i < len(args) {
				output = args[i]
			}
		case "-lang", "--lang":
			i++
			if i < len(args) {
				lang = args[i]
			}
		case "-model", "--model":
			i++
			if i < len(args) {
				modelID = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s\nRun 'cattery help' for usage", args[i])
			}
			textParts = append(textParts, args[i])
		}
	}

	text := strings.Join(textParts, " ")
	if text == "" {
		printUsage()
		return nil
	}

	// Resolve model and voice from registry
	model := registry.Get(modelID)
	if model == nil {
		return fmt.Errorf("unknown model %q. Available: %s", modelID, availableModels())
	}

	voiceInfo := model.GetVoice(voice)
	if voiceInfo == nil {
		return fmt.Errorf("unknown voice %q for model %s. Run 'cattery voices' to see available voices", voice, model.Name)
	}

	// Pre-flight: check espeak-ng
	if !phonemize.Available() {
		return fmt.Errorf("espeak-ng not found\n\nInstall it:\n  Linux:  sudo apt install espeak-ng\n  macOS:  brew install espeak-ng")
	}

	// Ensure all files are downloaded
	dataDir := paths.DataDir()
	files, err := download.Ensure(dataDir, model, voiceInfo)
	if err != nil {
		return err
	}

	// Initialize ORT
	if err := engine.Init(files.ORTLib); err != nil {
		return fmt.Errorf("init ORT: %w", err)
	}
	defer engine.Shutdown()

	// Load model
	eng, err := engine.New(files.ModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	defer eng.Close()

	// Phonemize
	p := &phonemize.EspeakPhonemizer{Voice: lang}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		return fmt.Errorf("phonemize: %w", err)
	}

	// Tokenize
	tokens := engine.Tokenize(phonemes)
	if len(tokens) == 0 {
		return fmt.Errorf("no tokens produced — text may not contain speakable content")
	}

	// Load voice
	style, err := engine.LoadVoice(files.VoicePath, len(tokens))
	if err != nil {
		return fmt.Errorf("load voice: %w", err)
	}

	// Synthesize
	t0 := time.Now()
	samples, err := eng.Synthesize(tokens, style, float32(speed))
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}
	elapsed := time.Since(t0)

	// Write WAV
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
	fmt.Fprintf(os.Stderr, "  %s → %s (%.1fs, %.2fx realtime)\n", voiceInfo.Name, output, duration, rtf)

	return nil
}

func cmdStatus() error {
	dataDir := paths.DataDir()
	model := registry.Get(registry.Default)

	fmt.Println("cattery status")
	fmt.Println()

	// Platform
	fmt.Printf("  Platform:     %s/%s\n", goruntime.GOOS, goruntime.GOARCH)
	fmt.Printf("  Data dir:     %s\n", dataDir)
	fmt.Println()

	// espeak-ng
	if phonemize.Available() {
		fmt.Println("  espeak-ng:    ✓ installed")
	} else {
		fmt.Println("  espeak-ng:    ✗ not found (required)")
	}
	fmt.Println()

	// ORT
	ortLib := findORTStatus(dataDir)
	if ortLib != "" {
		fmt.Printf("  ONNX Runtime: ✓ %s\n", ortLib)
	} else {
		fmt.Println("  ONNX Runtime: ✗ not downloaded")
	}
	fmt.Println()

	// Model
	fmt.Printf("  Model: %s\n", model.Name)
	modelPath := paths.ModelFile(dataDir, model.ID, model.Filename)
	if info, err := os.Stat(modelPath); err == nil {
		fmt.Printf("    Status:     ✓ downloaded (%s)\n", formatSize(info.Size()))
	} else {
		fmt.Printf("    Status:     ✗ not downloaded (%s needed)\n", formatSize(model.SizeBytes))
	}
	fmt.Println()

	// Voices
	fmt.Println("  Voices:")
	voiceDir := paths.ModelFile(dataDir, model.ID, "voices")
	downloaded := 0
	for _, v := range model.Voices {
		vPath := paths.VoiceFile(dataDir, model.ID, v.ID)
		if _, err := os.Stat(vPath); err == nil {
			downloaded++
		}
	}
	entries, _ := os.ReadDir(voiceDir)
	fmt.Printf("    Downloaded: %d / %d available\n", downloaded, len(model.Voices))
	if len(entries) > 0 {
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".bin")
			if v := model.GetVoice(name); v != nil {
				fmt.Printf("      ✓ %s (%s, %s %s)\n", v.Name, v.ID, v.Accent, v.Gender)
			}
		}
	}
	fmt.Println()

	// Disk usage
	totalSize := dirSize(dataDir)
	fmt.Printf("  Disk usage:   %s\n", formatSize(totalSize))

	return nil
}

func cmdVoices() error {
	model := registry.Get(registry.Default)
	dataDir := paths.DataDir()

	fmt.Printf("Voices for %s:\n\n", model.Name)

	byAccent := model.VoicesByAccent()
	for _, accent := range []string{"American", "British"} {
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
			fmt.Printf("    %s%-10s %s %-8s  %s%s\n", marker, v.Name, gender, v.ID, v.Description, def)
		}
		fmt.Println()
	}

	return nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `cattery — text-to-speech from the command line

Usage:
  cattery "Hello, world."              Speak text to output.wav
  cattery -voice af_bella "Hi there"   Use a specific voice
  cattery -speed 1.5 "Fast speech"     Adjust speed (0.5-2.0)
  cattery -o out.wav "Save here"       Custom output file

  cattery voices                       List available voices
  cattery status                       Show system status

Flags:
  -voice NAME    Voice to use (default: af_heart)
  -speed FLOAT   Speech speed (default: 1.0)
  -o FILE        Output WAV file (default: output.wav)
  -lang LANG     Phonemizer language (default: en-us)
  -model ID      Model to use (default: kokoro-82m-v1.0)

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.
`)
}

func availableModels() string {
	var names []string
	for _, m := range registry.Models {
		names = append(names, m.ID)
	}
	return strings.Join(names, ", ")
}

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

