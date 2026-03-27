// cattery - speech tools from the command line.
//
// Usage:
//
//	cattery "Hello, world."
//	cattery tts --voice 4 "Hello!"
//	cattery stt audio.wav
//	cattery llm "What is the capital of France?"
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
	"github.com/jikkuatwork/cattery/llm/qwen"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/server"
	"github.com/jikkuatwork/cattery/tts"
	"github.com/jikkuatwork/cattery/tts/kokoro"
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
		printDefaultUsage()
		return nil
	}

	// Subcommands (info/management only)
	switch args[0] {
	case "tts":
		return cmdTTS(args[1:])
	case "stt":
		return cmdSTT(args[1:])
	case "llm":
		return cmdLLM(args[1:])
	case "keys":
		return cmdKeys(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "list":
		return cmdList(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "download":
		return cmdDownload(args[1:])
	case "help", "--help", "-h":
		if wantsAdvancedHelp(args[1:]) {
			printAdvancedUsage()
			return nil
		}
		printDefaultUsage()
		return nil
	case "version", "--version":
		fmt.Println("cattery 0.1.0")
		return nil
	}

	// Check for likely typos of subcommands before treating as text
	if len(args) == 1 && !strings.HasPrefix(args[0], "--") && looksLikeCommand(args[0]) {
		return fmt.Errorf(
			"unknown command %q\nDid you mean one of: %s?",
			args[0],
			strings.Join(displayCommandNames(), ", "),
		)
	}

	// Primary action: bare text is a shortcut for TTS.
	return cmdTTS(args)
}

// --- serve ---

func cmdServe(args []string) error {
	cfg := server.Config{
		Port:       7100,
		TTSWorkers: 1,
		STTWorkers: 1,
	}
	var idleSec int
	var chunkSizeFlag string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.Port)
			}
		case "--tts-workers", "-w":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.TTSWorkers)
			}
		case "--stt-workers":
			i++
			if i < len(args) {
				fmt.Sscanf(args[i], "%d", &cfg.STTWorkers)
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
		case "--auth":
			cfg.Auth = true
		case "--chunk-size":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --chunk-size")
			}
			chunkSizeFlag = args[i]
		case "--model", "--tts-model":
			i++
			if i < len(args) {
				index, err := resolveServeModelIndex(registry.KindTTS, args[i])
				if err != nil {
					return err
				}
				cfg.TTSModel = index
			}
		case "--stt-model":
			i++
			if i < len(args) {
				index, err := resolveServeModelIndex(registry.KindSTT, args[i])
				if err != nil {
					return err
				}
				cfg.STTModel = index
			}
		default:
			return fmt.Errorf(
				"unknown flag %q for serve\nUsage: cattery serve [--port 7100] [--tts-workers 1] [--stt-workers 1] [--chunk-size 30s] [--auth]",
				args[i],
			)
		}
	}

	chunkSize, err := resolveCommandChunkSize(chunkSizeFlag, os.Stderr)
	if err != nil {
		return err
	}
	cfg.ChunkSize = chunkSize

	srv, err := server.New(cfg)
	if err != nil {
		return err
	}
	defer srv.Shutdown()

	return srv.ListenAndServe()
}

// --- tts (primary action) ---

