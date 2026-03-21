// Spike: prove ONNX Runtime can load and run Moonshine-tiny STT from Go.
//
// Downloads encoder + decoder ONNX files from HuggingFace on first run.
// Generates a test WAV via cattery TTS, then transcribes it back.
//
// Run: go run ./cmd/stt-spike
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/engine"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/registry"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	hfBase      = "https://huggingface.co/onnx-community/moonshine-tiny-ONNX/resolve/main/onnx"
	encoderFile = "encoder_model_quantized.onnx"
	decoderFile = "decoder_model_merged_quantized.onnx"

	sttSampleRate = 16000
	ttsSampleRate = 24000

	bosToken = 1
	eosToken = 2
	maxSteps = 448

	// Moonshine-tiny decoder: 6 layers, 8 heads, dim 36
	numLayers = 6
	numHeads  = 8
	headDim   = 36
)

var vocab map[int]string

// kvCache holds the past_key_values and present values for the decoder.
type kvCache struct {
	// Each layer has 4 tensors: decoder.key, decoder.value, encoder.key, encoder.value
	// Indexed as [layer][0=dec_k, 1=dec_v, 2=enc_k, 3=enc_v]
	tensors [numLayers][4]ort.Value
}

func (kv *kvCache) destroy() {
	for l := 0; l < numLayers; l++ {
		for i := 0; i < 4; i++ {
			if kv.tensors[l][i] != nil {
				kv.tensors[l][i].Destroy()
				kv.tensors[l][i] = nil
			}
		}
	}
}

