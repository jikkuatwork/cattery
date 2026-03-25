//go:build memtest

// Package memtest contains RSS-based memory regression tests for issue #27.
//
// These tests require downloaded model files (~/.cattery/) and are excluded
// from normal `go test ./...` runs. To execute:
//
//	go test -tags memtest ./memtest/ -v
//
// Each test measures peak RSS during synthesis or transcription and asserts it
// stays within calibrated thresholds from the observed 2026-03-25 runs:
//
//	TTS short 479 MB  →  threshold 623 MB
//	TTS long  888 MB  →  threshold 1155 MB
//	STT short 274 MB  →  threshold 357 MB
//	STT long  449 MB  →  threshold 584 MB
//
// Short-run thresholds catch obvious streaming regressions. Long-run thresholds
// allow for the observed ONNX Runtime/session retention while still protecting
// against unbounded accumulation.
package memtest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jikkuatwork/cattery/listen"
	"github.com/jikkuatwork/cattery/listen/moonshine"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/speak"
	"github.com/jikkuatwork/cattery/speak/kokoro"
)

const (
	// ttsPeakRSSThresholdMB is 1.3× the observed 479 MB TTS short baseline.
	ttsPeakRSSThresholdMB int64 = 623
	// ttsPeakRSSLongThresholdMB is 1.3× the observed 888 MB TTS long baseline.
	ttsPeakRSSLongThresholdMB int64 = 1155
	// sttPeakRSSThresholdMB is 1.3× the observed 274 MB STT short baseline.
	sttPeakRSSThresholdMB int64 = 357
	// sttPeakRSSLongThresholdMB is 1.3× the observed 449 MB STT long baseline.
	sttPeakRSSLongThresholdMB int64 = 584

	sttSampleRate = 16000
)

// shortText is ~50 words — produces a single TTS chunk (<30s of audio).
const shortText = "The quick brown fox jumps over the lazy dog. " +
	"This sentence tests phoneme coverage across many consonants and vowels. " +
	"Speech synthesis should handle punctuation, rhythm, and prosody cleanly."

// longText is ~400 words — forces multiple TTS chunks (~3 min of audio).
// With correct streaming, peak RSS should be the same as for shortText.
const longText = "The history of computing spans several centuries of ingenuity and practical invention. " +
	"Long before electronic machines existed, people devised mechanical devices to assist with calculation and record-keeping. " +
	"The abacus, astrolabe, and mechanical clock all represent early attempts to encode mathematical operations in physical form. " +
	"By the seventeenth century, philosophers like Leibniz and Pascal had designed calculating machines that could add, subtract, and sometimes multiply. " +
	"These devices were expensive, fragile, and unreliable, but they planted the seed of an idea: " +
	"that arithmetic could be automated and need not depend on human attention for every step. " +
	"Charles Babbage spent decades designing his Analytical Engine, a general-purpose mechanical computer that was never fully built during his lifetime. " +
	"Ada Lovelace, working from Babbage's notes, wrote what many consider the first algorithm intended for machine execution. " +
	"Her insight was that the machine could manipulate symbols beyond numbers, opening the door to a much broader conception of computing. " +
	"The twentieth century brought electricity into the picture, enabling machines of unprecedented speed and reliability. " +
	"Vacuum tubes, then transistors, then integrated circuits compressed entire computing systems onto smaller and smaller substrates. " +
	"Each generation of hardware brought new programming languages, operating systems, and application domains. " +
	"The personal computer revolution of the nineteen eighties made computing accessible to individuals rather than institutions. " +
	"The internet then connected those individuals into a global network of shared knowledge and communication. " +
	"Today, machine learning systems trained on vast datasets can recognize images, translate languages, and generate coherent text. " +
	"The boundary between human and machine cognition has become a subject of active research and philosophical debate. " +
	"What remains constant across all these transformations is the fundamental idea: " +
	"that problems can be decomposed into steps, steps can be encoded as instructions, " +
	"and instructions can be executed reliably and repeatedly by machines."

