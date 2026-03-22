package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

type pcmStreamKind int

const (
	pcmStreamRawFloat32 pcmStreamKind = iota + 1
	pcmStreamWAVPCM16
	pcmStreamWAVFloat32
)

// PCMStreamReader incrementally decodes mono float32 PCM samples.
type PCMStreamReader struct {
	src *bufio.Reader

	sampleRate int
	kind       pcmStreamKind
	channels   int
	frameSize  int

	dataBytesRemaining int64
	padBytesRemaining  int

	scratch []byte
	done    bool
}

// NewPCMStreamReader creates a streaming PCM reader for WAV or raw float32.
func NewPCMStreamReader(r io.Reader, defaultRate int) (*PCMStreamReader, error) {
	if r == nil {
		return nil, fmt.Errorf("audio reader is nil")
	}
	if defaultRate <= 0 {
		return nil, fmt.Errorf("default sample rate must be positive")
	}

	stream := &PCMStreamReader{
		src:        bufio.NewReader(r),
		sampleRate: defaultRate,
	}
	if err := stream.init(); err != nil {
		return nil, err
	}
	return stream, nil
}

// SampleRate returns the decoded source sample rate.
func (r *PCMStreamReader) SampleRate() int {
	if r == nil {
		return 0
	}
	return r.sampleRate
}

// ReadSamples reads up to maxSamples mono samples from the stream.
func (r *PCMStreamReader) ReadSamples(maxSamples int) ([]float32, error) {
	if r == nil {
		return nil, fmt.Errorf("audio reader is nil")
	}
	if maxSamples <= 0 {
		return nil, nil
	}
	if r.done {
		return nil, io.EOF
	}

	switch r.kind {
	case pcmStreamRawFloat32:
		return r.readRawFloat32(maxSamples)
	case pcmStreamWAVPCM16, pcmStreamWAVFloat32:
		return r.readWAVSamples(maxSamples)
	default:
		return nil, fmt.Errorf("audio stream is uninitialized")
	}
}

func (r *PCMStreamReader) init() error {
	peek, err := r.src.Peek(12)
	if err != nil && err != io.EOF {
		return err
	}
	if len(peek) == 0 {
		return fmt.Errorf("audio is empty")
	}
	if !looksLikeWAV(peek) {
		r.kind = pcmStreamRawFloat32
		return nil
	}
	return r.initWAV()
}

func (r *PCMStreamReader) initWAV() error {
	var header [12]byte
	if _, err := io.ReadFull(r.src, header[:]); err != nil {
		return fmt.Errorf("invalid wav header")
	}
	if !looksLikeWAV(header[:]) {
		return fmt.Errorf("invalid wav header")
	}

	var (
		formatTag     uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		haveFmt       bool
	)

	for {
		var chunkHeader [8]byte
		if _, err := io.ReadFull(r.src, chunkHeader[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return fmt.Errorf("wav missing data chunk")
			}
			return err
		}

		chunkID := string(chunkHeader[0:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkHeader[4:8]))
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return fmt.Errorf("wav fmt chunk too small")
			}

			var fmtChunk [16]byte
			if _, err := io.ReadFull(r.src, fmtChunk[:]); err != nil {
				return fmt.Errorf("truncated wav chunk %q", chunkID)
			}

			formatTag = binary.LittleEndian.Uint16(fmtChunk[0:2])
			channels = binary.LittleEndian.Uint16(fmtChunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(fmtChunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(fmtChunk[14:16])
			haveFmt = true

			if err := skipWAVBytes(r.src, chunkID, chunkSize-16); err != nil {
				return err
			}
		case "data":
			if !haveFmt {
				return fmt.Errorf("wav missing fmt chunk")
			}
			if chunkSize == 0 {
				return fmt.Errorf("wav missing data chunk")
			}
			if channels == 0 {
				return fmt.Errorf("wav channel count is zero")
			}
			if sampleRate == 0 {
				return fmt.Errorf("wav sample rate is zero")
			}

			r.sampleRate = int(sampleRate)
			r.channels = int(channels)

			switch formatTag {
			case wavFormatPCM:
				if bitsPerSample != 16 {
					return fmt.Errorf("unsupported pcm bit depth %d", bitsPerSample)
				}
				r.kind = pcmStreamWAVPCM16
				r.frameSize = r.channels * 2
			case wavFormatFloat:
				if bitsPerSample != 32 {
					return fmt.Errorf("unsupported float wav bit depth %d", bitsPerSample)
				}
				r.kind = pcmStreamWAVFloat32
				r.frameSize = r.channels * 4
			default:
				return fmt.Errorf("unsupported wav format %d", formatTag)
			}

			if r.frameSize <= 0 {
				return fmt.Errorf("wav channel count is zero")
			}
			if chunkSize%int64(r.frameSize) != 0 {
				if r.kind == pcmStreamWAVPCM16 {
					return fmt.Errorf("truncated pcm16 wav payload")
				}
				return fmt.Errorf("truncated float wav payload")
			}

			r.dataBytesRemaining = chunkSize
			return nil
		default:
			if err := skipWAVBytes(r.src, chunkID, chunkSize); err != nil {
				return err
			}
		}

		if chunkSize%2 == 1 {
			if err := skipWAVBytes(r.src, chunkID, 1); err != nil {
				return err
			}
		}
	}
}