func main() {
	dataDir := paths.DataDir()
	moonDir := filepath.Join(dataDir, "moonshine-tiny")

	fmt.Println("=== Moonshine STT Spike ===")
	fmt.Println("Data dir:", moonDir)

	// Step 1: Ensure ORT + TTS model available
	fmt.Println("\n--- Step 1: Ensure ORT + TTS available ---")
	model := registry.Get("kokoro-82m-v1.0")
	voice := model.GetVoice("af_heart")
	res, err := download.Ensure(dataDir, model, voice)
	if err != nil {
		log.Fatal("download.Ensure: ", err)
	}

	// Step 2: Download Moonshine ONNX files
	fmt.Println("\n--- Step 2: Download Moonshine ONNX ---")
	encPath := filepath.Join(moonDir, encoderFile)
	decPath := filepath.Join(moonDir, decoderFile)
	tokPath := filepath.Join(moonDir, "tokenizer.json")

	ensureFile(encPath, hfBase+"/"+encoderFile)
	ensureFile(decPath, hfBase+"/"+decoderFile)
	ensureFile(tokPath, "https://huggingface.co/onnx-community/moonshine-tiny-ONNX/resolve/main/tokenizer.json")

	// Step 3: Load tokenizer
	fmt.Println("\n--- Step 3: Load tokenizer ---")
	vocab, err = loadTokenizer(tokPath)
	if err != nil {
		log.Fatal("loadTokenizer: ", err)
	}
	fmt.Printf("Loaded %d vocab entries\n", len(vocab))

	// Step 4: Init ORT + load encoder
	fmt.Println("\n--- Step 4: Init ORT + load encoder ---")
	if err := engine.Init(res.ORTLib); err != nil {
		log.Fatal("engine.Init: ", err)
	}
	defer engine.Shutdown()

	t0 := time.Now()
	encSession, err := ort.NewDynamicAdvancedSession(
		encPath,
		[]string{"input_values"},
		[]string{"last_hidden_state"},
		nil,
	)
	if err != nil {
		log.Fatal("Load encoder: ", err)
	}
	defer encSession.Destroy()
	fmt.Printf("[OK] Encoder loaded in %v\n", time.Since(t0))

	// Step 5: Load decoder with all I/O names
	fmt.Println("\n--- Step 5: Load decoder ---")
	decInputNames, decOutputNames := decoderIONames()
	fmt.Printf("Decoder inputs: %d, outputs: %d\n", len(decInputNames), len(decOutputNames))

	t0 = time.Now()
	decSession, err := ort.NewDynamicAdvancedSession(
		decPath,
		decInputNames,
		decOutputNames,
		nil,
	)
	if err != nil {
		log.Fatal("Load decoder: ", err)
	}
	defer decSession.Destroy()
	fmt.Printf("[OK] Decoder loaded in %v\n", time.Since(t0))

	// Step 6: Generate test audio via TTS
	fmt.Println("\n--- Step 6: Generate test audio via TTS ---")
	testText := "The quick brown fox jumps over the lazy dog."
	samples := generateTestAudio(res, testText)
	fmt.Printf("Generated %d samples (%.2fs at %dHz)\n",
		len(samples), float64(len(samples))/float64(ttsSampleRate), ttsSampleRate)

	// Step 7: Resample 24kHz → 16kHz
	fmt.Println("\n--- Step 7: Resample 24→16kHz ---")
	resampled := resample(samples, ttsSampleRate, sttSampleRate)
	fmt.Printf("Resampled to %d samples (%.2fs at %dHz)\n",
		len(resampled), float64(len(resampled))/float64(sttSampleRate), sttSampleRate)

	// Step 8: Run encoder
	fmt.Println("\n--- Step 8: Run encoder ---")
	t0 = time.Now()
	audioTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(resampled))), resampled)
	if err != nil {
		log.Fatal("NewTensor(audio): ", err)
	}
	defer audioTensor.Destroy()

	encOutputs := []ort.Value{nil}
	if err := encSession.Run([]ort.Value{audioTensor}, encOutputs); err != nil {
		log.Fatal("Encoder run: ", err)
	}
	defer encOutputs[0].Destroy()
	fmt.Printf("[OK] Encoder: %v\n", time.Since(t0))

	encHidden, ok := encOutputs[0].(*ort.Tensor[float32])
	if !ok {
		log.Fatal("Encoder output is not float32")
	}
	encShape := encHidden.GetShape()
	fmt.Printf("Encoder output shape: %v\n", encShape)
	encSeqLen := encShape[1] // encoder sequence length

	// Step 9: Run decoder (autoregressive with KV cache)
	fmt.Println("\n--- Step 9: Decode (autoregressive) ---")
	t0 = time.Now()
	tokenIDs := transcribe(decSession, encOutputs[0], encSeqLen)
	fmt.Printf("[OK] Decoder: %v (%d tokens)\n", time.Since(t0), len(tokenIDs))

	// Step 10: Decode tokens
	text := decodeTokens(tokenIDs)
	fmt.Printf("\nInput:        %s\n", testText)
	fmt.Printf("Transcribed:  %s\n", text)

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("\n=== Memory ===")
	fmt.Printf("  HeapAlloc: %.2f MB\n", float64(m.HeapAlloc)/1024/1024)
	fmt.Printf("  Sys:       %.2f MB\n", float64(m.Sys)/1024/1024)
}

// decoderIONames returns the full list of input and output names for the merged decoder.
func decoderIONames() (inputs, outputs []string) {
	// Core inputs
	inputs = []string{"input_ids", "encoder_hidden_states"}

	// Past KV cache inputs: 6 layers × 4 tensors each
	for l := 0; l < numLayers; l++ {
		inputs = append(inputs,
			fmt.Sprintf("past_key_values.%d.decoder.key", l),
			fmt.Sprintf("past_key_values.%d.decoder.value", l),
			fmt.Sprintf("past_key_values.%d.encoder.key", l),
			fmt.Sprintf("past_key_values.%d.encoder.value", l),
		)
	}
	inputs = append(inputs, "use_cache_branch")

	// Outputs: logits + present KV cache
	outputs = []string{"logits"}
	for l := 0; l < numLayers; l++ {
		outputs = append(outputs,
			fmt.Sprintf("present.%d.decoder.key", l),
			fmt.Sprintf("present.%d.decoder.value", l),
			fmt.Sprintf("present.%d.encoder.key", l),
			fmt.Sprintf("present.%d.encoder.value", l),
		)
	}
	return
}

