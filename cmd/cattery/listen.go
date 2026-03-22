package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/listen"
	"github.com/jikkuatwork/cattery/listen/moonshine"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
)

func cmdListen(args []string) error {
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
			if i < len(args) {
				lang = args[i]
			}
		case "--output", "-o":
			i++
			if i < len(args) {
				outputPath = args[i]
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
			if inputPath != "" {
				return fmt.Errorf("unexpected argument %q\nUsage: cattery listen <audio.wav>", args[i])
			}
			inputPath = args[i]
		}
	}

	model := registry.Resolve(registry.KindSTT, modelRef)
	if model == nil {
		return fmt.Errorf("unknown STT model %q\nRun 'cattery list listen' to see available models", modelRef)
	}
	if model.Location != registry.Local {
		return fmt.Errorf("remote STT model %q is not supported yet", model.ID)
	}

	chunkSize, err := resolveCommandChunkSize(chunkSizeFlag, os.Stderr)
	if err != nil {
		return err
	}

	input, inputName, err := openAudioInput(inputPath, "cattery listen <audio.wav>")
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

	eng, err := newListenEngine(model, dataDir)
	if err != nil {
		return err
	}
	defer eng.Close()

	var result *listen.Result
	err = preflight.GuardMemoryError("transcription", func() error {
		var innerErr error
		result, innerErr = eng.Transcribe(input, listen.Options{
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
		"%s | Used %s on %.1fs from %s, took %.2fs (RTF: %.2f)\n",
		output.name,
		model.Name,
		result.Duration,
		inputName,
		result.Elapsed,
		result.RTF,
	)

	return nil
}

func newListenEngine(model *registry.Model, dataDir string) (listen.Engine, error) {
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
