package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"github.com/jikkuatwork/cattery/ort"
	ortgo "github.com/yalue/onnxruntime_go"
	"golang.org/x/text/unicode/norm"
)

const (
	modelRepo = "https://huggingface.co/onnx-community/Qwen3.5-0.8B-ONNX/resolve/main"
	modelDir  = "models-data/qwen3.5-0.8b"

	decoderRelPath = "onnx/decoder_model_merged_q4.onnx"
	embedRelPath   = "onnx/embed_tokens_q4.onnx"

	tokenizerRelPath        = "tokenizer.json"
	configRelPath           = "config.json"
	generationConfigRelPath = "generation_config.json"

	defaultPrompt    = "<|im_start|>user\nWrite one short sentence about cats.<|im_end|>\n<|im_start|>assistant\n"
	defaultMaxTokens = 64

	eosToken1 = 248044
	eosToken2 = 248046
)

var modelFiles = []string{
	decoderRelPath,
	"onnx/decoder_model_merged_q4.onnx_data",
	embedRelPath,
	"onnx/embed_tokens_q4.onnx_data",
	tokenizerRelPath,
	configRelPath,
	generationConfigRelPath,
}

type hfTokenizerJSON struct {
	Normalizer struct {
		Type string `json:"type"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type          string `json:"type"`
		Pretokenizers []struct {
			Type    string `json:"type"`
			Pattern struct {
				Regex string `json:"Regex"`
			} `json:"pattern"`
		} `json:"pretokenizers"`
	} `json:"pre_tokenizer"`
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
		Special bool   `json:"special"`
	} `json:"added_tokens"`
	Model struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []any          `json:"merges"`
	} `json:"model"`
	Decoder struct {
		Type string `json:"type"`
	} `json:"decoder"`
}

type qwenConfig struct {
	TextConfig struct {
		HiddenSize        int    `json:"hidden_size"`
		NumHiddenLayers   int    `json:"num_hidden_layers"`
		NumAttentionHeads int    `json:"num_attention_heads"`
		NumKeyValueHeads  int    `json:"num_key_value_heads"`
		MaxPositionEmbeds int    `json:"max_position_embeddings"`
		EOSTokenID        any    `json:"eos_token_id"`
		BOSTokenID        int64  `json:"bos_token_id"`
		HeadDim           int    `json:"head_dim"`
		SlidingWindow     any    `json:"sliding_window"`
		RopeTheta         any    `json:"rope_theta"`
		HiddenAct         string `json:"hidden_act"`
		IntermediateSize  int    `json:"intermediate_size"`
		VocabSize         int    `json:"vocab_size"`
	} `json:"text_config"`
}

type generationConfig struct {
	EOSTokenID any `json:"eos_token_id"`
}

type qwenTokenizer struct {
	vocab         map[string]int
	idToToken     map[int]string
	bpeRanks      map[string]int
	specialToID   map[string]int
	idToSpecial   map[int]string
	specialTokens []string
	splitRe       *regexp2.Regexp
	byteEncoder   [256]string
	byteDecoder   map[rune]byte
	cache         map[string][]int
}

type textPart struct {
	text      string
	isSpecial bool
}

type modelSpec struct {
	decoderInputs  []ortgo.InputOutputInfo
	decoderOutputs []ortgo.InputOutputInfo
	embedInputs    []ortgo.InputOutputInfo
	embedOutputs   []ortgo.InputOutputInfo

	inputsEmbedsName string
	inputIDsName     string
	attentionMask    string
	positionIDs      string
	logitsName       string
	stateInputs      []ortgo.InputOutputInfo
	stateOutputs     []ortgo.InputOutputInfo
	stateOutputByIn  map[string]string

	kvHeads  int64
	headDim  int64
	hidden   int64
	numLayer int
}

type metrics struct {
	firstTokenLatency time.Duration
	totalTime         time.Duration
	steadyTokensPerS  float64
	peakRSSKiB        int64
}

func main() {
	prompt := defaultPrompt
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	fmt.Println("=== Qwen3.5 0.8B LLM Spike ===")
	fmt.Printf("Model dir: %s\n", modelDir)

	if err := ensureFiles(); err != nil {
		fatalf("ensure files: %v", err)
	}

	cfg, genCfg, err := loadConfig()
	if err != nil {
		fatalf("load config: %v", err)
	}

	tk, err := loadTokenizer(filepath.Join(modelDir, tokenizerRelPath))
	if err != nil {
		fatalf("load tokenizer: %v", err)
	}

	ortLib := filepath.Join(os.Getenv("HOME"), ".cattery", "ort", "libonnxruntime.so.1.24.4")
	if _, err := os.Stat(ortLib); err != nil {
		fatalf("missing ORT shared library at %s: %v", ortLib, err)
	}
	if err := ort.Init(ortLib); err != nil {
		fatalf("init ORT: %v", err)
	}
	defer ort.Shutdown()

	spec, err := inspectGraphs(cfg)
	if err != nil {
		fatalf("inspect graphs: %v", err)
	}

	embedSession, decoderSession, err := openSessions(spec)
	if err != nil {
		fatalf("open sessions: %v", err)
	}
	defer embedSession.Destroy()
	defer decoderSession.Destroy()

	inputIDs, _, err := tk.Encode(prompt)
	if err != nil {
		fatalf("tokenize prompt: %v", err)
	}
	fmt.Printf("Prompt token count: %d\n", len(inputIDs))

	outputIDs, generatedText, m, err := generate(embedSession, decoderSession, spec, tk, cfg, genCfg, inputIDs, defaultMaxTokens)
	if err != nil {
		fatalf("generate: %v", err)
	}

	fmt.Println("\n=== Result ===")
	fmt.Printf("Generated token IDs: %v\n", outputIDs)
	fmt.Printf("Generated text: %q\n", generatedText)

	fmt.Println("\n=== Metrics ===")
	fmt.Printf("First-token latency: %v\n", m.firstTokenLatency)
	fmt.Printf("Steady-state tokens/s: %.2f\n", m.steadyTokensPerS)
	fmt.Printf("Peak RSS: %.2f MiB\n", float64(m.peakRSSKiB)/1024.0)
	fmt.Printf("Total generation time: %v\n", m.totalTime)
}

func ensureFiles() error {
	for _, rel := range modelFiles {
		url := modelRepo + "/" + rel
		dest := filepath.Join(modelDir, filepath.FromSlash(rel))
		if _, err := ensureFile(url, dest); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	return nil
}

func openSessions(spec *modelSpec) (*ortgo.DynamicAdvancedSession, *ortgo.DynamicAdvancedSession, error) {
	embedSession, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, filepath.FromSlash(embedRelPath)),
		ioNames(spec.embedInputs),
		ioNames(spec.embedOutputs),
		nil,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("load embed session: %w", err)
	}

	decoderSession, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, filepath.FromSlash(decoderRelPath)),
		ioNames(spec.decoderInputs),
		ioNames(spec.decoderOutputs),
		nil,
	)
	if err != nil {
		embedSession.Destroy()
		return nil, nil, fmt.Errorf("load decoder session: %w", err)
	}
	return embedSession, decoderSession, nil
}

func inspectGraphs(cfg *qwenConfig) (*modelSpec, error) {
	embedInputs, embedOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, filepath.FromSlash(embedRelPath)))
	if err != nil {
		return nil, fmt.Errorf("embed I/O: %w", err)
	}
	decoderInputs, decoderOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, filepath.FromSlash(decoderRelPath)))
	if err != nil {
		return nil, fmt.Errorf("decoder I/O: %w", err)
	}

	fmt.Println("\n=== Graph I/O ===")
	printIO("Embed inputs", embedInputs)
	printIO("Embed outputs", embedOutputs)
	printIO("Decoder inputs", decoderInputs)
	printIO("Decoder outputs", decoderOutputs)

	spec := &modelSpec{
		embedInputs:      embedInputs,
		embedOutputs:     embedOutputs,
		decoderInputs:    decoderInputs,
		decoderOutputs:   decoderOutputs,
		kvHeads:          int64(cfg.TextConfig.NumKeyValueHeads),
		headDim:          int64(cfg.TextConfig.HeadDim),
		hidden:           int64(cfg.TextConfig.HiddenSize),
		numLayer:         cfg.TextConfig.NumHiddenLayers,
		inputIDsName:     findExact(embedInputs, "input_ids"),
		inputsEmbedsName: findExact(decoderInputs, "inputs_embeds"),
		attentionMask:    findFirstContaining(decoderInputs, "attention_mask"),
		positionIDs:      findFirstContaining(decoderInputs, "position_ids"),
		logitsName:       findExact(decoderOutputs, "logits"),
		stateOutputByIn:  make(map[string]string),
	}
	if spec.inputIDsName == "" {
		return nil, fmt.Errorf("embed graph missing input_ids")
	}
	if spec.inputsEmbedsName == "" {
		return nil, fmt.Errorf("decoder graph missing inputs_embeds")
	}
	if spec.logitsName == "" {
		return nil, fmt.Errorf("decoder graph missing logits output")
	}

	for _, info := range decoderInputs {
		if isStateInput(info.Name) {
			spec.stateInputs = append(spec.stateInputs, info)
		}
	}
	for _, info := range decoderOutputs {
		if isStateOutput(info.Name) {
			spec.stateOutputs = append(spec.stateOutputs, info)
		}
	}
	sort.Slice(spec.stateInputs, func(i, j int) bool { return spec.stateInputs[i].Name < spec.stateInputs[j].Name })
	sort.Slice(spec.stateOutputs, func(i, j int) bool { return spec.stateOutputs[i].Name < spec.stateOutputs[j].Name })

	if len(spec.stateInputs) == 0 || len(spec.stateOutputs) == 0 {
		return nil, fmt.Errorf("decoder graph missing KV cache I/O")
	}
	if len(spec.stateInputs) != len(spec.stateOutputs) {
		return nil, fmt.Errorf("mismatched decoder state I/O: %d inputs %d outputs", len(spec.stateInputs), len(spec.stateOutputs))
	}
	for _, input := range spec.stateInputs {
		outputName := stateOutputName(input.Name)
		if indexOfName(spec.stateOutputs, outputName) < 0 {
			return nil, fmt.Errorf("missing output for decoder state %q", input.Name)
		}
		spec.stateOutputByIn[input.Name] = outputName
	}
	return spec, nil
}

func generate(
	embedSession *ortgo.DynamicAdvancedSession,
	decoderSession *ortgo.DynamicAdvancedSession,
	spec *modelSpec,
	tk *qwenTokenizer,
	cfg *qwenConfig,
	genCfg *generationConfig,
	promptIDs []int,
	maxTokens int,
) ([]int64, string, metrics, error) {
	if len(promptIDs) == 0 {
		return nil, "", metrics{}, fmt.Errorf("prompt encoded to zero tokens")
	}

	prompt := intsToInt64s(promptIDs)
	inputEmbeds, err := embedTokens(embedSession, spec, prompt)
	if err != nil {
		return nil, "", metrics{}, err
	}
	defer inputEmbeds.Destroy()

	eosSet := eosTokens(cfg, genCfg)
	var generated []int64
	var firstTokenAt time.Time
	start := time.Now()
	peakRSS := readRSSKiB()

	cache := make([]ortgo.Value, len(spec.stateInputs))
	defer destroyValues(cache)

	totalSeq := int64(len(prompt))
	stepEmbeds := ortgo.Value(inputEmbeds)

	for step := 0; step < maxTokens; step++ {
		seqLen := embeddingsSeqLen(stepEmbeds)
		if seqLen <= 0 {
			return nil, "", metrics{}, fmt.Errorf("invalid embeddings seq len at step %d", step)
		}

		mask, positions, err := buildAuxTensors(spec, totalSeq, seqLen)
		if err != nil {
			return nil, "", metrics{}, fmt.Errorf("step %d aux tensors: %w", step, err)
		}

		inputs, temp, err := buildDecoderInputs(spec, stepEmbeds, cache, mask, positions)
		if err != nil {
			destroyMaybe(mask)
			destroyMaybe(positions)
			return nil, "", metrics{}, fmt.Errorf("step %d decoder inputs: %w", step, err)
		}

		outputs := make([]ortgo.Value, len(spec.decoderOutputs))
		runErr := decoderSession.Run(inputs, outputs)

		destroyValues(temp)
		destroyMaybe(mask)
		destroyMaybe(positions)
		if runErr != nil {
			destroyValues(outputs)
			return nil, "", metrics{}, fmt.Errorf("decoder run step %d: %w", step, runErr)
		}

		next, err := nextToken(outputs[indexOfName(spec.decoderOutputs, spec.logitsName)])
		if err != nil {
			destroyValues(outputs)
			return nil, "", metrics{}, fmt.Errorf("decoder logits step %d: %w", step, err)
		}

		if step == 0 {
			firstTokenAt = time.Now()
		}

		if err := replaceCache(cache, outputs, spec); err != nil {
			destroyValues(outputs)
			return nil, "", metrics{}, fmt.Errorf("decoder cache step %d: %w", step, err)
		}

		generated = append(generated, next)
		if eosSet[next] {
			break
		}

		nextEmbeds, err := embedTokens(embedSession, spec, []int64{next})
		if err != nil {
			return nil, "", metrics{}, fmt.Errorf("embed next token step %d: %w", step, err)
		}
		if step > 0 {
			stepEmbeds.Destroy()
		}
		stepEmbeds = nextEmbeds
		totalSeq++
		if rss := readRSSKiB(); rss > peakRSS {
			peakRSS = rss
		}
	}

	if stepEmbeds != nil && stepEmbeds != inputEmbeds {
		stepEmbeds.Destroy()
	}

	decoded, err := tk.Decode(int64sToInts(generated))
	if err != nil {
		return nil, "", metrics{}, fmt.Errorf("decode tokens: %w", err)
	}

	total := time.Since(start)
	firstLatency := total
	if !firstTokenAt.IsZero() {
		firstLatency = firstTokenAt.Sub(start)
	}
	var steady float64
	if len(generated) > 1 {
		rest := start.Add(total).Sub(firstTokenAt)
		if rest > 0 {
			steady = float64(len(generated)-1) / rest.Seconds()
		}
	}

	return generated, decoded, metrics{
		firstTokenLatency: firstLatency,
		totalTime:         total,
		steadyTokensPerS:  steady,
		peakRSSKiB:        peakRSS,
	}, nil
}

func buildDecoderInputs(spec *modelSpec, embeds ortgo.Value, cache []ortgo.Value, mask, positions ortgo.Value) ([]ortgo.Value, []ortgo.Value, error) {
	inputs := make([]ortgo.Value, 0, len(spec.decoderInputs))
	var temp []ortgo.Value

	for _, info := range spec.decoderInputs {
		switch info.Name {
		case spec.inputsEmbedsName:
			inputs = append(inputs, embeds)
		case spec.attentionMask:
			if mask == nil {
				return nil, nil, fmt.Errorf("missing attention mask for %s", info.Name)
			}
			inputs = append(inputs, mask)
		case spec.positionIDs:
			if positions == nil {
				return nil, nil, fmt.Errorf("missing position ids for %s", info.Name)
			}
			inputs = append(inputs, positions)
		default:
			if idx := indexOfStateInput(spec, info.Name); idx >= 0 {
				if cache[idx] == nil {
					tensor, err := zeroStateTensor(info)
					if err != nil {
						return nil, nil, err
					}
					temp = append(temp, tensor)
					inputs = append(inputs, tensor)
				} else {
					inputs = append(inputs, cache[idx])
				}
				continue
			}
			return nil, nil, fmt.Errorf("unsupported decoder input %q", info.Name)
		}
	}
	return inputs, temp, nil
}

func buildAuxTensors(spec *modelSpec, totalSeq, stepSeq int64) (ortgo.Value, ortgo.Value, error) {
	var mask ortgo.Value
	var positions ortgo.Value

	if spec.attentionMask != "" {
		values := make([]int64, totalSeq)
		for i := range values {
			values[i] = 1
		}
		t, err := ortgo.NewTensor(ortgo.NewShape(1, totalSeq), values)
		if err != nil {
			return nil, nil, err
		}
		mask = t
	}
	if spec.positionIDs != "" {
		start := totalSeq - stepSeq
		values := make([]int64, 3*stepSeq)
		for group := int64(0); group < 3; group++ {
			for i := int64(0); i < stepSeq; i++ {
				values[group*stepSeq+i] = start + i
			}
		}
		t, err := ortgo.NewTensor(ortgo.NewShape(3, 1, stepSeq), values)
		if err != nil {
			destroyMaybe(mask)
			return nil, nil, err
		}
		positions = t
	}
	return mask, positions, nil
}

func replaceCache(cache []ortgo.Value, outputs []ortgo.Value, spec *modelSpec) error {
	for _, input := range spec.stateInputs {
		outName := spec.stateOutputByIn[input.Name]
		outIdx := indexOfName(spec.decoderOutputs, outName)
		if outIdx < 0 || outputs[outIdx] == nil {
			return fmt.Errorf("missing state output %q", outName)
		}
		cacheIdx := indexOfStateInput(spec, input.Name)
		if cacheIdx < 0 {
			return fmt.Errorf("no cache slot for %q", input.Name)
		}
		if cache[cacheIdx] != nil {
			cache[cacheIdx].Destroy()
		}
		cache[cacheIdx] = outputs[outIdx]
		outputs[outIdx] = nil
	}

	logitsIdx := indexOfName(spec.decoderOutputs, spec.logitsName)
	if logitsIdx >= 0 && outputs[logitsIdx] != nil {
		outputs[logitsIdx].Destroy()
		outputs[logitsIdx] = nil
	}
	destroyValues(outputs)
	return nil
}

func embedTokens(session *ortgo.DynamicAdvancedSession, spec *modelSpec, ids []int64) (*ortgo.Tensor[float32], error) {
	idsTensor, err := ortgo.NewTensor(ortgo.NewShape(1, int64(len(ids))), ids)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer idsTensor.Destroy()

	outputs := make([]ortgo.Value, len(spec.embedOutputs))
	if err := session.Run([]ortgo.Value{idsTensor}, outputs); err != nil {
		return nil, fmt.Errorf("run embed session: %w", err)
	}

	if len(outputs) != 1 {
		destroyValues(outputs)
		return nil, fmt.Errorf("unexpected embed output count %d", len(outputs))
	}
	tensor, ok := outputs[0].(*ortgo.Tensor[float32])
	if !ok {
		destroyValues(outputs)
		return nil, fmt.Errorf("unexpected embed output type %T", outputs[0])
	}
	return tensor, nil
}

func isStateInput(name string) bool {
	return strings.HasPrefix(name, "past_")
}

func isStateOutput(name string) bool {
	return strings.HasPrefix(name, "present")
}

func stateOutputName(inputName string) string {
	switch {
	case strings.HasPrefix(inputName, "past_conv."):
		return strings.Replace(inputName, "past_conv.", "present_conv.", 1)
	case strings.HasPrefix(inputName, "past_recurrent."):
		return strings.Replace(inputName, "past_recurrent.", "present_recurrent.", 1)
	case strings.HasPrefix(inputName, "past_key_values."):
		return strings.Replace(inputName, "past_key_values.", "present.", 1)
	default:
		return ""
	}
}

func indexOfStateInput(spec *modelSpec, want string) int {
	for i, info := range spec.stateInputs {
		if info.Name == want {
			return i
		}
	}
	return -1
}

func zeroStateTensor(info ortgo.InputOutputInfo) (*ortgo.Tensor[float32], error) {
	shape := make([]int64, len(info.Dimensions))
	size := int64(1)
	for i, dim := range info.Dimensions {
		switch {
		case dim > 0:
			shape[i] = dim
		case i == 0:
			shape[i] = 1
		case strings.HasPrefix(info.Name, "past_key_values.") && i == 2:
			shape[i] = 0
		default:
			shape[i] = 1
		}
		size *= shape[i]
	}
	if size == 0 {
		return ortgo.NewTensor(ortgo.Shape(shape), []float32{})
	}
	return ortgo.NewTensor(ortgo.Shape(shape), make([]float32, size))
}

func loadConfig() (*qwenConfig, *generationConfig, error) {
	var cfg qwenConfig
	if err := readJSON(filepath.Join(modelDir, configRelPath), &cfg); err != nil {
		return nil, nil, err
	}
	var gen generationConfig
	if err := readJSON(filepath.Join(modelDir, generationConfigRelPath), &gen); err != nil {
		return nil, nil, err
	}
	if cfg.TextConfig.HeadDim == 0 && cfg.TextConfig.NumAttentionHeads > 0 {
		cfg.TextConfig.HeadDim = cfg.TextConfig.HiddenSize / cfg.TextConfig.NumAttentionHeads
	}
	return &cfg, &gen, nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func loadTokenizer(tokenizerPath string) (*qwenTokenizer, error) {
	tokenizerData, err := os.ReadFile(tokenizerPath)
	if err != nil {
		return nil, err
	}

	var meta hfTokenizerJSON
	if err := json.Unmarshal(tokenizerData, &meta); err != nil {
		return nil, err
	}

	regex := ""
	for _, pt := range meta.PreTokenizer.Pretokenizers {
		if pt.Type == "Split" {
			regex = pt.Pattern.Regex
			break
		}
	}
	if regex == "" {
		return nil, fmt.Errorf("no split regex found")
	}

	splitRe, err := regexp2.Compile(regex, 0)
	if err != nil {
		return nil, fmt.Errorf("compile split regex: %w", err)
	}

	byteEncoder, byteDecoder := buildByteTables()
	idToToken := make(map[int]string, len(meta.Model.Vocab))
	for token, id := range meta.Model.Vocab {
		idToToken[id] = token
	}

	bpeRanks := make(map[string]int, len(meta.Model.Merges))
	for i, raw := range meta.Model.Merges {
		pair, err := mergePair(raw)
		if err != nil {
			return nil, fmt.Errorf("parse merge %d: %w", i, err)
		}
		bpeRanks[pairKey(pair[0], pair[1])] = i
	}

	specialToID := make(map[string]int)
	idToSpecial := make(map[int]string)
	var specialTokens []string
	for _, item := range meta.AddedTokens {
		idToToken[item.ID] = item.Content
		if !item.Special {
			continue
		}
		specialToID[item.Content] = item.ID
		idToSpecial[item.ID] = item.Content
		specialTokens = append(specialTokens, item.Content)
	}
	sort.Slice(specialTokens, func(i, j int) bool {
		if len(specialTokens[i]) == len(specialTokens[j]) {
			return specialTokens[i] < specialTokens[j]
		}
		return len(specialTokens[i]) > len(specialTokens[j])
	})

	return &qwenTokenizer{
		vocab:         meta.Model.Vocab,
		idToToken:     idToToken,
		bpeRanks:      bpeRanks,
		specialToID:   specialToID,
		idToSpecial:   idToSpecial,
		specialTokens: specialTokens,
		splitRe:       splitRe,
		byteEncoder:   byteEncoder,
		byteDecoder:   byteDecoder,
		cache:         make(map[string][]int),
	}, nil
}

func (t *qwenTokenizer) Encode(input string) ([]int, []string, error) {
	input = norm.NFC.String(input)
	parts := t.splitSpecialTokens(input)

	var ids []int
	var tokens []string
	for _, part := range parts {
		if part.isSpecial {
			id := t.specialToID[part.text]
			ids = append(ids, id)
			tokens = append(tokens, part.text)
			continue
		}
		pieceIDs, pieceTokens, err := t.encodeOrdinary(part.text)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, pieceIDs...)
		tokens = append(tokens, pieceTokens...)
	}

	return ids, tokens, nil
}

func (t *qwenTokenizer) Decode(ids []int) (string, error) {
	var encoded strings.Builder
	for _, id := range ids {
		if special, ok := t.idToSpecial[id]; ok {
			encoded.WriteString(special)
			continue
		}
		token, ok := t.idToToken[id]
		if !ok {
			return "", fmt.Errorf("unknown token id %d", id)
		}
		encoded.WriteString(token)
	}
	return decodeBytes(encoded.String(), t.byteDecoder)
}

func (t *qwenTokenizer) splitSpecialTokens(input string) []textPart {
	if len(t.specialTokens) == 0 || input == "" {
		return []textPart{{text: input}}
	}

	var parts []textPart
	for len(input) > 0 {
		matched := false
		for _, tok := range t.specialTokens {
			if strings.HasPrefix(input, tok) {
				parts = append(parts, textPart{text: tok, isSpecial: true})
				input = input[len(tok):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		next := len(input)
		for _, tok := range t.specialTokens {
			if idx := strings.Index(input, tok); idx >= 0 && idx < next {
				next = idx
			}
		}
		parts = append(parts, textPart{text: input[:next]})
		input = input[next:]
	}
	return parts
}

func (t *qwenTokenizer) encodeOrdinary(input string) ([]int, []string, error) {
	if input == "" {
		return nil, nil, nil
	}

	var ids []int
	var tokens []string
	m, err := t.splitRe.FindStringMatch(input)
	if err != nil {
		return nil, nil, err
	}

	lastRune := 0
	for m != nil {
		matchStart := runeIndexToByteOffset(input, m.Index)
		if m.Index > lastRune {
			missed := input[runeIndexToByteOffset(input, lastRune):matchStart]
			missedIDs, missedTokens, err := t.encodeTextPiece(missed)
			if err != nil {
				return nil, nil, err
			}
			ids = append(ids, missedIDs...)
			tokens = append(tokens, missedTokens...)
		}

		match := input[matchStart:runeIndexToByteOffset(input, m.Index+m.Length)]
		matchIDs, matchTokens, err := t.encodeTextPiece(match)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, matchIDs...)
		tokens = append(tokens, matchTokens...)

		lastRune = m.Index + m.Length
		m, err = t.splitRe.FindNextMatch(m)
		if err != nil {
			return nil, nil, err
		}
	}

	totalRunes := utf8.RuneCountInString(input)
	if lastRune < totalRunes {
		tail := input[runeIndexToByteOffset(input, lastRune):]
		tailIDs, tailTokens, err := t.encodeTextPiece(tail)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, tailIDs...)
		tokens = append(tokens, tailTokens...)
	}

	return ids, tokens, nil
}

func (t *qwenTokenizer) encodeTextPiece(piece string) ([]int, []string, error) {
	if piece == "" {
		return nil, nil, nil
	}
	if cached, ok := t.cache[piece]; ok {
		return append([]int(nil), cached...), idsToTokens(cached, t.idToToken), nil
	}

	encoded := encodeBytes(piece, t.byteEncoder)
	symbols := splitRunes(encoded)
	merged := bpeMerge(symbols, t.bpeRanks)

	ids := make([]int, 0, len(merged))
	for _, token := range merged {
		id, ok := t.vocab[token]
		if !ok {
			return nil, nil, fmt.Errorf("token %q not in vocab", token)
		}
		ids = append(ids, id)
	}
	t.cache[piece] = append([]int(nil), ids...)
	return ids, merged, nil
}

func mergePair(raw any) ([2]string, error) {
	switch pair := raw.(type) {
	case []any:
		if len(pair) != 2 {
			return [2]string{}, fmt.Errorf("expected pair len 2, got %d", len(pair))
		}
		left, ok := pair[0].(string)
		if !ok {
			return [2]string{}, fmt.Errorf("left merge token is %T", pair[0])
		}
		right, ok := pair[1].(string)
		if !ok {
			return [2]string{}, fmt.Errorf("right merge token is %T", pair[1])
		}
		return [2]string{left, right}, nil
	default:
		return [2]string{}, fmt.Errorf("merge pair type %T", raw)
	}
}

func pairKey(left, right string) string {
	return left + "\x00" + right
}

func bpeMerge(symbols []string, ranks map[string]int) []string {
	if len(symbols) < 2 {
		return symbols
	}

	for {
		bestIdx := -1
		bestRank := int(^uint(0) >> 1)
		for i := 0; i < len(symbols)-1; i++ {
			rank, ok := ranks[pairKey(symbols[i], symbols[i+1])]
			if ok && rank < bestRank {
				bestIdx = i
				bestRank = rank
			}
		}
		if bestIdx < 0 {
			return symbols
		}

		merged := make([]string, 0, len(symbols)-1)
		merged = append(merged, symbols[:bestIdx]...)
		merged = append(merged, symbols[bestIdx]+symbols[bestIdx+1])
		merged = append(merged, symbols[bestIdx+2:]...)
		symbols = merged
	}
}

func splitRunes(s string) []string {
	out := make([]string, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func buildByteTables() ([256]string, map[rune]byte) {
	var bs []int
	for i := int('!'); i <= int('~'); i++ {
		bs = append(bs, i)
	}
	for i := int(0xA1); i <= int(0xAC); i++ {
		bs = append(bs, i)
	}
	for i := int(0xAE); i <= int(0xFF); i++ {
		bs = append(bs, i)
	}

	cs := append([]int(nil), bs...)
	extra := 0
	for b := 0; b < 256; b++ {
		found := false
		for _, keep := range bs {
			if b == keep {
				found = true
				break
			}
		}
		if !found {
			bs = append(bs, b)
			cs = append(cs, 256+extra)
			extra++
		}
	}

	var encoder [256]string
	decoder := make(map[rune]byte, 256)
	for i, b := range bs {
		r := rune(cs[i])
		encoder[b] = string(r)
		decoder[r] = byte(b)
	}
	return encoder, decoder
}

func encodeBytes(s string, encoder [256]string) string {
	data := []byte(s)
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, bt := range data {
		b.WriteString(encoder[bt])
	}
	return b.String()
}

func decodeBytes(s string, decoder map[rune]byte) (string, error) {
	buf := make([]byte, 0, len(s))
	for _, r := range s {
		bt, ok := decoder[r]
		if !ok {
			if r < utf8.RuneSelf {
				buf = append(buf, byte(r))
				continue
			}
			return "", fmt.Errorf("missing byte decoder for rune %q", r)
		}
		buf = append(buf, bt)
	}
	return string(buf), nil
}

func idsToTokens(ids []int, idToToken map[int]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, idToToken[id])
	}
	return out
}

func runeIndexToByteOffset(s string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	if runeIndex >= utf8.RuneCountInString(s) {
		return len(s)
	}
	byteOffset := 0
	for i := 0; i < runeIndex && byteOffset < len(s); i++ {
		_, size := utf8.DecodeRuneInString(s[byteOffset:])
		byteOffset += size
	}
	return byteOffset
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

func ioNames(infos []ortgo.InputOutputInfo) []string {
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Name)
	}
	return out
}

func printIO(label string, infos []ortgo.InputOutputInfo) {
	fmt.Println(label + ":")
	for _, info := range infos {
		fmt.Printf("  - %s | type=%s | elem=%s | shape=%v\n", info.Name, info.OrtValueType, info.DataType, []int64(info.Dimensions))
	}
}

func findExact(infos []ortgo.InputOutputInfo, want string) string {
	for _, info := range infos {
		if info.Name == want {
			return info.Name
		}
	}
	return ""
}

func findFirstContaining(infos []ortgo.InputOutputInfo, needle string) string {
	for _, info := range infos {
		if strings.Contains(info.Name, needle) {
			return info.Name
		}
	}
	return ""
}

func indexOfName(infos []ortgo.InputOutputInfo, want string) int {
	for i, info := range infos {
		if info.Name == want {
			return i
		}
	}
	return -1
}

func indexOfString(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

func embeddingsSeqLen(v ortgo.Value) int64 {
	tensor, ok := v.(*ortgo.Tensor[float32])
	if !ok {
		return 0
	}
	shape := tensor.GetShape()
	if len(shape) < 2 {
		return 0
	}
	return shape[1]
}

func nextToken(v ortgo.Value) (int64, error) {
	tensor, ok := v.(*ortgo.Tensor[float32])
	if !ok {
		return 0, fmt.Errorf("unexpected logits type %T", v)
	}
	shape := tensor.GetShape()
	if len(shape) == 0 {
		return 0, fmt.Errorf("empty logits shape")
	}
	vocabSize := int(shape[len(shape)-1])
	logits := tensor.GetData()
	if vocabSize <= 0 || len(logits) < vocabSize {
		return 0, fmt.Errorf("invalid logits buffer size")
	}
	start := len(logits) - vocabSize
	best := 0
	bestVal := logits[start]
	for i := 1; i < vocabSize; i++ {
		if logits[start+i] > bestVal {
			best = i
			bestVal = logits[start+i]
		}
	}
	return int64(best), nil
}

func eosTokens(cfg *qwenConfig, genCfg *generationConfig) map[int64]bool {
	out := map[int64]bool{
		eosToken1: true,
		eosToken2: true,
	}
	for _, src := range []any{cfg.TextConfig.EOSTokenID, genCfg.EOSTokenID} {
		for _, id := range asInt64Slice(src) {
			out[id] = true
		}
	}
	return out
}

func asInt64Slice(v any) []int64 {
	switch value := v.(type) {
	case float64:
		return []int64{int64(value)}
	case int:
		return []int64{int64(value)}
	case int64:
		return []int64{value}
	case []any:
		out := make([]int64, 0, len(value))
		for _, item := range value {
			out = append(out, asInt64Slice(item)...)
		}
		return out
	default:
		return nil
	}
}

func readRSSKiB() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmHWM:") {
			var value int64
			if _, err := fmt.Sscanf(line, "VmHWM:%d kB", &value); err == nil {
				return value
			}
		}
	}
	return 0
}

func destroyValues(values []ortgo.Value) {
	for i := range values {
		if values[i] != nil {
			values[i].Destroy()
			values[i] = nil
		}
	}
}

func destroyMaybe(v ortgo.Value) {
	if v != nil {
		v.Destroy()
	}
}

func ensureFile(url, path string) (string, error) {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		fmt.Printf("cached: %s\n", path)
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	fmt.Printf("download: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