// transcribe runs autoregressive decoding with KV cache.
func transcribe(dec *ort.DynamicAdvancedSession, encHidden ort.Value, encSeqLen int64) []int64 {
	tokens := []int64{bosToken}

	var cache kvCache
	defer cache.destroy()

	for step := 0; step < maxSteps; step++ {
		var inputTokens []int64
		if step == 0 {
			inputTokens = tokens
		} else {
			inputTokens = []int64{tokens[len(tokens)-1]}
		}

		idsTensor, err := ort.NewTensor(ort.NewShape(1, int64(len(inputTokens))), inputTokens)
		if err != nil {
			log.Fatalf("step %d: create ids tensor: %v", step, err)
		}

		// Build KV cache tensors
		var kvInputs [numLayers][4]ort.Value
		var ownKV bool // whether we created zero tensors that need cleanup

		if step == 0 {
			// First pass: zero-sized KV cache for decoder, zero for encoder
			ownKV = true
			for l := 0; l < numLayers; l++ {
				kvInputs[l][0], _ = ort.NewTensor(ort.NewShape(1, int64(numHeads), 0, int64(headDim)), []float32{})
				kvInputs[l][1], _ = ort.NewTensor(ort.NewShape(1, int64(numHeads), 0, int64(headDim)), []float32{})
				kvInputs[l][2], _ = ort.NewTensor(ort.NewShape(1, int64(numHeads), 0, int64(headDim)), []float32{})
				kvInputs[l][3], _ = ort.NewTensor(ort.NewShape(1, int64(numHeads), 0, int64(headDim)), []float32{})
			}
		} else {
			// Subsequent: use cache from previous step
			ownKV = false
			kvInputs = cache.tensors
		}

		// use_cache_branch
		useCacheVal := step > 0
		useCacheTensor, _ := ort.NewTensor(ort.NewShape(1), []bool{useCacheVal})

		// Assemble inputs: input_ids, encoder_hidden_states, [kv pairs...], use_cache_branch
		inputs := make([]ort.Value, 0, 3+numLayers*4)
		inputs = append(inputs, idsTensor, encHidden)
		for l := 0; l < numLayers; l++ {
			inputs = append(inputs, kvInputs[l][0], kvInputs[l][1], kvInputs[l][2], kvInputs[l][3])
		}
		inputs = append(inputs, useCacheTensor)

		// Outputs: logits + present KV
		numOutputs := 1 + numLayers*4
		outputs := make([]ort.Value, numOutputs)

		err = dec.Run(inputs, outputs)

		// Cleanup input tensors we own
		idsTensor.Destroy()
		useCacheTensor.Destroy()
		if ownKV {
			for l := 0; l < numLayers; l++ {
				for i := 0; i < 4; i++ {
					kvInputs[l][i].Destroy()
				}
			}
		}

		if err != nil {
			fmt.Printf("[WARN] Decoder step %d failed: %v\n", step, err)
			break
		}

		// Extract logits
		logitsTensor, ok := outputs[0].(*ort.Tensor[float32])
		if !ok {
			fmt.Println("[WARN] Unexpected logits type")
			for _, o := range outputs {
				if o != nil {
					o.Destroy()
				}
			}
			break
		}

		logits := logitsTensor.GetData()
		shape := logitsTensor.GetShape()
		vocabSize := int(shape[len(shape)-1])
		lastPos := logits[len(logits)-vocabSize:]
		nextToken := argmax(lastPos)

		// Update cache from present outputs.
		// On step 0 (use_cache_branch=false): save both decoder and encoder cache.
		// On step 1+ (use_cache_branch=true): only update decoder cache;
		// the merged decoder outputs dummy encoder cache when use_cache=true.
		if step == 0 {
			cache.destroy()
			for l := 0; l < numLayers; l++ {
				cache.tensors[l][0] = outputs[1+l*4+0] // dec.key
				cache.tensors[l][1] = outputs[1+l*4+1] // dec.val
				cache.tensors[l][2] = outputs[1+l*4+2] // enc.key
				cache.tensors[l][3] = outputs[1+l*4+3] // enc.val
			}
		} else {
			// Update decoder KV, destroy old decoder KV + dummy encoder outputs
			for l := 0; l < numLayers; l++ {
				cache.tensors[l][0].Destroy()
				cache.tensors[l][1].Destroy()
				cache.tensors[l][0] = outputs[1+l*4+0] // dec.key
				cache.tensors[l][1] = outputs[1+l*4+1] // dec.val
				// Destroy dummy encoder outputs, keep existing encoder cache
				outputs[1+l*4+2].Destroy()
				outputs[1+l*4+3].Destroy()
			}
		}

		// Destroy logits (we've extracted what we need)
		outputs[0].Destroy()

		if int64(nextToken) == eosToken {
			break
		}
		tokens = append(tokens, int64(nextToken))

		if step < 5 || step%50 == 0 {
			fmt.Printf("  step %d: token %d = %q\n", step, nextToken, vocab[nextToken])
		}
	}

	return tokens[1:] // strip BOS
}