func TestMain(m *testing.M) {
	ortLib := findORTLib()
	if ortLib == "" {
		fmt.Fprintln(os.Stderr, "memtest: ORT library not found in ~/.cattery/ort — run 'cattery download' first; skipping all tests")
		os.Exit(0)
	}
	if err := ort.Init(ortLib); err != nil {
		fmt.Fprintf(os.Stderr, "memtest: ort.Init: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	ort.Shutdown()
	os.Exit(code)
}

// --- TTS tests ---

// TestTTSPeakRSS_Short verifies that a single-chunk TTS run stays within the
// memory threshold. This is the baseline — multi-chunk must not exceed it.
func TestTTSPeakRSS_Short(t *testing.T) {
	drainMemory(t)
	peak := runTTS(t, shortText)
	t.Logf("TTS short: peak RSS %d MB (threshold %d MB, %d chars)", peak, ttsPeakRSSThresholdMB, len(shortText))
	assertRSS(t, "TTS short", peak, ttsPeakRSSThresholdMB)
}

// TestTTSPeakRSS_Long verifies that multi-chunk TTS stays within the calibrated
// long-run bound and does not regress into unbounded accumulation.
func TestTTSPeakRSS_Long(t *testing.T) {
	drainMemory(t)
	shortPeak := runTTS(t, shortText)
	drainMemory(t)
	peak := runTTS(t, longText)
	t.Logf("TTS long:  peak RSS %d MB (threshold %d MB, %d chars)", peak, ttsPeakRSSLongThresholdMB, len(longText))
	if shortPeak > 0 {
		ratio := float64(peak) / float64(shortPeak)
		t.Logf("TTS long/short RSS ratio: %.2fx (%d MB / %d MB)", ratio, peak, shortPeak)
	} else {
		t.Logf("TTS long/short RSS ratio: unavailable (short baseline measured 0 MB)")
	}
	assertRSS(t, "TTS long", peak, ttsPeakRSSLongThresholdMB)
}

// --- STT tests ---

// TestSTTPeakRSS_Short verifies that transcribing a short clip stays within
// the memory threshold. This is the baseline for the STT path.
func TestSTTPeakRSS_Short(t *testing.T) {
	drainMemory(t)
	peak := runSTT(t, 25) // 25s — single chunk
	t.Logf("STT short: peak RSS %d MB (threshold %d MB, 25s audio)", peak, sttPeakRSSThresholdMB)
	assertRSS(t, "STT short", peak, sttPeakRSSThresholdMB)
}

// TestSTTPeakRSS_Long verifies that streaming STT input does not accumulate
// the full audio in memory. Without streaming, 3 min of float32 PCM is ~35 MB
// on top of the model footprint — noticeable but not catastrophic alone.
// More importantly it validates that the sliding-window path runs correctly.
func TestSTTPeakRSS_Long(t *testing.T) {
	drainMemory(t)
	shortPeak := runSTT(t, 25)
	drainMemory(t)
	peak := runSTT(t, 180) // 180s = 3 min — multiple chunks
	t.Logf("STT long:  peak RSS %d MB (threshold %d MB, 180s audio)", peak, sttPeakRSSLongThresholdMB)
	if shortPeak > 0 {
		ratio := float64(peak) / float64(shortPeak)
		t.Logf("STT long/short RSS ratio: %.2fx (%d MB / %d MB)", ratio, peak, shortPeak)
	}
	assertRSS(t, "STT long", peak, sttPeakRSSLongThresholdMB)
}

// --- helpers ---

func runTTS(t *testing.T, text string) int64 {
	t.Helper()

	model := registry.Default(registry.KindTTS)
	if model == nil {
		t.Fatal("no TTS model in registry")
	}

	dataDir := paths.DataDir()
	modelFile := model.PrimaryFile()
	if modelFile == nil {
		t.Fatal("TTS model has no primary file")
	}
	modelPath := paths.ModelFile(dataDir, model.ID, modelFile.Filename)
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("TTS model not found at %s — run 'cattery download'", modelPath)
	}

	eng, err := kokoro.New(modelPath, dataDir)
	if err != nil {
		t.Fatalf("kokoro.New: %v", err)
	}
	defer eng.Close()

	voiceID := firstAvailableVoice(t, eng.Voices(), dataDir, model.ID)

	stop, peakMB := startRSSPoller()
	defer stop()

	wavOut, err := os.CreateTemp(t.TempDir(), "memtest-*.wav")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer wavOut.Close()

	if err := eng.Speak(wavOut, text, speak.Options{
		Voice: voiceID,
		Speed: 1.0,
	}); err != nil {
		t.Fatalf("Speak: %v", err)
	}

	return peakMB()
}

func runSTT(t *testing.T, durationSec int) int64 {
	t.Helper()

	model := registry.Default(registry.KindSTT)
	if model == nil {
		t.Fatal("no STT model in registry")
	}

	dataDir := paths.DataDir()
	modelDir := paths.ModelDir(dataDir, model.ID)
	if _, err := os.Stat(modelDir); err != nil {
		t.Skipf("STT model not found at %s — run 'cattery download'", modelDir)
	}

	eng, err := moonshine.New(modelDir, model.Meta)
	if err != nil {
		t.Fatalf("moonshine.New: %v", err)
	}
	defer eng.Close()

	audio := syntheticWAV(durationSec, sttSampleRate)

	stop, peakMB := startRSSPoller()
	t.Cleanup(stop)

	if _, err := eng.Transcribe(audio, listen.Options{}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	return peakMB()
}

func assertRSS(t *testing.T, label string, peak, threshold int64) {
	t.Helper()
	if peak > threshold {
		t.Errorf("%s: peak RSS %d MB exceeds threshold %d MB — possible memory accumulation regression",
			label, peak, threshold)
	}
}

func drainMemory(t *testing.T) {
	t.Helper()
	runtime.GC()
	ort.Drain()
}

// firstAvailableVoice returns the ID of the first voice whose .bin file exists.
func firstAvailableVoice(t *testing.T, voices []speak.Voice, dataDir, modelID string) string {
	t.Helper()
	for _, v := range voices {
		vf := paths.VoiceFile(dataDir, modelID, v.ID)
		if _, err := os.Stat(vf); err == nil {
			return v.ID
		}
	}
	t.Skip("no voice files found in ~/.cattery — run 'cattery download'")
	return ""
}

// startRSSPoller starts a background goroutine that samples /proc/self/status
// every 50ms and tracks peak VmRSS. Returns a stop func (safe to call multiple
// times) and a peakMB func that returns the highest RSS seen so far.
func startRSSPoller() (stop func(), peakMB func() int64) {
	var peak atomic.Int64
	done := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if rss := currentRSSMB(); rss > peak.Load() {
					peak.Store(rss)
				}
			}
		}
	}()

	stop = func() { once.Do(func() { close(done) }) }
	peakMB = func() int64 { return peak.Load() }
	return
}

