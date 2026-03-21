package kokoro

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/jikkuatwork/cattery/audio"
	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/speak"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	sampleRate   = 24000
	styleDim     = 256
	defaultLang  = "en-us"
	defaultSpeed = 1.0
)

// vocab is the Kokoro phoneme-to-token mapping.
var vocab = map[rune]int64{
	';': 1, ':': 2, ',': 3, '.': 4, '!': 5, '?': 6, '\u2014': 9, '\u2026': 10,
	'"': 11, '(': 12, ')': 13, '\u201c': 14, '\u201d': 15, ' ': 16,
	'ʣ': 18, 'ʥ': 19, 'ʦ': 20, 'ʨ': 21, 'ᵝ': 22,
	'A': 24, 'I': 25, 'O': 31, 'Q': 33, 'S': 35, 'T': 36, 'W': 39, 'Y': 41,
	'a': 43, 'b': 44, 'c': 45, 'd': 46, 'e': 47, 'f': 48, 'h': 50,
	'i': 51, 'j': 52, 'k': 53, 'l': 54, 'm': 55, 'n': 56, 'o': 57,
	'p': 58, 'q': 59, 'r': 60, 's': 61, 't': 62, 'u': 63, 'v': 64,
	'w': 65, 'x': 66, 'y': 67, 'z': 68,
	'ɑ': 69, 'ɐ': 70, 'ɒ': 71, 'æ': 72, 'β': 75, 'ɔ': 76, 'ɕ': 77,
	'ç': 78, 'ɖ': 80, 'ð': 81, 'ʤ': 82, 'ə': 83, 'ɚ': 85, 'ɛ': 86,
	'ɜ': 87, 'ɟ': 90, 'ɡ': 92, 'ɥ': 99, 'ɨ': 101, 'ɪ': 102, 'ʝ': 103,
	'ɯ': 110, 'ɰ': 111, 'ŋ': 112, 'ɳ': 113, 'ɲ': 114, 'ɴ': 115,
	'ø': 116, 'ɸ': 118, 'θ': 119, 'œ': 120, 'ɹ': 123, 'ɾ': 125,
	'ɻ': 126, 'ʁ': 128, 'ɽ': 129, 'ʂ': 130, 'ʃ': 131, 'ʈ': 132,
	'ʧ': 133, 'ʊ': 135, 'ʋ': 136, 'ʌ': 138, 'ɣ': 139, 'ɤ': 140,
	'χ': 142, 'ʎ': 143, 'ʒ': 147, 'ʔ': 148,
	'ˈ': 156, 'ˌ': 157, 'ː': 158, 'ʰ': 162, 'ʲ': 164,
	'↓': 169, '→': 171, '↗': 172, '↘': 173, 'ᵻ': 177,
}

var _ speak.Engine = (*Engine)(nil)

// Engine wraps a Kokoro ONNX Runtime session.
type Engine struct {
	session *ort.DynamicAdvancedSession
	model   *registry.Model
	dataDir string
}

// New creates a Kokoro engine by loading the ONNX model.
func New(modelPath string, dataDir string) (*Engine, error) {
	model, err := modelFromPath(dataDir, modelPath)
	if err != nil {
		return nil, err
	}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"input_ids", "style", "speed"},
		[]string{"waveform"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}

	return &Engine{
		session: session,
		model:   model,
		dataDir: dataDir,
	}, nil
}

// Voices returns the Kokoro voice catalog for a model.
func Voices(model *registry.Model) []speak.Voice {
	if model == nil {
		return nil
	}

	voices := make([]speak.Voice, len(model.Voices))
	for i := range model.Voices {
		voices[i] = speakVoice(&model.Voices[i])
	}
	return voices
}

// ResolveVoice resolves a user voice selection to a concrete Kokoro voice.
func ResolveVoice(model *registry.Model, voiceFlag, genderFilter string) (speak.Voice, error) {
	voice, _, err := resolveVoice(model, voiceFlag, genderFilter)
	return voice, err
}

// Voices returns the available Kokoro voices.
func (e *Engine) Voices() []speak.Voice {
	return Voices(e.model)
}

// Speak phonemizes the text, resolves a voice, runs Kokoro, and writes WAV.
func (e *Engine) Speak(w io.Writer, text string, opts speak.Options) error {
	if e == nil || e.session == nil {
		return fmt.Errorf("engine is closed")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text is empty")
	}

	speed := opts.Speed
	if speed == 0 {
		speed = defaultSpeed
	}
	if speed < 0.5 || speed > 2.0 {
		return fmt.Errorf("speed must be between 0.5 and 2.0")
	}

	lang := strings.TrimSpace(opts.Lang)
	if lang == "" {
		lang = defaultLang
	}

	voice, asset, err := resolveVoice(e.model, opts.Voice, opts.Gender)
	if err != nil {
		return err
	}

	result, err := download.Ensure(e.dataDir, e.model, asset)
	if err != nil {
		return fmt.Errorf("download voice %q: %w", voice.Name, err)
	}

	p := &phonemize.EspeakPhonemizer{Voice: lang}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		return fmt.Errorf("phonemize: %w", err)
	}

	tokens := tokenize(phonemes)
	if len(tokens) == 0 {
		return fmt.Errorf("no speakable content in text")
	}

	stylePath := result.Files[asset.File.Filename]
	style, err := loadVoice(stylePath, len(tokens), styleDimFor(e.model))
	if err != nil {
		return fmt.Errorf("load voice: %w", err)
	}

	samples, err := e.synthesize(tokens, style, float32(speed))
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}

	if err := audio.WriteWAV(w, samples, sampleRateFor(e.model)); err != nil {
		return fmt.Errorf("write wav: %w", err)
	}

	return nil
}

