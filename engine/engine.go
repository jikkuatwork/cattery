// Package engine wraps ONNX Runtime inference for Kokoro TTS.
package engine

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	SampleRate = 24000
	StyleDim   = 256
)

// Vocab is the Kokoro phoneme-to-token mapping.
var Vocab = map[rune]int64{
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

// Engine holds an ONNX Runtime session for Kokoro TTS inference.
type Engine struct {
	session *ort.DynamicAdvancedSession
}

// New creates a new Engine by loading the ONNX model.
func New(modelPath string) (*Engine, error) {
	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"input_ids", "style", "speed"},
		[]string{"waveform"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	return &Engine{session: session}, nil
}

// Close releases the ONNX session.
func (e *Engine) Close() {
	if e.session != nil {
		e.session.Destroy()
	}
}

// Synthesize generates audio samples from phoneme tokens and a voice style vector.
func (e *Engine) Synthesize(tokens []int64, style []float32, speed float32) ([]float32, error) {
	// Pad tokens with 0 at start and end
	padded := make([]int64, len(tokens)+2)
	copy(padded[1:], tokens)
	seqLen := int64(len(padded))

	tokenTensor, err := ort.NewTensor(ort.NewShape(1, seqLen), padded)
	if err != nil {
		return nil, fmt.Errorf("create token tensor: %w", err)
	}
	defer tokenTensor.Destroy()

	styleTensor, err := ort.NewTensor(ort.NewShape(1, int64(StyleDim)), style)
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

	// Copy data out before tensor is destroyed
	src := audioTensor.GetData()
	samples := make([]float32, len(src))
	copy(samples, src)
	return samples, nil
}

// Tokenize converts an IPA phoneme string to Kokoro token IDs.
func Tokenize(phonemes string) []int64 {
	var tokens []int64
	for _, r := range phonemes {
		if id, ok := Vocab[r]; ok {
			tokens = append(tokens, id)
		}
	}
	return tokens
}

// LoadVoice reads a voice style vector from a raw float32 .bin file.
// The file has shape [510, 256]. Row is selected by token count.
func LoadVoice(path string, numTokens int) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	numEntries := int(info.Size()/4) / StyleDim
	if numEntries == 0 {
		return nil, fmt.Errorf("voice file too small: %d bytes", info.Size())
	}

	idx := numTokens
	if idx >= numEntries {
		idx = numEntries - 1
	}

	offset := int64(idx) * int64(StyleDim) * 4
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("seek to entry %d: %w", idx, err)
	}

	style := make([]float32, StyleDim)
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
