package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	wavFormatPCM   = 1
	wavFormatFloat = 3
)

// ReadPCM reads audio from r and returns mono float32 PCM samples.
// WAV input is decoded from its header. Non-WAV input is treated as raw
// little-endian float32 PCM at defaultRate.
func ReadPCM(r io.Reader, defaultRate int) ([]float32, int, error) {
	stream, err := NewPCMStreamReader(r, defaultRate)
	if err != nil {
		return nil, 0, err
	}

	const readBlock = 4096
	all := make([]float32, 0, readBlock)
	for {
		block, err := stream.ReadSamples(readBlock)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		all = append(all, block...)
	}

	if len(all) == 0 {
		return nil, 0, fmt.Errorf("audio is empty")
	}

	return all, stream.SampleRate(), nil
}

func looksLikeWAV(data []byte) bool {
	return len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WAVE"
}

func decodePCM16Frames(payload []byte, channels int) ([]float32, error) {
	frameSize := channels * 2
	if frameSize <= 0 || len(payload)%frameSize != 0 {
		return nil, fmt.Errorf("truncated pcm16 wav payload")
	}

	frames := len(payload) / frameSize
	samples := make([]float32, frames)
	for i := 0; i < frames; i++ {
		base := i * frameSize
		var sum float32
		for ch := 0; ch < channels; ch++ {
			pos := base + ch*2
			raw := int16(binary.LittleEndian.Uint16(payload[pos : pos+2]))
			sum += float32(raw) / 32768.0
		}
		samples[i] = sum / float32(channels)
	}

	return samples, nil
}

func decodeFloat32Frames(payload []byte, channels int) ([]float32, error) {
	frameSize := channels * 4
	if frameSize <= 0 || len(payload)%frameSize != 0 {
		return nil, fmt.Errorf("truncated float wav payload")
	}

	frames := len(payload) / frameSize
	samples := make([]float32, frames)
	for i := 0; i < frames; i++ {
		base := i * frameSize
		var sum float32
		for ch := 0; ch < channels; ch++ {
			pos := base + ch*4
			v := math.Float32frombits(binary.LittleEndian.Uint32(payload[pos : pos+4]))
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return nil, fmt.Errorf("wav contains invalid float sample")
			}
			sum += v
		}
		samples[i] = sum / float32(channels)
	}

	return samples, nil
}

func decodeRawFloat32Bytes(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("raw pcm is not float32: %d trailing bytes", len(data)%4)
	}

	samples := make([]float32, len(data)/4)
	for i := range samples {
		v := math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("raw pcm contains invalid float sample")
		}
		samples[i] = v
	}

	return samples, nil
}
