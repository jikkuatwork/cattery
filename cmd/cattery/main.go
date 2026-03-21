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

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/server"
	"github.com/jikkuatwork/cattery/speak"
	"github.com/jikkuatwork/cattery/speak/kokoro"
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
	case "serve":
		return cmdServe(args[1:])
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
		return fmt.Errorf("unknown command %q\nDid you mean one of: serve, list, status, download, help?", args[0])
	}

	// Primary action: synthesize text
	return cmdSpeak(args)
}

// --- serve ---

func cmdServe(args []string) error {
	cfg := server.Config{
		Port:    7100,
		Workers: 1,
	}
	var idleSec int

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.Port)
			}
		case "--workers", "-w":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.Workers)
			}
		case "--max-chars":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.MaxChars)
			}
		case "--queue-max":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.QueueMax)
			}
		case "--idle-timeout":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &idleSec)
				cfg.IdleTimeout = time.Duration(idleSec) * time.Second
			}
		case "--keep-alive":
			cfg.KeepAlive = true
		case "--model":
			i++
			if i < len(args) {
				cfg.ModelID = args[i]
			}
		default:
			return fmt.Errorf("unknown flag %q for serve\nUsage: cattery serve [--port 7100] [--workers 1]", args[i])
		}
	}

	srv, err := server.New(cfg)
	if err != nil {
		return err
	}
	defer srv.Shutdown()

	return srv.ListenAndServe()
}

// --- speak (primary action) ---

func cmdSpeak(args []string) error {
	var voiceFlag string
	var genderFilter string
	speed := 1.0
	output := "output.wav"
	lang := "en-us"
	var modelRef string
	var textParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--voice":
			i++
			if i < len(args) {
				voiceFlag = args[i]
			}
		case "--male":
			genderFilter = "male"
		case "--female":
			genderFilter = "female"
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
				modelRef = args[i]
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

	model := resolveTTSModel(modelRef)
	if model == nil {
		return fmt.Errorf("unknown TTS model %q\nRun 'cattery list' to see available models", modelRef)
	}

	voiceInfo, err := kokoro.ResolveVoice(model, voiceFlag, genderFilter)
	if err != nil {
		if voiceFlag != "" {
			return fmt.Errorf("%w\nRun 'cattery list' to see available voices", err)
		}
		return err
	}

	if !phonemize.Available() {
		return fmt.Errorf("espeak-ng not found\n\nInstall it:\n  Linux:  sudo apt install espeak-ng\n  macOS:  brew install espeak-ng")
	}

	dataDir := paths.DataDir()
	files, err := download.Ensure(dataDir, model)
	if err != nil {
		return err
	}
	modelFile := model.PrimaryFile()
	if modelFile == nil {
		return fmt.Errorf("model %q has no downloadable files", model.ID)
	}
	modelPath := files.Files[modelFile.Filename]

	if err := ort.Init(files.ORTLib); err != nil {
		return fmt.Errorf("init ORT: %w", err)
	}
	defer ort.Shutdown()

	eng, err := kokoro.New(modelPath, dataDir)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	defer eng.Close()

	t0 := time.Now()
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := eng.Speak(f, text, speak.Options{
		Voice: voiceInfo.ID,
		Lang:  lang,
		Speed: speed,
	}); err != nil {
		return err
	}
	elapsed := time.Since(t0)

	info, err := f.Stat()
	if err != nil {
		return err
	}

	duration := wavDurationFromSize(info.Size(), model.MetaInt("sample_rate", 24000))
	rtf := 0.0
	if duration > 0 {
		rtf = elapsed.Seconds() / duration
	}
	fmt.Fprintf(os.Stderr, "%s | Used %s in %s, took %.1fs (RTF: %.2f)\n",
		output, voiceInfo.Name, model.Name, duration, rtf)

	return nil
}

// --- list ---