func cmdTTS(args []string) error {
	var voiceFlag string
	var genderFilter string
	speed := 1.0
	var outputPath string
	lang := "en-us"
	var modelRef string
	var chunkSizeFlag string
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
				outputPath = args[i]
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
		case "--chunk-size":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --chunk-size")
			}
			chunkSizeFlag = args[i]
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown flag: %s\nRun 'cattery help' for usage", args[i])
			}
			textParts = append(textParts, args[i])
		}
	}

	text, err := resolveTTSText(textParts)
	if err != nil {
		return err
	}

	model := resolveTTSModel(modelRef)
	if model == nil {
		return fmt.Errorf("unknown TTS model %q\nRun 'cattery list tts' to see available models", modelRef)
	}
	if model.Location != registry.Local {
		return fmt.Errorf("remote TTS model %q is not supported yet", model.ID)
	}

	voiceInfo, err := kokoro.ResolveVoice(model, voiceFlag, genderFilter)
	if err != nil {
		if voiceFlag != "" {
			return fmt.Errorf("%w\nRun 'cattery list tts' to see available voices", err)
		}
		return err
	}

	if !phonemize.Available() {
		return fmt.Errorf("espeak-ng not found\n\nInstall it:\n  Linux:  sudo apt install espeak-ng\n  macOS:  brew install espeak-ng")
	}

	chunkSize, err := resolveCommandChunkSize(chunkSizeFlag, os.Stderr)
	if err != nil {
		return err
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

	output, err := openTTSOutput(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()

	counted := &countingWriter{writer: output.writer}

	t0 := time.Now()
	if err := preflight.GuardMemoryError("speech synthesis", func() error {
		return eng.Speak(counted, text, tts.Options{
			Voice:     voiceInfo.ID,
			Lang:      lang,
			Speed:     speed,
			ChunkSize: chunkSize,
		})
	}); err != nil {
		return err
	}
	elapsed := time.Since(t0)

	duration := wavDurationFromSize(counted.count, model.MetaInt("sample_rate", 24000))
	rtf := 0.0
	if duration > 0 {
		rtf = elapsed.Seconds() / duration
	}
	fmt.Fprintf(os.Stderr, "%s | %s, %.1fs (RTF: %.2f)\n",
		output.name, voiceInfo.Name, duration, rtf)

	return nil
}

// --- list ---

func cmdList(args []string) error {
	kind, modelRef, err := parseSelectionArgs(args)
	if err != nil {
		return err
	}

	dataDir := paths.DataDir()
	models, err := selectModels(kind, modelRef, false)
	if err != nil {
		return err
	}

	for _, groupKind := range orderedKindsFor(models, cliKindOrder()) {
		group := modelsByKind(models, groupKind)
		if len(group) == 0 {
			continue
		}

		fmt.Println(kindTitle(groupKind))
		if groupKind == registry.KindRuntime {
			ortLib := findORTStatus(dataDir)
			for _, model := range group {
				marker := " "
				location := "local"
				size := ""
				if model.Location == registry.Remote {
					marker = "☁"
					location = "remote"
				} else if ortLib != "" {
					marker = "✓"
					size = ortLib
				}
				fmt.Printf("  %02d  %-18s %12s  %-6s %s\n",
					model.Index, model.Name, size, location, marker)
			}
			fmt.Println()
			continue
		}

		for _, model := range group {
			if model.Location == registry.Remote {
				fmt.Printf("  %02d  %-18s %12s  %-6s %s\n",
					model.Index, model.Name, "", "remote", "☁")
				if groupKind == registry.KindTTS && len(model.Voices) > 0 {
					fmt.Println()
					fmt.Printf("  Voices (%02d %s)\n", model.Index, model.Name)
					for i := range model.Voices {
						voice := model.Voices[i]
						fmt.Printf("  %02d  %s  %-12s %s\n",
							i+1, voiceSymbol(model, voice), voice.Name, strings.TrimSpace(voice.Description))
					}
					fmt.Println()
				}
				continue
			}

			status := inspectModel(dataDir, model)
			marker := " "
			if modelReady(model, status) {
				marker = "✓"
			}

			fmt.Printf("  %02d  %-18s %12s  %-6s %s\n",
				model.Index, model.Name, formatSize(status.totalFileBytes), "local", marker)

			if groupKind == registry.KindTTS {
				if len(model.Voices) > 0 {
					fmt.Println()
					fmt.Printf("  Voices (%02d %s)\n", model.Index, model.Name)
					for i := range model.Voices {
						voice := model.Voices[i]
						fmt.Printf("  %02d  %s  %-12s %-24s %s\n",
							i+1,
							voiceSymbol(model, voice),
							voice.Name,
							strings.TrimSpace(voice.Description),
							voiceReadyMarker(dataDir, model, voice),
						)
					}
					fmt.Println()
				}
			}
		}
	}

	return nil
}

// --- status ---

func cmdStatus(args []string) error {
	kind, modelRef, err := parseSelectionArgs(args)
	if err != nil {
		return err
	}

	dataDir := paths.DataDir()
	models, err := selectModels(kind, modelRef, false)
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

	for _, groupKind := range orderedKindsFor(models, cliKindOrder()) {
		group := modelsByKind(models, groupKind)
		if len(group) == 0 {
			continue
		}

		fmt.Printf("  %s\n", kindTitle(groupKind))
		if groupKind == registry.KindRuntime {
			for _, model := range group {
				marker := " "
				detail := "not downloaded"
				if model.Location == registry.Remote {
					marker = "☁"
					detail = "remote"
				} else if ortLib != "" {
					marker = "✓"
					detail = ortLib
				}
				fmt.Printf("    %02d  %s  %-18s %s\n", model.Index, marker, model.Name, detail)
			}
			fmt.Println()
			continue
		}

		for _, model := range group {
			if model.Location == registry.Remote {
				fmt.Printf("    %02d  ☁  %-18s remote\n", model.Index, model.Name)
				continue
			}

			status := inspectModel(dataDir, model)
			marker := " "
			if modelReady(model, status) {
				marker = "✓"
			}

			fmt.Printf("    %02d  %s  %-18s %s",
				model.Index,
				marker,
				model.Name,
				formatSize(status.totalFileBytes),
			)
			if len(model.Voices) > 0 {
				fmt.Printf("   %d/%d voices", status.downloadedVoices, status.totalVoices)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	fmt.Printf("  Disk:          %s\n", formatSize(dirSize(dataDir)))

	return nil
}

// --- download ---

func cmdDownload(args []string) error {
	kind, modelRef, err := parseSelectionArgs(args)
	if err != nil {
		return err
	}

	models, err := selectModels(kind, modelRef, true)
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

func printDefaultUsage() {
	fmt.Fprintf(os.Stderr, `cattery - speech tools from the command line

Usage:
  cattery "Hello, world."          Shortcut for cattery tts
  cattery tts "Hello, world."      Text to speech
  cattery stt audio.wav            Speech to text
  cattery llm "Hello"              Local text generation
  cattery serve                    Start REST API server
  cattery status                   Show model/runtime status
  cattery download                 Pre-download local assets
  cattery help --advanced          Show advanced commands and flags

Commands:
  tts         Text to speech
  stt         Speech to text
  llm         Local text generation
  serve       Start REST API server
  status      Show system status and downloaded artefacts
  download    Pre-download local models and runtime
  help        Show this help

TTS:
  --voice REF      Voice number, name, or ID
  --female         Pick a random female voice
  --male           Pick a random male voice
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --output, -o     Output WAV file (default: output.wav or stdout if piped)

STT:
  --output, -o     Output text file (default: stdout)

LLM:
  cattery llm "What is the capital of France?"
  cattery llm --system "You are helpful" "Hi"
  cattery llm --stdin < prompt.txt

Server:
  cattery serve --port 8080
  cattery serve --auth

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.

Run 'cattery help --advanced' for more.
`)
}

func printAdvancedUsage() {
	fmt.Fprintf(os.Stderr, `cattery - speech tools from the command line

Usage:
  cattery "Hello, world."          Shortcut for cattery tts
  cattery tts --voice 4 "Hi there"
  cattery tts --model 1 "Hi there"
  cattery stt call.wav
  cattery llm "What is the capital of France?"
  cattery llm --stdin < prompt.txt
  cattery stt -

Commands:
  tts         Text to speech
  stt         Speech to text
  llm         Local text generation
  serve       Start REST API server
  status      Show system status and downloaded artefacts
  download    Pre-download local models and runtime
  list        List TTS/STT/LLM models and voices
  keys        Manage API keys for --auth server mode
  help        Show this help

TTS flags:
  --voice REF      Voice number, name, or ID
  --female         Pick a random female voice
  --male           Pick a random male voice
  --speed FLOAT    Speech speed, 0.5-2.0 (default: 1.0)
  --chunk-size DUR Chunk size override, 10s-60s (bare ints = seconds)
  --output, -o     Output WAV file (default: output.wav or stdout if piped)
  --lang LANG      Phonemizer language (default: en-us)
  --model REF      TTS model index or ID (default: 1)

STT flags:
  --chunk-size DUR Chunk size override, 10s-60s (bare ints = seconds)
  --output, -o     Output text file (default: stdout)
  --lang LANG      Language hint
  --model REF      STT model index or ID (default: 1)

LLM flags:
  --system TEXT    System prompt
  --stdin          Read prompt from stdin
  --model REF      LLM model index or ID (default: 1)
  --max-tokens N   Maximum output tokens

Manage:
  cattery list
  cattery list tts
  cattery list llm
  cattery download stt
  cattery download llm
  cattery status tts --model 1

Pipes:
  cattery stt call.wav | cattery tts
  echo "Hello" | cattery tts | cattery stt

Server:
  cattery serve --port 8080
  cattery serve --tts-workers 2
  cattery serve --stt-workers 2
  cattery serve --chunk-size 20s
  cattery serve --tts-model 1
  cattery serve --stt-model 1
  cattery serve --max-chars 300
  cattery serve --queue-max 10
  cattery serve --idle-timeout 600
  cattery serve --keep-alive
  cattery serve --auth

Keys:
  cattery keys create --name my-app
  cattery keys list
  cattery keys revoke cat_a1b2c3d4
  cattery keys delete cat_a1b2c3d4

Chunk size:
  CATTERY_CHUNK_SIZE   Shared override for tts, stt, and serve
  Auto default         10s <=512MB, 15s <=1GB, 20s <=2GB, 30s <=4GB,
                       45s <=8GB, 60s >8GB, 30s if RAM is unknown

On first run, cattery downloads the model (~92MB) and runtime (~18MB).
No accounts or API keys required.
`)
}

// looksLikeCommand returns true if a single-word argument looks like
// it was meant to be a subcommand rather than text to synthesize.
// A single lowercase word with no spaces is suspicious.
func looksLikeCommand(s string) bool {
	if strings.Contains(s, " ") {
		return false
	}
	// Known commands for fuzzy matching
	commands := knownCommandNames()
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

func wantsAdvancedHelp(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--advanced", "-a":
			return true
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

func parseSelectionArgs(args []string) (registry.Kind, string, error) {
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
			if parsed, ok := parseKindAlias(args[i]); ok {
				if kind != "" && kind != parsed {
					return "", "", fmt.Errorf("conflicting kind filters %q and %q", kind, parsed)
				}
				kind = parsed
				continue
			}
			if modelRef != "" {
				return "", "", fmt.Errorf("unexpected argument %q", args[i])
			}
			modelRef = args[i]
		}
	}

	return kind, modelRef, nil
}

func parseKind(ref string) (registry.Kind, error) {
	if kind, ok := parseKindAlias(ref); ok {
		return kind, nil
	}
	return "", fmt.Errorf("unknown kind %q (want: tts, stt, llm, runtime)", ref)
}

func parseKindAlias(ref string) (registry.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "tts":
		return registry.KindTTS, true
	case "stt":
		return registry.KindSTT, true
	case "llm":
		return registry.KindLLM, true
	case "runtime", "ort":
		return registry.KindRuntime, true
	default:
		return "", false
	}
}

func selectModels(kind registry.Kind, modelRef string, localOnly bool) ([]*registry.Model, error) {
	if modelRef != "" {
		model, err := resolveModel(kind, modelRef, localOnly)
		if err != nil {
			return nil, err
		}
		if model == nil {
			if kind != "" {
				scope := kindTitle(kind)
				if localOnly {
					return nil, fmt.Errorf("unknown local %s model %q", scope, modelRef)
				}
				return nil, fmt.Errorf("unknown %s model %q", scope, modelRef)
			}
			return nil, fmt.Errorf("unknown model %q", modelRef)
		}
		return []*registry.Model{model}, nil
	}

	if kind != "" {
		models := modelsForKind(kind, localOnly)
		if len(models) == 0 {
			if localOnly {
				return nil, fmt.Errorf("no local %s models registered", kindTitle(kind))
			}
			return nil, fmt.Errorf("no %s models registered", kindTitle(kind))
		}
		return models, nil
	}

	if localOnly {
		return allLocalModels(displayKindOrder()), nil
	}
	return allVisibleModels(displayKindOrder()), nil
}

func resolveModel(kind registry.Kind, ref string, localOnly bool) (*registry.Model, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}

	if kind != "" {
		model := registry.Resolve(kind, ref)
		if modelAllowed(model, localOnly) {
			return model, nil
		}
		return nil, nil
	}

	if model := registry.Get(ref); modelAllowed(model, localOnly) && modelKindAddressable(model.Kind) {
		return model, nil
	}

	if modelIndex, ok := parseIndex(ref); ok {
		var matches []*registry.Model
		for _, groupKind := range displayKindOrder() {
			model := registry.GetByIndex(groupKind, modelIndex)
			if modelAllowed(model, localOnly) {
				matches = append(matches, model)
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf(
				"model index %d is ambiguous; use tts/stt/llm or --kind",
				modelIndex,
			)
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}

	for _, groupKind := range displayKindOrder() {
		model := registry.Resolve(groupKind, ref)
		if modelAllowed(model, localOnly) {
			return model, nil
		}
	}
	return nil, nil
}

func allLocalModels(order []registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, kind := range order {
		out = append(out, localModelsByKind(kind)...)
	}
	return out
}

func allVisibleModels(order []registry.Kind) []*registry.Model {
	var out []*registry.Model
	for _, kind := range order {
		out = append(out, visibleModelsByKind(kind)...)
	}
	return out
}

func modelsForKind(kind registry.Kind, localOnly bool) []*registry.Model {
	if localOnly {
		return localModelsByKind(kind)
	}
	return visibleModelsByKind(kind)
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

func visibleModelsByKind(kind registry.Kind) []*registry.Model {
	return registry.GetByKind(kind)
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

func orderedKindsFor(models []*registry.Model, order []registry.Kind) []registry.Kind {
	seen := make(map[registry.Kind]bool)
	out := make([]registry.Kind, 0, len(order))

	for _, kind := range order {
		if len(modelsByKind(models, kind)) == 0 {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	for _, model := range models {
		if seen[model.Kind] {
			continue
		}
		seen[model.Kind] = true
		out = append(out, model.Kind)
	}
	return out
}

func displayKindOrder() []registry.Kind {
	return []registry.Kind{registry.KindTTS, registry.KindSTT}
}

func cliKindOrder() []registry.Kind {
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
	case registry.KindLLM:
		return "LLM"
	case registry.KindRuntime:
		return "Runtime"
	default:
		return strings.ToUpper(string(kind))
	}
}

func displayCommandNames() []string {
	return []string{"tts", "stt", "llm", "serve", "status", "download", "help"}
}

func knownCommandNames() []string {
	return []string{"tts", "stt", "llm", "serve", "keys", "list", "status", "download", "help", "version"}
}

func resolveServeModelIndex(kind registry.Kind, ref string) (int, error) {
	model := registry.Resolve(kind, ref)
	if model == nil {
		return 0, fmt.Errorf("unknown %s model %q", strings.ToUpper(string(kind)), ref)
	}
	return model.Index, nil
}

func parseIndex(ref string) (int, bool) {
	var index int
	if _, err := fmt.Sscanf(strings.TrimSpace(ref), "%d", &index); err != nil || index < 1 {
		return 0, false
	}
	return index, true
}

func modelAllowed(model *registry.Model, localOnly bool) bool {
	if model == nil {
		return false
	}
	return !localOnly || model.Location == registry.Local
}

func modelKindAddressable(kind registry.Kind) bool {
	switch kind {
	case registry.KindTTS, registry.KindSTT, registry.KindLLM:
		return true
	default:
		return false
	}
}

func newLLMEngine(model *registry.Model, dataDir string) (*qwen.Engine, error) {
	if model == nil {
		return nil, fmt.Errorf("missing LLM model")
	}

	switch model.ID {
	case "qwen3.5-4b-v1.0":
		return qwen.New(paths.ModelDir(dataDir, model.ID), model)
	default:
		return nil, fmt.Errorf("LLM model %q is not supported yet", model.ID)
	}
}

func modelReady(model *registry.Model, status localModelStatus) bool {
	if model == nil || model.Location != registry.Local {
		return false
	}
	if len(model.Voices) == 0 {
		return status.filesReady()
	}
	return status.filesReady() && status.downloadedVoices == status.totalVoices
}

func voiceReadyMarker(dataDir string, model *registry.Model, voice registry.Voice) string {
	if model == nil || model.Location != registry.Local {
		return " "
	}
	if _, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, voice.File.Filename)); err == nil {
		return "✓"
	}
	return " "
}

func voiceSymbol(model *registry.Model, voice registry.Voice) string {
	if model != nil && model.Location == registry.Remote {
		return "●"
	}
	switch voice.Gender {
	case "male":
		return "♂"
	case "female":
		return "♀"
	default:
		return "●"
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
