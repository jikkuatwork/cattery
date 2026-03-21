// Package audio provides PCM audio helpers.
package audio

import (
	"encoding/binary"
	"io"
	"math"
)

// WriteWAV writes float32 PCM samples as a 16-bit mono WAV file.
func WriteWAV(w io.Writer, samples []float32, sampleRate int) error {
	numSamples := len(samples)
	dataSize := numSamples * 2 // 16-bit = 2 bytes per sample
	fileSize := 36 + dataSize

	// RIFF header
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(fileSize)); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WAVE")); err != nil {
		return err
	}

	// fmt chunk
	if _, err := w.Write([]byte("fmt ")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16)); err != nil { // chunk size
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // PCM format
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // mono
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(sampleRate)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(sampleRate*2)); err != nil { // byte rate
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(2)); err != nil { // block align
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(16)); err != nil { // bits per sample
		return err
	}

	// data chunk
	if _, err := w.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(dataSize)); err != nil {
		return err
	}

	// Convert float32 [-1,1] to int16 and write
	for _, s := range samples {
		// Clamp
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		sample := int16(math.Round(float64(s) * 32767))
		if err := binary.Write(w, binary.LittleEndian, sample); err != nil {
			return err
		}
	}

	return nil
}