func argmax(data []float32) int {
	best := 0
	bestVal := data[0]
	for i := 1; i < len(data); i++ {
		if data[i] > bestVal {
			bestVal = data[i]
			best = i
		}
	}
	return best
}

func decodeTokens(ids []int64) string {
	var parts []string
	for _, id := range ids {
		if s, ok := vocab[int(id)]; ok {
			parts = append(parts, s)
		}
	}
	// Moonshine uses sentencepiece-style ▁ for word boundaries
	text := strings.Join(parts, "")
	text = strings.ReplaceAll(text, "▁", " ")
	return strings.TrimSpace(text)
}

func generateTestAudio(res *download.Result, text string) []float32 {
	eng, err := engine.New(res.ModelPath)
	if err != nil {
		log.Fatal("engine.New: ", err)
	}
	defer eng.Close()

	p := &phonemize.EspeakPhonemizer{Voice: "en-us"}
	phonemes, err := p.Phonemize(text)
	if err != nil {
		log.Fatal("Phonemize: ", err)
	}

	tokens := engine.Tokenize(phonemes)
	style, err := engine.LoadVoice(res.VoicePath, len(tokens))
	if err != nil {
		log.Fatal("LoadVoice: ", err)
	}

	samples, err := eng.Synthesize(tokens, style, 1.0)
	if err != nil {
		log.Fatal("Synthesize: ", err)
	}
	return samples
}

func resample(samples []float32, fromRate, toRate int) []float32 {
	if fromRate == toRate {
		return samples
	}
	ratio := float64(fromRate) / float64(toRate)
	outLen := int(float64(len(samples)) / ratio)
	out := make([]float32, outLen)
	for i := range out {
		srcIdx := float64(i) * ratio
		idx := int(srcIdx)
		frac := float32(srcIdx - float64(idx))
		if idx+1 < len(samples) {
			out[i] = samples[idx]*(1-frac) + samples[idx+1]*frac
		} else if idx < len(samples) {
			out[i] = samples[idx]
		}
	}
	return out
}

func loadTokenizer(path string) (map[int]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tok struct {
		Model struct {
			Vocab map[string]int `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}

	result := make(map[int]string, len(tok.Model.Vocab))
	for s, id := range tok.Model.Vocab {
		result[id] = s
	}
	return result, nil
}

func ensureFile(path, url string) {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  [exists] %s\n", filepath.Base(path))
		return
	}

	fmt.Printf("  [download] %s\n", filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Fatal("mkdir: ", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		log.Fatal("GET: ", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatalf("HTTP %d for %s", resp.StatusCode, url)
	}

	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		log.Fatal("create: ", err)
	}

	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmpPath)
		log.Fatal("download: ", err)
	}
	fmt.Printf("           %s (%s)\n", filepath.Base(path), formatBytes(n))

	if err := os.Rename(tmpPath, path); err != nil {
		log.Fatal("rename: ", err)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
