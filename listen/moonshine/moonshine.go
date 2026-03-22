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

	lang := strings.TrimSpace(opts.Lang)
	if lang != "" && !strings.HasPrefix(strings.ToLower(lang), "en") {
		// Moonshine-tiny is English-only. Ignore the hint for now.
	}

	return transcribeStream(r, e.sampleRate, e.transcribePCM)
}

func (e *Engine) transcribePCM(samples []float32) (string, error) {
	audioTensor, err := ortgo.NewTensor(ortgo.NewShape(1, int64(len(samples))), samples)
	if err != nil {
		return "", fmt.Errorf("create audio tensor: %w", err)
	}
	defer audioTensor.Destroy()

	outputs := []ortgo.Value{nil}
	if err := e.encoder.Run([]ortgo.Value{audioTensor}, outputs); err != nil {
		return "", fmt.Errorf("run encoder: %w", err)
	}
	defer destroyValues(outputs)

	encTensor, ok := outputs[0].(*ortgo.Tensor[float32])
	if !ok {
		return "", fmt.Errorf("unexpected encoder output type %T", outputs[0])
	}

	encShape := encTensor.GetShape()
	if len(encShape) < 2 {
		return "", fmt.Errorf("unexpected encoder output shape %v", encShape)
	}

	tokenIDs, err := e.decoder.decode(outputs[0], encShape[1])
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	return decodeTokens(e.tokenizer, tokenIDs), nil
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

func transcribeStream(
	r io.Reader,
	sampleRate int,
	transcribe func([]float32) (string, error),
) (*listen.Result, error) {
	if transcribe == nil {
		return nil, fmt.Errorf("missing chunk transcriber")
	}

	stream, err := audio.NewPCMStreamReader(r, sampleRate)
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}

	sourceRate := stream.SampleRate()
	if sourceRate <= 0 {
		return nil, fmt.Errorf("read audio: source sample rate must be positive")
	}

	var resampler *audio.StreamResampler
	if sourceRate != sampleRate {
		resampler, err = audio.NewStreamResampler(sourceRate, sampleRate)
		if err != nil {
			return nil, err
		}
	}

	readBlock := maxInt(1, secondsToSamples(sourceRate, 1.0))
	windowCap := secondsToSamples(
		sampleRate,
		chunkTargetSeconds+chunkSearchWindowSeconds+chunkOverlapSeconds+1.0,
	)
	window := make([]float32, 0, maxInt(1, windowCap))
	texts := make([]string, 0, 4)

	totalSourceSamples := 0
	chunkIndex := 0
	eof := false

	start := time.Now()

	for {
		plan := planNextChunk(window, sampleRate, eof)
		if plan.needMore {
			block, readErr := stream.ReadSamples(readBlock)
			if readErr == io.EOF {
				eof = true
				if resampler != nil {
					window = append(window, resampler.Flush()...)
				}
				continue
			}
			if readErr != nil {
				return nil, fmt.Errorf("read audio: %w", readErr)
			}

			totalSourceSamples += len(block)
			if resampler != nil {
				window = append(window, resampler.Write(block)...)
			} else {
				window = append(window, block...)
			}
			continue
		}

		if len(window) == 0 {
			if eof {
				break
			}
			continue
		}

		chunkIndex++
		part := window[plan.chunk.start:plan.chunk.end]
		if !chunkIsSilent(part, sampleRate, plan.threshold) {
			text, err := transcribe(part)
			if err != nil {
				return nil, fmt.Errorf("transcribe chunk %d: %w", chunkIndex, err)
			}
			text = strings.TrimSpace(text)
			if text != "" {
				texts = append(texts, text)
			}
		}

		if plan.final {
			break
		}
		window = retainWindowTail(window, plan.nextStart)
	}

	duration := float64(totalSourceSamples) / float64(sourceRate)
	elapsed := time.Since(start).Seconds()
	result := &listen.Result{
		Text:     stitchChunkTexts(texts),
		Duration: duration,
		Elapsed:  elapsed,
	}
	if duration > 0 {
		result.RTF = elapsed / duration
	}
	return result, nil
}

func retainWindowTail(window []float32, start int) []float32 {
	if start <= 0 {
		return window
	}
	if start >= len(window) {
		return window[:0]
	}
	n := copy(window, window[start:])
	return window[:n]
}
