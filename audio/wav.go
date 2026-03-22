// Package audio provides PCM audio helpers.
package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	wavHeaderSize    = 44
	pcm16BytesPerPCM = 2
)

type writeSeeker interface {
	io.Writer
	io.Seeker
}

// WAVWriter streams float32 PCM samples as a 16-bit mono WAV file.
type WAVWriter struct {
	dst        io.Writer
	seekable   writeSeeker
	buffered   *os.File
	sampleRate int
	dataBytes  int64
	closed     bool
}

// NewWAVWriter creates a streaming WAV writer.
func NewWAVWriter(w io.Writer, sampleRate int) (*WAVWriter, error) {
	if w == nil {
		return nil, fmt.Errorf("wav writer is nil")
	}
	if sampleRate <= 0 {
		return nil, fmt.Errorf("wav sample rate must be positive")
	}
	if !fitsWAVHeader(sampleRate, 0) {
		return nil, fmt.Errorf("wav sample rate %d is too large", sampleRate)
	}

	ww := &WAVWriter{
		dst:        w,
		sampleRate: sampleRate,
	}

	if ws, ok := probeWriteSeeker(w); ok {
		header, err := encodeWAVHeader(sampleRate, 0)
		if err != nil {
			return nil, err
		}
		if _, err := ws.Write(header); err != nil {
			return nil, err
		}
		ww.seekable = ws
		return ww, nil
	}

	tmp, err := os.CreateTemp("", "cattery-wav-*")
	if err != nil {
		return nil, err
	}
	ww.buffered = tmp
	return ww, nil
}

// WriteSamples encodes and appends float32 PCM samples.
func (w *WAVWriter) WriteSamples(samples []float32) error {
	if w == nil {
		return fmt.Errorf("wav writer is nil")
	}
	if w.closed {
		return fmt.Errorf("wav writer is closed")
	}
	if len(samples) == 0 {
		return nil
	}

	dataBytes := int64(len(samples) * pcm16BytesPerPCM)
	if !fitsWAVHeader(w.sampleRate, w.dataBytes+dataBytes) {
		return fmt.Errorf("wav data exceeds 4 GiB limit")
	}

	pcm := make([]byte, len(samples)*pcm16BytesPerPCM)
	for i, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		sample := int16(math.Round(float64(s) * 32767))
		binary.LittleEndian.PutUint16(pcm[i*pcm16BytesPerPCM:], uint16(sample))
	}

	target := io.Writer(w.buffered)
	if w.seekable != nil {
		target = w.seekable
	}

	n, err := target.Write(pcm)
	w.dataBytes += int64(n)
	if err != nil {
		return err
	}
	if n != len(pcm) {
		return io.ErrShortWrite
	}

	return nil
}

// Close finalizes the WAV stream. It does not close the destination writer.
func (w *WAVWriter) Close() error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true

	var err error
	if w.seekable != nil {
		err = w.closeSeekable()
	}
	if w.buffered != nil {
		if flushErr := w.closeBuffered(); err == nil {
			err = flushErr
		}
		if cleanupErr := w.cleanupTemp(); err == nil {
			err = cleanupErr
		}
	}

	return err
}

// WriteWAV writes float32 PCM samples as a 16-bit mono WAV file.
func WriteWAV(w io.Writer, samples []float32, sampleRate int) error {
	ww, err := NewWAVWriter(w, sampleRate)
	if err != nil {
		return err
	}
	if err := ww.WriteSamples(samples); err != nil {
		_ = ww.cleanupTemp()
		return err
	}
	return ww.Close()
}

func probeWriteSeeker(w io.Writer) (writeSeeker, bool) {
	ws, ok := w.(writeSeeker)
	if !ok {
		return nil, false
	}
	if _, err := ws.Seek(0, io.SeekCurrent); err != nil {
		return nil, false
	}
	return ws, true
}

func (w *WAVWriter) closeSeekable() error {
	header, err := encodeWAVHeader(w.sampleRate, w.dataBytes)
	if err != nil {
		return err
	}

	if _, err := w.seekable.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.seekable.Write(header); err != nil {
		return err
	}
	_, err = w.seekable.Seek(int64(wavHeaderSize)+w.dataBytes, io.SeekStart)
	return err
}

func (w *WAVWriter) closeBuffered() error {
	header, err := encodeWAVHeader(w.sampleRate, w.dataBytes)
	if err != nil {
		return err
	}

	if _, err := w.dst.Write(header); err != nil {
		return err
	}
	if _, err := w.buffered.Seek(0, io.SeekStart); err != nil {
		return err
	}

	_, err = io.Copy(w.dst, w.buffered)
	return err
}

func (w *WAVWriter) cleanupTemp() error {
	if w == nil || w.buffered == nil {
		return nil
	}

	name := w.buffered.Name()
	err := w.buffered.Close()
	w.buffered = nil
	if removeErr := os.Remove(name); err == nil {
		err = removeErr
	}
	return err
}

func encodeWAVHeader(sampleRate int, dataBytes int64) ([]byte, error) {
	if !fitsWAVHeader(sampleRate, dataBytes) {
		return nil, fmt.Errorf("wav header exceeds 32-bit size limits")
	}

	header := make([]byte, wavHeaderSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataBytes))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], wavFormatPCM)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*pcm16BytesPerPCM))
	binary.LittleEndian.PutUint16(header[32:34], pcm16BytesPerPCM)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataBytes))
	return header, nil
}

func fitsWAVHeader(sampleRate int, dataBytes int64) bool {
	if sampleRate <= 0 || dataBytes < 0 {
		return false
	}
	maxUint32 := int64(^uint32(0))
	if int64(sampleRate) > maxUint32/int64(pcm16BytesPerPCM) {
		return false
	}
	return dataBytes <= maxUint32-36
}
