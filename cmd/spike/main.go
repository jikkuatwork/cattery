// Spike: prove ONNX Runtime can run Kokoro TTS model from Go.
//
// Prerequisites (in models-data/):
//   - onnx/model_quantized.onnx  (from onnx-community/Kokoro-82M-v1.0-ONNX)
//   - voices/af_heart.bin         (same repo)
//   - libonnxruntime.so.X.Y.Z     (from github.com/microsoft/onnxruntime/releases)
//
// Run: go run ./cmd/spike
package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kodeman/cattery/audio"
	"github.com/kodeman/cattery/phonemize"
	ort "github.com/yalue/onnxruntime_go"
)

// Vocab: phoneme-to-token mapping from Kokoro's config.json.
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

const (
	sampleRate = 24000
	styleDim   = 256
	maxTokens  = 510 // voice file has 510 entries
	modelFile  = "models-data/onnx/model_quantized.onnx"
	voiceFile  = "models-data/voices/af_heart.bin"
	outputFile = "output.wav"
)

func main() {
	libPath := findOrtLib()
	if libPath == "" {
		log.Fatal("Cannot find libonnxruntime shared library in models-data/")
	}
	fmt.Println("ORT library:", libPath)

	ort.SetSharedLibraryPath(libPath)
	if err := ort.InitializeEnvironment(); err != nil {
		log.Fatal("InitializeEnvironment:", err)
	}
	defer ort.DestroyEnvironment()

	// Text -> phonemes -> tokens
	text := "Hello, world. This is a test of the Cattery text to speech system."
	if len(os.Args) > 1 {
		text = os.Args[1]
	}

	p := &phonemize.EspeakPhonemizer{Voice: "en-us"}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		log.Fatal("Phonemize: ", err)
	}
	tokens := tokenize(phonemes)
	fmt.Printf("Text: %s\nPhonemes: %s\nTokens (%d): %v\n", text, phonemes, len(tokens), tokens)

	// Load voice style vector — select row indexed by token count
	style, err := loadVoice(voiceFile, len(tokens))
	if err != nil {
		log.Fatal("loadVoice: ", err)
	}
	fmt.Printf("Voice style: %d floats, first few: [%.4f, %.4f, %.4f]\n",
		len(style), style[0], style[1], style[2])

	// Pad tokens with 0 at start and end (model expects this)
	padded := make([]int64, len(tokens)+2)
	copy(padded[1:], tokens)
	seqLen := int64(len(padded))

	// Create input tensors
	tokenTensor, err := ort.NewTensor(ort.NewShape(1, seqLen), padded)
	if err != nil {
		log.Fatal("NewTensor(tokens): ", err)
	}
	defer tokenTensor.Destroy()

	styleTensor, err := ort.NewTensor(ort.NewShape(1, int64(styleDim)), style)
	if err != nil {
		log.Fatal("NewTensor(style): ", err)
	}
	defer styleTensor.Destroy()

	speedTensor, err := ort.NewTensor(ort.NewShape(1), []float32{1.0})
	if err != nil {
		log.Fatal("NewTensor(speed): ", err)
	}
	defer speedTensor.Destroy()

	// Create session
	session, err := ort.NewDynamicAdvancedSession(
		modelFile,
		[]string{"input_ids", "style", "speed"},
		[]string{"waveform"},
		nil,
	)
	if err != nil {
		log.Fatal("NewDynamicAdvancedSession: ", err)
	}
	defer session.Destroy()

	// Run inference (nil output = auto-allocate)
	fmt.Println("Running inference...")
	outputs := []ort.Value{nil}
	err = session.Run(
		[]ort.Value{tokenTensor, styleTensor, speedTensor},
		outputs,
	)
	if err != nil {
		log.Fatal("Run: ", err)
	}
	defer outputs[0].Destroy()

	audioTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		log.Fatal("Output is not float32 tensor")
	}
	samples := audioTensor.GetData()
	fmt.Printf("Got %d samples (%.2fs at %dHz)\n",
		len(samples), float64(len(samples))/float64(sampleRate), sampleRate)

	// Write WAV
	f, err := os.Create(outputFile)
	if err != nil {
		log.Fatal("Create: ", err)
	}
	defer f.Close()

	if err := audio.WriteWAV(f, samples, sampleRate); err != nil {
		log.Fatal("WriteWAV: ", err)
	}
	fmt.Println("Written to", outputFile)
}

// tokenize converts IPA phoneme string to Kokoro token IDs.
func tokenize(phonemes string) []int64 {
	var tokens []int64
	for _, r := range phonemes {
		if id, ok := vocab[r]; ok {
			tokens = append(tokens, id)
		}
	}
	return tokens
}

// loadVoice reads a raw float32 voice .bin file [510, 1, 256] and returns
// the style vector at index min(numTokens, 509) as a flat [256]float32.
func loadVoice(path string, numTokens int) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	numFloats := info.Size() / 4
	numEntries := int(numFloats) / styleDim
	if numEntries == 0 {
		return nil, fmt.Errorf("voice file too small: %d bytes", info.Size())
	}

	// Select style index based on token count (clamped)
	idx := numTokens
	if idx >= numEntries {
		idx = numEntries - 1
	}

	// Seek to the right position: idx * 256 * 4 bytes
	offset := int64(idx) * int64(styleDim) * 4
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("seek to entry %d: %w", idx, err)
	}

	style := make([]float32, styleDim)
	if err := binary.Read(f, binary.LittleEndian, style); err != nil {
		return nil, fmt.Errorf("read style vector: %w", err)
	}

	// Sanity check: values should be reasonable floats
	for i, v := range style {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("bad float at position %d: %v", i, v)
		}
	}

	return style, nil
}

// findOrtLib searches models-data/ for the onnxruntime shared library.
func findOrtLib() string {
	var pattern string
	switch runtime.GOOS {
	case "linux":
		pattern = "libonnxruntime.so*"
	case "darwin":
		pattern = "libonnxruntime*.dylib"
	case "windows":
		pattern = "onnxruntime.dll"
	}

	matches, _ := filepath.Glob(filepath.Join("models-data", pattern))
	if len(matches) == 0 {
		return ""
	}
	// Prefer the versioned .so file (not the symlink)
	for _, m := range matches {
		info, err := os.Lstat(m)
		if err == nil && info.Mode()&os.ModeSymlink == 0 {
			return m
		}
	}
	return matches[0]
}
