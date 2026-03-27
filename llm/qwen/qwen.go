package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/registry"
	ortgo "github.com/yalue/onnxruntime_go"
)

const defaultMaxTokens = 256

var _ llm.Engine = (*Engine)(nil)

// Engine runs Qwen local inference with separate embed and decoder sessions.
type Engine struct {
	embed     *ortgo.DynamicAdvancedSession
	decoder   *ortgo.DynamicAdvancedSession
	tokenizer *Tokenizer
	model     *registry.Model
	maxTokens int
	spec      *modelSpec
}

type config struct {
	DecoderFile          string
	EmbedFile            string
	TokenizerFile        string
	ConfigFile           string
	GenerationConfigFile string
	ContextWindow        int
	MaxTokens            int
}

type modelSpec struct {
	embedInputs       []ortgo.InputOutputInfo
	embedOutputs      []ortgo.InputOutputInfo
	decoderInputs     []ortgo.InputOutputInfo
	decoderOutputs    []ortgo.InputOutputInfo
	inputIDsName      string
	inputsEmbedsName  string
	attentionMaskName string
	positionIDsName   string
	logitsName        string
	convStates        []stateSpec
	recurrentStates   []stateSpec
	kvStates          []stateSpec
	eosTokenIDs       map[int64]bool
}

type generationConfig struct {
	EOSTokenID any `json:"eos_token_id"`
}

// New creates a Qwen engine from a downloaded model dir and registry model.
func New(modelDir string, model *registry.Model) (*Engine, error) {
	modelDir = strings.TrimSpace(modelDir)
	if modelDir == "" {
		return nil, fmt.Errorf("model dir is empty")
	}
	if model == nil {
		return nil, fmt.Errorf("missing registry model")
	}
	if !ort.IsInitialized() {
		return nil, fmt.Errorf("ort is not initialized")
	}

	cfg := configFromModel(model)

	tokenizer, err := LoadTokenizer(filepath.Join(modelDir, cfg.TokenizerFile))
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	spec, err := inspectGraphs(modelDir, cfg)
	if err != nil {
		return nil, err
	}
	spec.eosTokenIDs, err = loadEOSTokens(modelDir, cfg, model)
	if err != nil {
		return nil, err
	}

	embed, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, cfg.EmbedFile),
		ioNames(spec.embedInputs),
		ioNames(spec.embedOutputs),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load embed session: %w", err)
	}

	decoder, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, cfg.DecoderFile),
		ioNames(spec.decoderInputs),
		ioNames(spec.decoderOutputs),
		nil,
	)
	if err != nil {
		embed.Destroy()
		return nil, fmt.Errorf("load decoder session: %w", err)
	}

	return &Engine{
		embed:     embed,
		decoder:   decoder,
		tokenizer: tokenizer,
		model:     model,
		maxTokens: cfg.MaxTokens,
		spec:      spec,
	}, nil
}

// Generate formats a ChatML prompt and runs greedy decode.
func (e *Engine) Generate(ctx context.Context, prompt string, opts llm.Options) (*llm.Result, error) {
	if e == nil || e.embed == nil || e.decoder == nil || e.tokenizer == nil || e.spec == nil {
		return nil, fmt.Errorf("engine is closed")
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	formatted := FormatChat(strings.TrimSpace(opts.System), prompt)
	promptIDs, err := e.tokenizer.encode(formatted)
	if err != nil {
		return nil, fmt.Errorf("tokenize prompt: %w", err)
	}

	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = e.maxTokens
	}
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	generatedIDs, finishReason, err := generateTokens(ctx, e.embed, e.decoder, e.spec, intsToInt64s(promptIDs), maxTokens)
	if err != nil {
		return nil, err
	}

	text, err := e.tokenizer.decode(int64sToInts(generatedIDs))
	if err != nil {
		return nil, fmt.Errorf("decode tokens: %w", err)
	}

	text, stopMatched := applyStopStrings(text, opts.Stop)
	if stopMatched {
		finishReason = "stop"
	}

	return &llm.Result{
		Text:         text,
		TokensUsed:   len(promptIDs) + len(generatedIDs),
		FinishReason: finishReason,
	}, nil
}

// Close releases the ONNX sessions.
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	if e.decoder != nil {
		e.decoder.Destroy()
		e.decoder = nil
	}
	if e.embed != nil {
		e.embed.Destroy()
		e.embed = nil
	}
	e.tokenizer = nil
	e.spec = nil
	return nil
}

func configFromModel(model *registry.Model) config {
	return config{
		DecoderFile:          model.MetaString("decoder_file", "onnx/decoder_model_merged_q4.onnx"),
		EmbedFile:            model.MetaString("embed_file", "onnx/embed_tokens_q4.onnx"),
		TokenizerFile:        model.MetaString("tokenizer_file", "tokenizer.json"),
		ConfigFile:           model.MetaString("config_file", "config.json"),
		GenerationConfigFile: model.MetaString("generation_config_file", "generation_config.json"),
		ContextWindow:        model.MetaInt("context_window", 0),
		MaxTokens:            model.MetaInt("max_tokens", defaultMaxTokens),
	}
}

