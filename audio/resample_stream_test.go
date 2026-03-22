package audio

import (
	"math"
	"testing"
)

func TestStreamResamplerMatchesWholeBufferDownsample(t *testing.T) {
	samples := testSignal(113)
	want := Resample(samples, 24000, 16000)
	got := streamResampleAll(t, samples, 24000, 16000, []int{1, 7, 3, 11, 2, 19, 5})

	assertResampledSamples(t, got, want)
}

func TestStreamResamplerMatchesWholeBufferUpsample(t *testing.T) {
	samples := testSignal(91)
	want := Resample(samples, 16000, 24000)
	got := streamResampleAll(t, samples, 16000, 24000, []int{2, 5, 1, 9, 4, 13})

	assertResampledSamples(t, got, want)
}

func TestStreamResamplerCopiesSameRateBlocks(t *testing.T) {
	samples := testSignal(37)
	want := append([]float32(nil), samples...)
	got := streamResampleAll(t, samples, 16000, 16000, []int{4, 3, 8})

	assertResampledSamples(t, got, want)
}

func streamResampleAll(
	t *testing.T,
	samples []float32,
	fromRate, toRate int,
	chunkSizes []int,
) []float32 {
	t.Helper()

	resampler, err := NewStreamResampler(fromRate, toRate)
	if err != nil {
		t.Fatalf("NewStreamResampler: %v", err)
	}

	var out []float32
	for offset, i := 0, 0; offset < len(samples); i++ {
		size := chunkSizes[i%len(chunkSizes)]
		end := offset + size
		if end > len(samples) {
			end = len(samples)
		}
		out = append(out, resampler.Write(samples[offset:end])...)
		offset = end
	}
	out = append(out, resampler.Flush()...)
	return out
}

func assertResampledSamples(t *testing.T, got, want []float32) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-6 {
			t.Fatalf("sample[%d] = %.6f, want %.6f", i, got[i], want[i])
		}
	}
}

func testSignal(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(math.Sin(float64(i)/11.0)) + float32((i%7)-3)/10
	}
	return out
}
