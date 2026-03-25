package moonshine

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/jikkuatwork/cattery/audio"
)

func TestTranscribeStreamShortAudioResamplesAndTracksDuration(t *testing.T) {
	sourceRate := 8000
	samples := make([]float32, secondsToSamples(sourceRate, 1.25))
	for i := range samples {
		samples[i] = 0.2
	}

	var wav bytes.Buffer
	if err := audio.WriteWAV(&wav, samples, sourceRate); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	calls := 0
	gotLen := 0
	result, err := transcribeStream(
		bytes.NewReader(wav.Bytes()),
		defaultSampleRate,
		func(chunk []float32) (string, error) {
			calls++
			gotLen = len(chunk)
			return "hello world", nil
		},
	)
	if err != nil {
		t.Fatalf("transcribeStream() error = %v", err)
	}

	wantLen := len(audio.Resample(samples, sourceRate, defaultSampleRate))
	if gotLen != wantLen {
		t.Fatalf("chunk len = %d, want %d", gotLen, wantLen)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if result.Text != "hello world" {
		t.Fatalf("text = %q, want %q", result.Text, "hello world")
	}
	if math.Abs(result.Duration-1.25) > 1e-6 {
		t.Fatalf("Duration = %v, want 1.25", result.Duration)
	}
}

func TestTranscribeStreamRetainsOverlapAndDedupsText(t *testing.T) {
	samples := constantPCM(35, 0.2)
	audioBytes := rawPCMTestBytes(samples)

	var chunkLens []int
	result, err := transcribeStream(
		bytes.NewReader(audioBytes),
		defaultSampleRate,
		func(chunk []float32) (string, error) {
			chunkLens = append(chunkLens, len(chunk))
			if len(chunkLens) == 1 {
				return "hello from the moon", nil
			}
			return "the moon base", nil
		},
	)
	if err != nil {
		t.Fatalf("transcribeStream() error = %v", err)
	}

	if len(chunkLens) != 2 {
		t.Fatalf("len(chunkLens) = %d, want 2", len(chunkLens))
	}
	if chunkLens[0] != secondsToSamples(defaultSampleRate, chunkTargetSeconds) {
		t.Fatalf("first chunk len = %d", chunkLens[0])
	}
	if chunkLens[1] != secondsToSamples(defaultSampleRate, 5.5) {
		t.Fatalf("second chunk len = %d", chunkLens[1])
	}
	if result.Text != "hello from the moon base" {
		t.Fatalf("text = %q, want %q", result.Text, "hello from the moon base")
	}
	if math.Abs(result.Duration-35.0) > 1e-6 {
		t.Fatalf("Duration = %v, want 35", result.Duration)
	}
}

func TestTranscribeStreamSkipsPureSilence(t *testing.T) {
	samples := constantPCM(65, 0)

	calls := 0
	result, err := transcribeStream(
		bytes.NewReader(rawPCMTestBytes(samples)),
		defaultSampleRate,
		func(chunk []float32) (string, error) {
			calls++
			return "hallucination", nil
		},
	)
	if err != nil {
		t.Fatalf("transcribeStream() error = %v", err)
	}
	if result.Text != "" {
		t.Fatalf("text = %q, want empty", result.Text)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
	if math.Abs(result.Duration-65.0) > 1e-6 {
		t.Fatalf("Duration = %v, want 65", result.Duration)
	}
}

func TestTranscribeStreamWithChunkSizeUsesConfiguredTarget(t *testing.T) {
	samples := constantPCM(25, 0.2)

	var chunkLens []int
	result, err := transcribeStreamWithChunkSize(
		bytes.NewReader(rawPCMTestBytes(samples)),
		defaultSampleRate,
		10*time.Second,
		func(chunk []float32) (string, error) {
			chunkLens = append(chunkLens, len(chunk))
			return "hello", nil
		},
	)
	if err != nil {
		t.Fatalf("transcribeStreamWithChunkSize() error = %v", err)
	}

	if len(chunkLens) != 3 {
		t.Fatalf("len(chunkLens) = %d, want 3", len(chunkLens))
	}
	if chunkLens[0] != secondsToSamples(defaultSampleRate, 10) {
		t.Fatalf("first chunk len = %d", chunkLens[0])
	}
	if chunkLens[1] != secondsToSamples(defaultSampleRate, 10) {
		t.Fatalf("second chunk len = %d", chunkLens[1])
	}
	if chunkLens[2] != secondsToSamples(defaultSampleRate, 6) {
		t.Fatalf("final chunk len = %d", chunkLens[2])
	}
	if math.Abs(result.Duration-25.0) > 1e-6 {
		t.Fatalf("Duration = %v, want 25", result.Duration)
	}
}

func rawPCMTestBytes(samples []float32) []byte {
	data := make([]byte, len(samples)*4)
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(sample))
	}
	return data
}