func inspectGraphs(modelDir string, cfg config) (*modelSpec, error) {
	embedInputs, embedOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, cfg.EmbedFile))
	if err != nil {
		return nil, fmt.Errorf("embed I/O: %w", err)
	}
	decoderInputs, decoderOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, cfg.DecoderFile))
	if err != nil {
		return nil, fmt.Errorf("decoder I/O: %w", err)
	}

	spec := &modelSpec{
		embedInputs:       embedInputs,
		embedOutputs:      embedOutputs,
		decoderInputs:     decoderInputs,
		decoderOutputs:    decoderOutputs,
		inputIDsName:      findExact(embedInputs, "input_ids"),
		inputsEmbedsName:  findExact(decoderInputs, "inputs_embeds"),
		attentionMaskName: findContaining(decoderInputs, "attention_mask"),
		positionIDsName:   findContaining(decoderInputs, "position_ids"),
		logitsName:        findExact(decoderOutputs, "logits"),
	}

	if spec.inputIDsName == "" {
		return nil, fmt.Errorf("embed graph missing input_ids")
	}
	if spec.inputsEmbedsName == "" {
		return nil, fmt.Errorf("decoder graph missing inputs_embeds")
	}
	if spec.logitsName == "" {
		return nil, fmt.Errorf("decoder graph missing logits")
	}

	for _, info := range decoderInputs {
		switch {
		case strings.HasPrefix(info.Name, "past_conv."):
			spec.convStates = append(spec.convStates, stateSpec{
				input:      info,
				outputName: stateOutputName(info.Name),
				family:     stateFamilyConv,
			})
		case strings.HasPrefix(info.Name, "past_recurrent."):
			spec.recurrentStates = append(spec.recurrentStates, stateSpec{
				input:      info,
				outputName: stateOutputName(info.Name),
				family:     stateFamilyRecurrent,
			})
		case strings.HasPrefix(info.Name, "past_key_values."):
			spec.kvStates = append(spec.kvStates, stateSpec{
				input:      info,
				outputName: stateOutputName(info.Name),
				family:     stateFamilyKV,
			})
		}
	}

	sortStateSpecs(spec.convStates)
	sortStateSpecs(spec.recurrentStates)
	sortStateSpecs(spec.kvStates)
	for _, state := range append(append([]stateSpec(nil), spec.convStates...), append(spec.recurrentStates, spec.kvStates...)...) {
		if state.outputName == "" {
			return nil, fmt.Errorf("unsupported state input %q", state.input.Name)
		}
		if indexOfName(decoderOutputs, state.outputName) < 0 {
			return nil, fmt.Errorf("missing state output %q", state.outputName)
		}
	}

	return spec, nil
}

func loadEOSTokens(modelDir string, cfg config, model *registry.Model) (map[int64]bool, error) {
	out := make(map[int64]bool)

	if model != nil {
		if token, ok := parseInt64(strings.TrimSpace(model.MetaString("eos_token", ""))); ok {
			out[token] = true
		}
	}

	path := filepath.Join(modelDir, cfg.GenerationConfigFile)
	data, err := os.ReadFile(path)
	if err == nil {
		var gen generationConfig
		if err := json.Unmarshal(data, &gen); err != nil {
			return nil, fmt.Errorf("parse generation config: %w", err)
		}
		for _, token := range parseTokenIDs(gen.EOSTokenID) {
			out[token] = true
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read generation config: %w", err)
	}

	if len(out) == 0 {
		out[248044] = true
		out[248046] = true
	}
	return out, nil
}

func parseTokenIDs(raw any) []int64 {
	switch v := raw.(type) {
	case float64:
		return []int64{int64(v)}
	case int:
		return []int64{int64(v)}
	case int64:
		return []int64{v}
	case []any:
		var out []int64
		for _, item := range v {
			out = append(out, parseTokenIDs(item)...)
		}
		return out
	default:
		return nil
	}
}

func parseInt64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func applyStopStrings(text string, stops []string) (string, bool) {
	stopAt := -1
	for _, stop := range stops {
		if stop == "" {
			continue
		}
		idx := strings.Index(text, stop)
		if idx < 0 {
			continue
		}
		if stopAt < 0 || idx < stopAt {
			stopAt = idx
		}
	}
	if stopAt < 0 {
		return text, false
	}
	return text[:stopAt], true
}

func ioNames(infos []ortgo.InputOutputInfo) []string {
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Name)
	}
	return out
}

func findExact(infos []ortgo.InputOutputInfo, want string) string {
	for _, info := range infos {
		if info.Name == want {
			return info.Name
		}
	}
	return ""
}

func findContaining(infos []ortgo.InputOutputInfo, needle string) string {
	for _, info := range infos {
		if strings.Contains(info.Name, needle) {
			return info.Name
		}
	}
	return ""
}

func sortStateSpecs(specs []stateSpec) {
	for i := 0; i < len(specs)-1; i++ {
		for j := i + 1; j < len(specs); j++ {
			if specs[j].input.Name < specs[i].input.Name {
				specs[i], specs[j] = specs[j], specs[i]
			}
		}
	}
}

func intsToInt64s(in []int) []int64 {
	out := make([]int64, len(in))
	for i, v := range in {
		out[i] = int64(v)
	}
	return out
}

func int64sToInts(in []int64) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}
