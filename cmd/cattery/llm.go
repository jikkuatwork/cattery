package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
)

func cmdLLM(args []string) error {
	var (
		systemPrompt string
		modelRef     string
		outputPath   string
		promptParts  []string
		readStdin    bool
		maxTokens    int
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--system":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --system")
			}
			systemPrompt = args[i]
		case "--stdin":
			readStdin = true
		case "--output", "-o":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --output")
			}
			outputPath = args[i]
		case "--max-tokens":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --max-tokens")
			}
			n, err := strconv.Atoi(strings.TrimSpace(args[i]))
			if err != nil || n < 1 {
				return fmt.Errorf("invalid value for --max-tokens: %q", args[i])
			}
			maxTokens = n
		case "--model":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --model")
			}
			modelRef = args[i]
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown flag: %s\nRun 'cattery help' for usage", args[i])
			}
			promptParts = append(promptParts, args[i])
		}
	}

	prompt, err := resolveLLMPrompt(promptParts, readStdin)
	if err != nil {
		return err
	}

	model := registry.Resolve(registry.KindLLM, modelRef)
	if model == nil {
		return fmt.Errorf("unknown LLM model %q\nRun 'cattery list llm' to see available models", modelRef)
	}
	if model.Location != registry.Local {
		return fmt.Errorf("remote LLM model %q is not supported yet", model.ID)
	}
	preflight.WarnLowLLMMemory(os.Stderr, model)

	dataDir := paths.DataDir()
	res, err := download.Ensure(dataDir, model)
	if err != nil {
		return err
	}

	if err := ort.Init(res.ORTLib); err != nil {
		return fmt.Errorf("init ORT: %w", err)
	}
	defer ort.Shutdown()

	eng, err := newLLMEngine(model, dataDir)
	if err != nil {
		return err
	}
	defer eng.Close()

	output, err := openTextOutput(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()

	var result *llm.Result
	t0 := time.Now()
	err = preflight.GuardMemoryError("text generation", func() error {
		var innerErr error
		result, innerErr = eng.Generate(context.Background(), prompt, llm.Options{
			System:    strings.TrimSpace(systemPrompt),
			MaxTokens: maxTokens,
		})
		return innerErr
	})
	if err != nil {
		return err
	}
	elapsed := time.Since(t0)

	if result == nil {
		return fmt.Errorf("generation failed")
	}
	if _, err := io.WriteString(output.writer, result.Text); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s | %s, %d tokens, %.1fs\n",
		output.name, model.Name, result.TokensUsed, elapsed.Seconds())
	return nil
}

func resolveLLMPrompt(parts []string, readFromStdin bool) (string, error) {
	if readFromStdin {
		if len(parts) > 0 {
			return "", fmt.Errorf("prompt provided twice\nUsage: cattery llm [--system TEXT] [--stdin] [--output PATH] [--model REF] [--max-tokens N] \"prompt\"")
		}
		return readStdinText()
	}

	prompt := strings.TrimSpace(strings.Join(parts, " "))
	if prompt == "" {
		return "", fmt.Errorf("no prompt provided\nUsage: cattery llm [--system TEXT] [--stdin] [--output PATH] [--model REF] [--max-tokens N] \"prompt\"")
	}
	return prompt, nil
}
