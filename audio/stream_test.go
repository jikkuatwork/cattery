package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

func TestPCMStreamReaderReadsRawFloat32InBlocks(t *testing.T) {
	want := []float32{0.25, -0.5, 0.75, -0.125, 0.5}

	stream, err := NewPCMStreamReader(bytes.NewReader(rawFloat32TestBytes(want)), 22050)
	if err != nil {
		t.Fatalf("NewPCMStreamReader: %v", err)
	}
	if stream.SampleRate() != 22050 {
		t.Fatalf("SampleRate() = %d, want 22050", stream.SampleRate())
	}

	got := collectStreamSamples(t, stream, 2)
	assertSampleSlices(t, got, want)
}

func TestPCMStreamReaderReadsPCM16WAVInBlocks(t *testing.T) {
	frames := [][]int16{
		{16384, 16384},
		{32767, -32768},
		{0, 16384},
		{-16384, 0},
	}
	want := make([]float32, len(frames))
	for i, frame := range frames {
		sum := int32(frame[0]) + int32(frame[1])
		want[i] = float32(sum) / 2 / 32768.0
	}

	stream, err := NewPCMStreamReader(
		bytes.NewReader(testPCM16WAVBytes(24000, frames, true)),
		16000,
	)
	if err != nil {
		t.Fatalf("NewPCMStreamReader: %v", err)
	}
	if stream.SampleRate() != 24000 {
		t.Fatalf("SampleRate() = %d, want 24000", stream.SampleRate())
	}

	got := collectStreamSamples(t, stream, 2)
	assertSampleSlices(t, got, want)
}

func TestPCMStreamReaderReadsFloat32WAVInBlocks(t *testing.T) {
	frames := [][]float32{
		{0.25, -0.25},
		{0.5, 0.0},
		{-0.75, -0.25},
	}
	want := []float32{0, 0.25, -0.5}

	stream, err := NewPCMStreamReader(
		bytes.NewReader(testFloat32WAVBytes(32000, frames)),
		16000,
	)
	if err != nil {
		t.Fatalf("NewPCMStreamReader: %v", err)
	}
	if stream.SampleRate() != 32000 {
		t.Fatalf("SampleRate() = %d, want 32000", stream.SampleRate())
	}

	got := collectStreamSamples(t, stream, 1)
	assertSampleSlices(t, got, want)
}

func collectStreamSamples(t *testing.T, stream *PCMStreamReader, blockSize int) []float32 {
	t.Helper()

	var out []float32
	for {
		block, err := stream.ReadSamples(blockSize)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadSamples(%d): %v", blockSize, err)
		}
		if len(block) > blockSize {
			t.Fatalf("len(block) = %d, want <= %d", len(block), blockSize)
		}
		out = append(out, block...)
	}
	return out
}

func assertSampleSlices(t *testing.T, got, want []float32) {
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

func rawFloat32TestBytes(samples []float32) []byte {
	data := make([]byte, len(samples)*4)
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(sample))
	}
	return data
}

func testPCM16WAVBytes(sampleRate int, frames [][]int16, withJunk bool) []byte {
	var data bytes.Buffer
	for _, frame := range frames {
		for _, sample := range frame {
			_ = binary.Write(&data, binary.LittleEndian, sample)
		}
	}

	var payload bytes.Buffer
	writeWAVTestChunk(&payload, "fmt ", func() []byte {
		chunk := make([]byte, 16)
		binary.LittleEndian.PutUint16(chunk[0:2], wavFormatPCM)
		binary.LittleEndian.PutUint16(chunk[2:4], uint16(len(frames[0])))
		binary.LittleEndian.PutUint32(chunk[4:8], uint32(sampleRate))
		binary.LittleEndian.PutUint32(
			chunk[8:12],
			uint32(sampleRate*len(frames[0])*pcm16BytesPerPCM),
		)
		binary.LittleEndian.PutUint16(chunk[12:14], uint16(len(frames[0])*pcm16BytesPerPCM))
		binary.LittleEndian.PutUint16(chunk[14:16], 16)
		return chunk
	}())
	if withJunk {
		writeWAVTestChunk(&payload, "JUNK", []byte{1, 2, 3})
	}
	writeWAVTestChunk(&payload, "data", data.Bytes())

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36+payload.Len()))
	wav.WriteString("WAVE")
	wav.Write(payload.Bytes())
	return wav.Bytes()
}

func testFloat32WAVBytes(sampleRate int, frames [][]float32) []byte {
	var data bytes.Buffer
	for _, frame := range frames {
		for _, sample := range frame {
			_ = binary.Write(&data, binary.LittleEndian, sample)
		}
	}

	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], wavFormatFloat)
	binary.LittleEndian.PutUint16(fmtChunk[2:4], uint16(len(frames[0])))
	binary.LittleEndian.PutUint32(fmtChunk[4:8], uint32(sampleRate))
	binary.LittleEndian.PutUint32(fmtChunk[8:12], uint32(sampleRate*len(frames[0])*4))
	binary.LittleEndian.PutUint16(fmtChunk[12:14], uint16(len(frames[0])*4))
	binary.LittleEndian.PutUint16(fmtChunk[14:16], 32)

	var payload bytes.Buffer
	writeWAVTestChunk(&payload, "fmt ", fmtChunk)
	writeWAVTestChunk(&payload, "data", data.Bytes())

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36+payload.Len()))
	wav.WriteString("WAVE")
	wav.Write(payload.Bytes())
	return wav.Bytes()
}

func writeWAVTestChunk(dst *bytes.Buffer, id string, payload []byte) {
	dst.WriteString(id)
	_ = binary.Write(dst, binary.LittleEndian, uint32(len(payload)))
	dst.Write(payload)
	if len(payload)%2 == 1 {
		dst.WriteByte(0)
	}
}
