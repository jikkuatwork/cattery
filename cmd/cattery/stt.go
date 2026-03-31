package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/stt/moonshine"
)

func cmdSTT(args []string) error {
	var (
		inputPath     string
		outputPath    string
		lang          string
		modelRef      string
		chunkSizeFlag string
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lang":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --lang")
			}
			lang = args[i]
		case "--output", "-o":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --output")
			}
			outputPath = args[i]
		case "--model":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --model")
			}
			modelRef = args[i]
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
			if inputPath != "" {
				return fmt.Errorf("unexpected argument %q\nUsage: cattery stt <audio.wav>", args[i])
			}
			inputPath = args[i]
		}
	}

	model := registry.Resolve(registry.KindSTT, modelRef)
	if model == nil {
		return fmt.Errorf("unknown STT model %q\nRun 'cattery list stt' to see available models", modelRef)
	}
	if model.Location != registry.Local {
		return fmt.Errorf("remote STT model %q is not supported yet", model.ID)
	}

	chunkSize, err := resolveCommandChunkSize(chunkSizeFlag, os.Stderr)
	if err != nil {
		return err
	}

	input, _, err := openAudioInput(inputPath, "cattery stt <audio.wav>")
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := openTextOutput(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()

	dataDir := paths.DataDir()
	res, err := download.Ensure(dataDir, model)
	if err != nil {
		return err
	}

	if err := ort.Init(res.ORTLib); err != nil {
		return fmt.Errorf("init ORT: %w", err)
	}
	defer ort.Shutdown()

	eng, err := newSTTEngine(model, dataDir)
	if err != nil {
		return err
	}
	defer eng.Close()

	var result *stt.Result
	err = preflight.GuardMemoryError("transcription", func() error {
		var innerErr error
		result, innerErr = eng.Transcribe(input, stt.Options{
			Lang:      lang,
			ChunkSize: chunkSize,
		})
		return innerErr
	})
	if err != nil {
		return err
	}

	if err := writeTranscript(output.writer, result.Text); err != nil {
		return err
	}

	fmt.Fprintf(
		os.Stderr,
		"%s | %.1fs audio, %.1fs (RTF: %.2f)\n",
		output.name,
		result.Duration,
		result.Elapsed,
		result.RTF,
	)

	return nil
}

func newSTTEngine(model *registry.Model, dataDir string) (stt.Engine, error) {
	if model == nil {
		return nil, fmt.Errorf("missing STT model")
	}

	switch model.ID {
	case "moonshine-tiny-v1.0":
		return moonshine.New(paths.ModelDir(dataDir, model.ID), model.Meta)
	default:
		return nil, fmt.Errorf("STT model %q is not supported yet", model.ID)
	}
}

func writeTranscript(w io.Writer, text string) error {
	text = strings.TrimRight(text, "\r\n")
	if _, err := io.WriteString(w, text); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := io.WriteString(w, "\n")
	return err
}