func cmdList() error {
	dataDir := paths.DataDir()

	for _, kind := range displayKindOrder() {
		models := localModelsByKind(kind)
		if len(models) == 0 {
			continue
		}

		fmt.Printf("%s:\n", kindTitle(kind))
		if kind == registry.KindRuntime {
			ortLib := findORTStatus(dataDir)
			for _, model := range models {
				marker := "✗"
				detail := "not downloaded"
				if ortLib != "" {
					marker = "✓"
					detail = ortLib
				}
				fmt.Printf("  %s %02d %-18s %s\n", marker, model.Index, model.Name, detail)
			}
			fmt.Println()
			continue
		}

		for _, model := range models {
			status := inspectModel(dataDir, model)
			marker := "✗"
			if status.filesReady() {
				marker = "✓"
			}

			fmt.Printf("  %s %02d %-18s %s\n",
				marker, model.Index, model.Name, formatSize(status.totalFileBytes))
			fmt.Printf("     files: %d / %d downloaded\n",
				status.downloadedFiles, status.totalFiles)

			switch kind {
			case registry.KindTTS:
				for i := range model.Voices {
					voice := model.Voices[i]
					vMarker := " "
					if _, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, voice.File.Filename)); err == nil {
						vMarker = "✓"
					}
					gender := "♀"
					if voice.Gender == "male" {
						gender = "♂"
					}
					fmt.Printf("     %s %02d %s %-12s %s\n",
						vMarker, i+1, gender, voice.Name, voice.Description)
				}
			case registry.KindSTT:
				for _, file := range model.Files {
					fMarker := " "
					if _, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, file.Filename)); err == nil {
						fMarker = "✓"
					}
					fmt.Printf("     %s %-28s %s\n",
						fMarker, filepath.Base(file.Filename), formatSize(file.SizeBytes))
				}
			}
		}
		fmt.Println()
	}

	return nil
}

// --- status ---

