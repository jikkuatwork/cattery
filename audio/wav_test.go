package audio

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"testing"
)

func TestWAVWriterSeekablePatchesHeader(t *testing.T) {
	first := []float32{-1.0, -0.5, 0.0}
	second := []float32{0.25, 0.75, 1.0}
	want := append(append([]float32{}, first...), second...)

	dst := &seekBuffer{}
	w, err := NewWAVWriter(dst, 24000)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	if got, wantLen := len(dst.Bytes()), wavHeaderSize; got != wantLen {
		t.Fatalf("len(before samples) = %d, want %d", got, wantLen)
	}

	if err := w.WriteSamples(first); err != nil {
		t.Fatalf("WriteSamples(first): %v", err)
	}
	if err := w.WriteSamples(second); err != nil {
		t.Fatalf("WriteSamples(second): %v", err)
	}

	wantDataLen := wavHeaderSize + len(want)*pcm16BytesPerPCM
	if got := len(dst.Bytes()); got != wantDataLen {
		t.Fatalf("len(before close) = %d, want %d", got, wantDataLen)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertDecodedPCM(t, dst.Bytes(), want, 24000)

	var wrapped bytes.Buffer
	if err := WriteWAV(&wrapped, want, 24000); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	if !bytes.Equal(dst.Bytes(), wrapped.Bytes()) {
		t.Fatalf("seekable output differs from WriteWAV output")
	}
}

func TestWAVWriterNonSeekableBuffersUntilClose(t *testing.T) {
	first := []float32{-0.25, 0.0, 0.25}
	second := []float32{0.5, 1.2, -1.2}
	want := append(append([]float32{}, first...), second...)

	var dst bytes.Buffer
	w, err := NewWAVWriter(&dst, 16000)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	if got := dst.Len(); got != 0 {
		t.Fatalf("dst.Len() after NewWAVWriter = %d, want 0", got)
	}

	if err := w.WriteSamples(first); err != nil {
		t.Fatalf("WriteSamples(first): %v", err)
	}
	if err := w.WriteSamples(second); err != nil {
		t.Fatalf("WriteSamples(second): %v", err)
	}
	if got := dst.Len(); got != 0 {
		t.Fatalf("dst.Len() before Close = %d, want 0", got)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertDecodedPCM(t, dst.Bytes(), want, 16000)
}

func TestWriteWAVMatchesStreamingWriter(t *testing.T) {
	samples := []float32{-1.0, -0.125, 0.0, 0.125, 0.9}

	var want bytes.Buffer
	w, err := NewWAVWriter(&want, 22050)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}
	if err := w.WriteSamples(samples[:2]); err != nil {
		t.Fatalf("WriteSamples(first): %v", err)
	}
	if err := w.WriteSamples(samples[2:]); err != nil {
		t.Fatalf("WriteSamples(second): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var got bytes.Buffer
	if err := WriteWAV(&got, samples, 22050); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("WriteWAV output differs from streaming output")
	}
}

type seekBuffer struct {
	data   []byte
	offset int64
}

func (b *seekBuffer) Write(p []byte) (int, error) {
	if b.offset < 0 {
		return 0, fmt.Errorf("negative offset %d", b.offset)
	}

	start := int(b.offset)
	end := start + len(p)
	if end > len(b.data) {
		grow := make([]byte, end-len(b.data))
		b.data = append(b.data, grow...)
	}
	copy(b.data[start:end], p)
	b.offset += int64(len(p))
	return len(p), nil
}

func (b *seekBuffer) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = b.offset + offset
	case io.SeekEnd:
		next = int64(len(b.data)) + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if next < 0 {
		return 0, fmt.Errorf("negative offset %d", next)
	}

	b.offset = next
	return next, nil
}

func (b *seekBuffer) Bytes() []byte {
	return append([]byte(nil), b.data...)
}

func assertDecodedPCM(t *testing.T, data []byte, want []float32, wantRate int) {
	t.Helper()

	got, sampleRate, err := ReadPCM(bytes.NewReader(data), 1)
	if err != nil {
		t.Fatalf("ReadPCM: %v", err)
	}
	if sampleRate != wantRate {
		t.Fatalf("sampleRate = %d, want %d", sampleRate, wantRate)
	}
	if len(got) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		wantPCM := pcm16RoundTrip(want[i])
		if math.Abs(float64(got[i]-wantPCM)) > 1e-6 {
			t.Fatalf("sample[%d] = %.6f, want %.6f", i, got[i], wantPCM)
		}
	}
}

func pcm16RoundTrip(sample float32) float32 {
	if sample > 1.0 {
		sample = 1.0
	} else if sample < -1.0 {
		sample = -1.0
	}
	pcm := int16(math.Round(float64(sample) * 32767))
	return float32(pcm) / 32768.0
}
