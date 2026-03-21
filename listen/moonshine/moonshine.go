package moonshine

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jikkuatwork/cattery/audio"
	"github.com/jikkuatwork/cattery/listen"
	ortgo "github.com/yalue/onnxruntime_go"
)

const (
	defaultSampleRate    = 16000
	defaultEncoderFile   = "encoder_model_quantized.onnx"
	defaultDecoderFile   = "decoder_model_merged_quantized.onnx"
	defaultTokenizerFile = "tokenizer.json"
	defaultNumLayers     = 6
	defaultNumHeads      = 8
	defaultHeadDim       = 36
	defaultMaxSteps      = 448
	defaultBOSToken      = 1
	defaultEOSToken      = 2
)

var _ listen.Engine = (*Engine)(nil)

// Engine runs Moonshine STT with separate encoder and decoder sessions.
type Engine struct {
	encoder    *ortgo.DynamicAdvancedSession
	decoder    *decoder
	tokenizer  map[int]string
	sampleRate int
}

type config struct {
	sampleRate    int
	encoderFile   string
	decoderFile   string
	tokenizerFile string
	numLayers     int
	numHeads      int
	headDim       int
	maxSteps      int
	bosToken      int64
	eosToken      int64
}

// New creates a Moonshine engine from a downloaded model dir and registry meta.
func New(modelDir string, meta map[string]string) (*Engine, error) {
	modelDir = strings.TrimSpace(modelDir)
	if modelDir == "" {
		return nil, fmt.Errorf("model dir is empty")
	}

	cfg := configFromMeta(meta)
	tokenizer, err := loadTokenizer(filepath.Join(modelDir, cfg.tokenizerFile))
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	encoder, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, cfg.encoderFile),
		[]string{"input_values"},
		[]string{"last_hidden_state"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load encoder: %w", err)
	}

	decInputs, decOutputs := decoderIONames(cfg.numLayers)
	decSession, err := ortgo.NewDynamicAdvancedSession(
		filepath.Join(modelDir, cfg.decoderFile),
		decInputs,
		decOutputs,
		nil,
	)
	if err != nil {
		encoder.Destroy()
		return nil, fmt.Errorf("load decoder: %w", err)
	}

	return &Engine{
		encoder: encoder,
		decoder: &decoder{
			session:   decSession,
			numLayers: cfg.numLayers,
			numHeads:  cfg.numHeads,
			headDim:   cfg.headDim,
			maxSteps:  cfg.maxSteps,
			bosToken:  cfg.bosToken,
			eosToken:  cfg.eosToken,
		},
		tokenizer:  tokenizer,
		sampleRate: cfg.sampleRate,
	}, nil
}

// Transcribe reads audio, resamples to the model rate, and returns text.
func (e *Engine) Transcribe(r io.Reader, opts listen.Options) (*listen.Result, error) {
	if e == nil || e.encoder == nil || e.decoder == nil {
		return nil, fmt.Errorf("engine is closed")
	}

	samples, sampleRate, err := audio.ReadPCM(r, e.sampleRate)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("audio is empty")
	}

	duration := float64(len(samples)) / float64(sampleRate)
	if sampleRate != e.sampleRate {
		samples = audio.Resample(samples, sampleRate, e.sampleRate)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("audio is empty")
	}

	lang := strings.TrimSpace(opts.Lang)
	if lang != "" && !strings.HasPrefix(strings.ToLower(lang), "en") {
		// Moonshine-tiny is English-only. Ignore the hint for now.
	}

	start := time.Now()

	audioTensor, err := ortgo.NewTensor(ortgo.NewShape(1, int64(len(samples))), samples)
	if err != nil {
		return nil, fmt.Errorf("create audio tensor: %w", err)
	}
	defer audioTensor.Destroy()

	outputs := []ortgo.Value{nil}
	if err := e.encoder.Run([]ortgo.Value{audioTensor}, outputs); err != nil {
		return nil, fmt.Errorf("run encoder: %w", err)
	}
	defer destroyValues(outputs)

	encTensor, ok := outputs[0].(*ortgo.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unexpected encoder output type %T", outputs[0])
	}

	encShape := encTensor.GetShape()
	if len(encShape) < 2 {
		return nil, fmt.Errorf("unexpected encoder output shape %v", encShape)
	}

	tokenIDs, err := e.decoder.decode(outputs[0], encShape[1])
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	elapsed := time.Since(start).Seconds()
	result := &listen.Result{
		Text:     decodeTokens(e.tokenizer, tokenIDs),
		Duration: duration,
		Elapsed:  elapsed,
	}
	if duration > 0 {
		result.RTF = elapsed / duration
	}
	return result, nil
}

// SampleRate returns the Moonshine input sample rate.
func (e *Engine) SampleRate() int {
	if e == nil {
		return 0
	}
	return e.sampleRate
}

// Close releases the ONNX sessions.
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	if e.decoder != nil && e.decoder.session != nil {
		e.decoder.session.Destroy()
		e.decoder.session = nil
	}
	if e.encoder != nil {
		e.encoder.Destroy()
		e.encoder = nil
	}
	e.decoder = nil
	e.tokenizer = nil
	return nil
}

func configFromMeta(meta map[string]string) config {
	return config{
		sampleRate:    metaInt(meta, "sample_rate", defaultSampleRate),
		encoderFile:   metaString(meta, "encoder_file", defaultEncoderFile),
		decoderFile:   metaString(meta, "decoder_file", defaultDecoderFile),
		tokenizerFile: metaString(meta, "tokenizer_file", defaultTokenizerFile),
		numLayers:     metaInt(meta, "num_layers", defaultNumLayers),
		numHeads:      metaInt(meta, "num_heads", defaultNumHeads),
		headDim:       metaInt(meta, "head_dim", defaultHeadDim),
		maxSteps:      metaInt(meta, "max_steps", defaultMaxSteps),
		bosToken:      int64(metaInt(meta, "bos_token", defaultBOSToken)),
		eosToken:      int64(metaInt(meta, "eos_token", defaultEOSToken)),
	}
}

func metaString(meta map[string]string, key, fallback string) string {
	if meta == nil {
		return fallback
	}
	value := strings.TrimSpace(meta[key])
	if value == "" {
		return fallback
	}
	return value
}

func metaInt(meta map[string]string, key string, fallback int) int {
	value := metaString(meta, key, "")
	if value == "" {
		return fallback
	}

	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		return fallback
	}
	return n
}