func cmdStatus(args []string) error {
	kind, modelRef, err := parseKindAndModel(args)
	if err != nil {
		return err
	}

	dataDir := paths.DataDir()
	models, err := selectLocalModels(kind, modelRef)
	if err != nil {
		return err
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

	for _, groupKind := range displayKindOrder() {
		group := modelsByKind(models, groupKind)
		if len(group) == 0 {
			continue
		}

		fmt.Printf("  %s:\n", kindTitle(groupKind))
		if groupKind == registry.KindRuntime {
			for _, model := range group {
				marker := "✗"
				detail := "not downloaded"
				if ortLib != "" {
					marker = "✓"
					detail = ortLib
				}
				fmt.Printf("    %s %02d %-18s %s\n", marker, model.Index, model.Name, detail)
			}
			fmt.Println()
			continue
		}

		for _, model := range group {
			status := inspectModel(dataDir, model)
			marker := "✗"
			if status.filesReady() {
				marker = "✓"
			}

			fmt.Printf("    %s %02d %-18s %s / %s\n",
				marker,
				model.Index,
				model.Name,
				formatSize(status.downloadedFileBytes),
				formatSize(status.totalFileBytes),
			)
			fmt.Printf("      Files: %d / %d downloaded\n",
				status.downloadedFiles, status.totalFiles)
			if len(model.Voices) > 0 {
				fmt.Printf("      Voices: %d / %d downloaded\n",
					status.downloadedVoices, status.totalVoices)
			}
		}
		fmt.Println()
	}

	totalSize := dirSize(dataDir)
	fmt.Printf("  Disk usage:    %s\n", formatSize(totalSize))

	return nil
}

// --- download ---

func cmdDownload(args []string) error {
	kind, modelRef, err := parseKindAndModel(args)
	if err != nil {
		return err
	}

	models, err := selectLocalModels(kind, modelRef)
	if err != nil {
		return err
	}
	dataDir := paths.DataDir()
	for i, model := range reorderModels(models, downloadKindOrder()) {
		if i > 0 {
			fmt.Fprintln(os.Stderr)
		}
		voices := []*registry.Voice(nil)
		if model.Kind == registry.KindTTS {
			voices = model.VoiceRefs()
		}
		if _, err := download.Ensure(dataDir, model, voices...); err != nil {
			return err
		}
	}
	return nil
}

// --- help ---

func printUsage() {
	fmt.Fprintf(os.Stderr, `cattery — text-to-speech from the command line

Usage:
  cattery "Hello, world."                Speak text (random voice)
  cattery --voice 3 "Hi there"           Use voice by number
  cattery --voice bella "Hi there"       Use voice by name
  cattery --female "Hi there"            Random female voice
  cattery --speed 1.5 -o out.wav "Fast"  Custom speed and output

Commands:
  serve        Start REST API server
  list         List local TTS/STT/runtime registry entries
  status       Show system status and downloaded artefacts
  download     Pre-download local models, voices, and runtime
  help         Show this help

Server:
  cattery serve                        Start on :7100 (1 worker, lazy-loaded)
  cattery serve --port 8080 -w 2       Custom port and workers
  cattery serve --max-chars 300        Shared char budget (bounds RAM)
  cattery serve --idle-timeout 600     Evict engines after N seconds idle
  cattery serve --keep-alive           Pre-warm engines, never evict

Flags:
  --voice NAME     Voice name, ID, or number (default: random)
  --female           Pick a random female voice
  --male             Pick a random male voice
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --output, -o     Output WAV file (default: output.wav)
  --lang LANG      Phonemizer language (default: en-us)
  --model REF      TTS model ID or index (default: 1)

Management:
  cattery download                   Download all local models + runtime
  cattery download --kind stt        Download all STT artefacts
  cattery download --model 1         Download TTS model 1 + all voices
  cattery status --kind stt          Show STT status only

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
	commands := []string{"serve", "list", "status", "download", "help", "version"}
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

type localModelStatus struct {
	downloadedFiles     int
	totalFiles          int
	downloadedVoices    int
	totalVoices         int
	downloadedFileBytes int64
	totalFileBytes      int64
}

func (s localModelStatus) filesReady() bool {
	return s.totalFiles == 0 || s.downloadedFiles == s.totalFiles
}

func resolveTTSModel(ref string) *registry.Model {
	return registry.Resolve(registry.KindTTS, ref)
}

func parseKindAndModel(args []string) (registry.Kind, string, error) {
	var (
		kind     registry.Kind
		modelRef string
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--kind":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("missing value for --kind")
			}
			parsed, err := parseKind(args[i])
			if err != nil {
				return "", "", err
			}
			kind = parsed
		case "--model":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("missing value for --model")
			}
			modelRef = args[i]
		default:
			if strings.HasPrefix(args[i], "--") {
				return "", "", fmt.Errorf("unknown flag: %s\nRun 'cattery help' for usage", args[i])
			}
			modelRef = args[i]
		}
	}

	return kind, modelRef, nil
}

func parseKind(ref string) (registry.Kind, error) {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "tts":
		return registry.KindTTS, nil
	case "stt":
		return registry.KindSTT, nil
	case "runtime", "ort":
		return registry.KindRuntime, nil
	default:
		return "", fmt.Errorf("unknown kind %q (want: tts, stt, runtime)", ref)
	}
}

func selectLocalModels(kind registry.Kind, modelRef string) ([]*registry.Model, error) {
	if modelRef != "" {
		model := resolveLocalModel(kind, modelRef)
		if model == nil {
			if kind != "" {
				return nil, fmt.Errorf("unknown %s model %q", kindTitle(kind), modelRef)
			}
			return nil, fmt.Errorf("unknown model %q", modelRef)
		}
		return []*registry.Model{model}, nil
	}

	if kind != "" {
		models := localModelsByKind(kind)
		if len(models) == 0 {
			return nil, fmt.Errorf("no local %s models registered", kindTitle(kind))
		}
		return models, nil
	}

	return allLocalModels(displayKindOrder()), nil
}

func resolveLocalModel(kind registry.Kind, ref string) *registry.Model {
	if kind != "" {
		model := registry.Resolve(kind, ref)
		if model != nil && model.Location == registry.Local {
			return model
		}
		return nil
	}

	if model := registry.Get(ref); model != nil && model.Location == registry.Local {
		return model
	}
	for _, groupKind := range displayKindOrder() {
		model := registry.Resolve(groupKind, ref)
		if model != nil && model.Location == registry.Local {
			return model
		}
	}
	return nil
}

func allLocalModels(order []registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, kind := range order {
		out = append(out, localModelsByKind(kind)...)
	}
	return out
}

func localModelsByKind(kind registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, model := range registry.GetByKind(kind) {
		if model.Location == registry.Local {
			out = append(out, model)
		}
	}
	return out
}

func modelsByKind(models []*registry.Model, kind registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, model := range models {
		if model.Kind == kind {
			out = append(out, model)
		}
	}
	return out
}

func reorderModels(models []*registry.Model, order []registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, kind := range order {
		out = append(out, modelsByKind(models, kind)...)
	}
	return out
}

func displayKindOrder() []registry.Kind {
	return []registry.Kind{registry.KindTTS, registry.KindSTT, registry.KindRuntime}
}

func downloadKindOrder() []registry.Kind {
	return []registry.Kind{registry.KindRuntime, registry.KindTTS, registry.KindSTT}
}

func kindTitle(kind registry.Kind) string {
	switch kind {
	case registry.KindTTS:
		return "TTS"
	case registry.KindSTT:
		return "STT"
	case registry.KindRuntime:
		return "Runtime"
	default:
		return strings.ToUpper(string(kind))
	}
}

func inspectModel(dataDir string, model *registry.Model) localModelStatus {
	status := localModelStatus{
		totalFiles:  len(model.Files),
		totalVoices: len(model.Voices),
	}

	for _, file := range model.Files {
		status.totalFileBytes += file.SizeBytes
		if info, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, file.Filename)); err == nil {
			status.downloadedFiles++
			status.downloadedFileBytes += info.Size()
		}
	}

	for _, voice := range model.Voices {
		if _, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, voice.File.Filename)); err == nil {
			status.downloadedVoices++
		}
	}

	return status
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

func wavDurationFromSize(size int64, sampleRate int) float64 {
	if size <= 44 || sampleRate <= 0 {
		return 0
	}
	dataBytes := size - 44
	return float64(dataBytes/2) / float64(sampleRate)
}