// currentRSSMB reads VmRSS from /proc/self/status and returns it in MB.
func currentRSSMB() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb / 1024
			}
		}
	}
	return 0
}

// syntheticWAV returns a reader for a PCM16 mono WAV at the given sample rate
// and duration. The content is a 440 Hz sine wave — non-silent so it exercises
// the chunking path without triggering silence-skip logic.
func syntheticWAV(durationSec, sampleRate int) io.Reader {
	nSamples := durationSec * sampleRate
	dataBytes := nSamples * 2 // int16 per sample

	var hdr bytes.Buffer
	hdr.WriteString("RIFF")
	binary.Write(&hdr, binary.LittleEndian, uint32(36+dataBytes))
	hdr.WriteString("WAVE")
	hdr.WriteString("fmt ")
	binary.Write(&hdr, binary.LittleEndian, uint32(16))           // fmt chunk size
	binary.Write(&hdr, binary.LittleEndian, uint16(1))            // PCM
	binary.Write(&hdr, binary.LittleEndian, uint16(1))            // mono
	binary.Write(&hdr, binary.LittleEndian, uint32(sampleRate))   // sample rate
	binary.Write(&hdr, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	binary.Write(&hdr, binary.LittleEndian, uint16(2))            // block align
	binary.Write(&hdr, binary.LittleEndian, uint16(16))           // bits per sample
	hdr.WriteString("data")
	binary.Write(&hdr, binary.LittleEndian, uint32(dataBytes))

	pr, pw := io.Pipe()
	go func() {
		pw.Write(hdr.Bytes())
		buf := make([]byte, 8192)
		written := 0
		sampleIdx := 0
		for written < dataBytes {
			n := len(buf)
			if rem := dataBytes - written; rem < n {
				n = rem
			}
			for i := 0; i+1 < n; i += 2 {
				t := float64(sampleIdx) / float64(sampleRate)
				s := math.Sin(2 * math.Pi * 440 * t)
				v := int16(s * 16384)
				buf[i] = byte(v)
				buf[i+1] = byte(uint16(v) >> 8)
				sampleIdx++
			}
			pw.Write(buf[:n])
			written += n
		}
		pw.Close()
	}()
	return pr
}

// findORTLib returns the path to the ORT shared library in ~/.cattery/ort/,
// or empty string if not found.
func findORTLib() string {
	dir := paths.ORTLib(paths.DataDir())
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".so") || strings.HasSuffix(name, ".dylib") {
			return filepath.Join(dir, name)
		}
	}
	return ""
}