func (r *PCMStreamReader) readRawFloat32(maxSamples int) ([]float32, error) {
	buf := r.ensureScratch(maxSamples * 4)
	n, err := io.ReadFull(r.src, buf)
	switch {
	case err == nil:
		return decodeRawFloat32Bytes(buf[:n])
	case err == io.EOF && n == 0:
		r.done = true
		return nil, io.EOF
	case err == io.EOF || err == io.ErrUnexpectedEOF:
		r.done = true
		return decodeRawFloat32Bytes(buf[:n])
	default:
		return nil, err
	}
}

func (r *PCMStreamReader) readWAVSamples(maxSamples int) ([]float32, error) {
	if r.dataBytesRemaining == 0 {
		r.done = true
		return nil, io.EOF
	}

	frames := maxSamples
	remainingFrames := int(r.dataBytesRemaining / int64(r.frameSize))
	if frames > remainingFrames {
		frames = remainingFrames
	}

	size := frames * r.frameSize
	buf := r.ensureScratch(size)
	if _, err := io.ReadFull(r.src, buf); err != nil {
		return nil, fmt.Errorf("truncated wav chunk %q", "data")
	}
	r.dataBytesRemaining -= int64(size)

	var (
		samples []float32
		err     error
	)
	switch r.kind {
	case pcmStreamWAVPCM16:
		samples, err = decodePCM16Frames(buf, r.channels)
	case pcmStreamWAVFloat32:
		samples, err = decodeFloat32Frames(buf, r.channels)
	default:
		err = fmt.Errorf("unsupported wav stream format")
	}
	if err != nil {
		return nil, err
	}

	if r.dataBytesRemaining == 0 {
		if err := r.finishWAVData(); err != nil {
			return nil, err
		}
		r.done = true
	}

	return samples, nil
}

func (r *PCMStreamReader) finishWAVData() error {
	if r.padBytesRemaining == 0 {
		return nil
	}
	if err := skipWAVBytes(r.src, "data", int64(r.padBytesRemaining)); err != nil {
		return err
	}
	r.padBytesRemaining = 0
	return nil
}

func (r *PCMStreamReader) ensureScratch(size int) []byte {
	if cap(r.scratch) < size {
		r.scratch = make([]byte, size)
	}
	r.scratch = r.scratch[:size]
	return r.scratch
}

func skipWAVBytes(r io.Reader, chunkID string, n int64) error {
	if n <= 0 {
		return nil
	}
	if _, err := io.CopyN(io.Discard, r, n); err != nil {
		return fmt.Errorf("truncated wav chunk %q", chunkID)
	}
	return nil
}
