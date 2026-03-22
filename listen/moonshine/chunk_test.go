package moonshine

import (
	"math"
	"testing"
)

func TestTranscribeChunkedPCMKeepsShortAudioInOnePass(t *testing.T) {
	samples := constantPCM(10, 0.2)
	calls := 0
	gotLen := 0

	text, err := transcribeChunkedPCM(samples, defaultSampleRate, func(chunk []float32) (string, error) {
		calls++
		gotLen = len(chunk)
		return "hello world", nil
	})
	if err != nil {
		t.Fatalf("transcribeChunkedPCM() error = %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q, want %q", text, "hello world")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if gotLen != len(samples) {
		t.Fatalf("chunk len = %d, want %d", gotLen, len(samples))
	}
}

func TestPlanNextChunkNeedsMoreBeforeEOF(t *testing.T) {
	samples := constantPCM(31, 0.2)

	plan := planNextChunk(samples, defaultSampleRate, false)
	if !plan.needMore {
		t.Fatalf("needMore = false, want true")
	}
	if plan.chunk.end != 0 {
		t.Fatalf("chunk end = %d, want 0", plan.chunk.end)
	}
}

func TestPlanNextChunkAllowsShortFinalChunkAtEOF(t *testing.T) {
	samples := constantPCM(10, 0.2)

	plan := planNextChunk(samples, defaultSampleRate, true)
	if plan.needMore {
		t.Fatalf("needMore = true, want false")
	}
	if !plan.final {
		t.Fatalf("final = false, want true")
	}
	if plan.chunk.start != 0 || plan.chunk.end != len(samples) {
		t.Fatalf("chunk = %+v, want [0,%d)", plan.chunk, len(samples))
	}
}

func TestPlanAudioChunksPrefersSilenceNearTarget(t *testing.T) {
	samples := append(constantPCM(30.25, 0.2), constantPCM(0.30, 0)...)
	samples = append(samples, constantPCM(4.45, 0.2)...)

	chunks := planAudioChunks(samples, defaultSampleRate)
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want >= 2", len(chunks))
	}

	wantCut := secondsToSamples(defaultSampleRate, 30.25)
	if chunks[0].end != wantCut {
		t.Fatalf("first cut = %d, want %d", chunks[0].end, wantCut)
	}

	wantNextStart := wantCut - secondsToSamples(defaultSampleRate, chunkOverlapSeconds)
	if chunks[1].start != wantNextStart {
		t.Fatalf("next start = %d, want %d", chunks[1].start, wantNextStart)
	}
}

func TestPlanAudioChunksHardCutsWhenNoSilenceExists(t *testing.T) {
	samples := constantPCM(35, 0.2)

	chunks := planAudioChunks(samples, defaultSampleRate)
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want >= 2", len(chunks))
	}

	wantCut := secondsToSamples(defaultSampleRate, chunkTargetSeconds)
	if chunks[0].end != wantCut {
		t.Fatalf("first cut = %d, want %d", chunks[0].end, wantCut)
	}
}

func TestPlanAudioChunksRelaxesThresholdForQuietAudio(t *testing.T) {
	speech := dbfsAmplitude(-45)
	silence := dbfsAmplitude(-55)

	samples := append(constantPCM(30.25, speech), constantPCM(0.30, silence)...)
	samples = append(samples, constantPCM(4.45, speech)...)

	if got := silenceFloorFor(samples); got != quietSilenceFloorDBFS {
		t.Fatalf("silenceFloorFor() = %v, want %v", got, quietSilenceFloorDBFS)
	}

	chunks := planAudioChunks(samples, defaultSampleRate)
	wantCut := secondsToSamples(defaultSampleRate, 30.25)
	if chunks[0].end != wantCut {
		t.Fatalf("first cut = %d, want %d", chunks[0].end, wantCut)
	}
}

func TestTranscribeChunkedPCMSkipsPureSilence(t *testing.T) {
	samples := constantPCM(65, 0)
	calls := 0

	text, err := transcribeChunkedPCM(samples, defaultSampleRate, func(chunk []float32) (string, error) {
		calls++
		return "hallucination", nil
	})
	if err != nil {
		t.Fatalf("transcribeChunkedPCM() error = %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestStitchChunkTextsDedupsOverlapWords(t *testing.T) {
	got := stitchChunkTexts([]string{
		"hello from the moon",
		"the moon base is live",
	})
	want := "hello from the moon base is live"
	if got != want {
		t.Fatalf("stitchChunkTexts() = %q, want %q", got, want)
	}
}

func constantPCM(seconds float64, amplitude float32) []float32 {
	samples := make([]float32, secondsToSamples(defaultSampleRate, seconds))
	for i := range samples {
		samples[i] = amplitude
	}
	return samples
}

func dbfsAmplitude(db float64) float32 {
	return float32(math.Pow(10, db/20))
}