// Close releases the ONNX session.
func (e *Engine) Close() error {
	if e != nil && e.session != nil {
		e.session.Destroy()
		e.session = nil
	}
	return nil
}

func (e *Engine) synthesize(tokens []int64, style []float32, speed float32) ([]float32, error) {
	padded := make([]int64, len(tokens)+2)
	copy(padded[1:], tokens)
	seqLen := int64(len(padded))

	tokenTensor, err := ort.NewTensor(ort.NewShape(1, seqLen), padded)
	if err != nil {
		return nil, fmt.Errorf("create token tensor: %w", err)
	}
	defer tokenTensor.Destroy()

	styleTensor, err := ort.NewTensor(ort.NewShape(1, int64(styleDimFor(e.model))), style)
	if err != nil {
		return nil, fmt.Errorf("create style tensor: %w", err)
	}
	defer styleTensor.Destroy()

	speedTensor, err := ort.NewTensor(ort.NewShape(1), []float32{speed})
	if err != nil {
		return nil, fmt.Errorf("create speed tensor: %w", err)
	}
	defer speedTensor.Destroy()

	outputs := []ort.Value{nil}
	err = e.session.Run(
		[]ort.Value{tokenTensor, styleTensor, speedTensor},
		outputs,
	)
	if err != nil {
		return nil, fmt.Errorf("inference: %w", err)
	}
	defer outputs[0].Destroy()

	audioTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unexpected output type")
	}

	src := audioTensor.GetData()
	samples := make([]float32, len(src))
	copy(samples, src)
	return samples, nil
}

func tokenize(phonemes string) []int64 {
	var tokens []int64
	for _, r := range phonemes {
		if id, ok := vocab[r]; ok {
			tokens = append(tokens, id)
		}
	}
	return tokens
}

func loadVoice(path string, numTokens, styleDim int) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	numEntries := int(info.Size()/4) / styleDim
	if numEntries == 0 {
		return nil, fmt.Errorf("voice file too small: %d bytes", info.Size())
	}

	idx := numTokens
	if idx >= numEntries {
		idx = numEntries - 1
	}

	offset := int64(idx) * int64(styleDim) * 4
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("seek to entry %d: %w", idx, err)
	}

	style := make([]float32, styleDim)
	if err := binary.Read(f, binary.LittleEndian, style); err != nil {
		return nil, fmt.Errorf("read style vector: %w", err)
	}

	for i, v := range style {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("bad float at position %d: %v", i, v)
		}
	}

	return style, nil
}

func resolveVoice(model *registry.Model, voiceFlag, genderFilter string) (speak.Voice, *registry.Voice, error) {
	if model == nil {
		return speak.Voice{}, nil, fmt.Errorf("missing model metadata")
	}

	if voiceFlag != "" {
		asset := model.GetVoice(voiceFlag)
		if asset == nil {
			return speak.Voice{}, nil, fmt.Errorf("unknown voice %q", voiceFlag)
		}
		return speakVoice(asset), asset, nil
	}

	candidates := make([]int, 0, len(model.Voices))
	for i := range model.Voices {
		if genderFilter == "" || model.Voices[i].Gender == genderFilter {
			candidates = append(candidates, i)
		}
	}

	if genderFilter != "" && genderFilter != "male" && genderFilter != "female" {
		return speak.Voice{}, nil, fmt.Errorf("gender must be \"male\" or \"female\"")
	}
	if len(candidates) == 0 {
		return speak.Voice{}, nil, fmt.Errorf("no %s voices available", genderFilter)
	}

	asset := &model.Voices[candidates[rand.Intn(len(candidates))]]
	return speakVoice(asset), asset, nil
}

func speakVoice(v *registry.Voice) speak.Voice {
	return speak.Voice{
		ID:          v.ID,
		Name:        v.Name,
		Gender:      v.Gender,
		Accent:      v.Accent,
		Description: v.Description,
	}
}

func sampleRateFor(model *registry.Model) int {
	return model.MetaInt("sample_rate", sampleRate)
}

func styleDimFor(model *registry.Model) int {
	return model.MetaInt("style_dim", styleDim)
}

func modelFromPath(dataDir, modelPath string) (*registry.Model, error) {
	cleanModelPath := filepath.Clean(modelPath)
	for _, model := range registry.GetByKind(registry.KindTTS) {
		file := model.PrimaryFile()
		if file == nil {
			continue
		}
		want := filepath.Clean(paths.ModelFile(dataDir, model.ID, file.Filename))
		if want == cleanModelPath {
			return model, nil
		}
	}
	return nil, fmt.Errorf("resolve model from path %q", modelPath)
}
