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
	if r == nil {
		return nil, 0, fmt.Errorf("audio reader is nil")
	}
	if defaultRate <= 0 {
		return nil, 0, fmt.Errorf("default sample rate must be positive")
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("audio is empty")
	}

	if looksLikeWAV(data) {
		return decodeWAV(data)
	}
	return decodeRawFloat32(data, defaultRate)
}

func looksLikeWAV(data []byte) bool {
	return len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WAVE"
}

func decodeWAV(data []byte) ([]float32, int, error) {
	if !looksLikeWAV(data) {
		return nil, 0, fmt.Errorf("invalid wav header")
	}

	var (
		formatTag     uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		payload       []byte
		haveFmt       bool
	)

	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8

		if chunkSize < 0 || offset+chunkSize > len(data) {
			return nil, 0, fmt.Errorf("truncated wav chunk %q", chunkID)
		}

		chunk := data[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, fmt.Errorf("wav fmt chunk too small")
			}
			formatTag = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			haveFmt = true
		case "data":
			payload = chunk
		}

		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}

	if !haveFmt {
		return nil, 0, fmt.Errorf("wav missing fmt chunk")
	}
	if len(payload) == 0 {
		return nil, 0, fmt.Errorf("wav missing data chunk")
	}
	if channels == 0 {
		return nil, 0, fmt.Errorf("wav channel count is zero")
	}
	if sampleRate == 0 {
		return nil, 0, fmt.Errorf("wav sample rate is zero")
	}

	switch formatTag {
	case wavFormatPCM:
		return decodePCM16WAV(payload, int(channels), int(sampleRate), bitsPerSample)
	case wavFormatFloat:
		return decodeFloat32WAV(payload, int(channels), int(sampleRate), bitsPerSample)
	default:
		return nil, 0, fmt.Errorf("unsupported wav format %d", formatTag)
	}
}

func decodePCM16WAV(payload []byte, channels, sampleRate int, bitsPerSample uint16) ([]float32, int, error) {
	if bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("unsupported pcm bit depth %d", bitsPerSample)
	}

	frameSize := channels * 2
	if frameSize <= 0 || len(payload)%frameSize != 0 {
		return nil, 0, fmt.Errorf("truncated pcm16 wav payload")
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

	return samples, sampleRate, nil
}

func decodeFloat32WAV(payload []byte, channels, sampleRate int, bitsPerSample uint16) ([]float32, int, error) {
	if bitsPerSample != 32 {
		return nil, 0, fmt.Errorf("unsupported float wav bit depth %d", bitsPerSample)
	}

	frameSize := channels * 4
	if frameSize <= 0 || len(payload)%frameSize != 0 {
		return nil, 0, fmt.Errorf("truncated float wav payload")
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
				return nil, 0, fmt.Errorf("wav contains invalid float sample")
			}
			sum += v
		}
		samples[i] = sum / float32(channels)
	}

	return samples, sampleRate, nil
}

func decodeRawFloat32(data []byte, sampleRate int) ([]float32, int, error) {
	if len(data)%4 != 0 {
		return nil, 0, fmt.Errorf("raw pcm is not float32: %d trailing bytes", len(data)%4)
	}

	samples := make([]float32, len(data)/4)
	for i := range samples {
		v := math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, 0, fmt.Errorf("raw pcm contains invalid float sample")
		}
		samples[i] = v
	}

	return samples, sampleRate, nil
}
