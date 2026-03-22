package audio

import (
	"fmt"
	"math"
)

// StreamResampler incrementally linearly resamples mono PCM samples.
type StreamResampler struct {
	fromRate int
	toRate   int
	step     float64

	pending      []float32
	pendingStart int
	nextPos      float64

	totalIn int
	emitted int
}

// NewStreamResampler creates a streaming linear resampler.
func NewStreamResampler(fromRate, toRate int) (*StreamResampler, error) {
	if fromRate <= 0 {
		return nil, fmt.Errorf("source sample rate must be positive")
	}
	if toRate <= 0 {
		return nil, fmt.Errorf("target sample rate must be positive")
	}
	return &StreamResampler{
		fromRate: fromRate,
		toRate:   toRate,
		step:     float64(fromRate) / float64(toRate),
	}, nil
}

// Write appends source samples and returns the resampled output that is ready.
func (r *StreamResampler) Write(samples []float32) []float32 {
	if r == nil || len(samples) == 0 {
		return nil
	}
	if r.fromRate == r.toRate {
		r.totalIn += len(samples)
		r.emitted += len(samples)
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	r.pending = append(r.pending, samples...)
	r.totalIn += len(samples)
	return r.emit(false)
}

// Flush returns the remaining resampled output after the final input block.
func (r *StreamResampler) Flush() []float32 {
	if r == nil || r.totalIn == 0 {
		return nil
	}
	if r.fromRate == r.toRate {
		return nil
	}
	return r.emit(true)
}

func (r *StreamResampler) emit(final bool) []float32 {
	if len(r.pending) == 0 {
		return nil
	}

	limit := 0
	if final {
		limit = int(math.Round(float64(r.totalIn) * float64(r.toRate) / float64(r.fromRate)))
		if limit < 1 {
			limit = 1
		}
	}

	out := make([]float32, 0, estimateOutputCap(len(r.pending), r.fromRate, r.toRate, final))
	for {
		if final {
			if r.emitted >= limit {
				break
			}
		} else if r.nextPos >= float64(r.pendingStart+len(r.pending)-1) {
			break
		}

		out = append(out, r.interpolate(r.nextPos))
		r.nextPos += r.step
		r.emitted++
	}

	r.discardConsumed()
	return out
}

func (r *StreamResampler) interpolate(pos float64) float32 {
	idx := int(pos)
	frac := float32(pos - float64(idx))
	local := idx - r.pendingStart
	switch {
	case local+1 < len(r.pending):
		return r.pending[local]*(1-frac) + r.pending[local+1]*frac
	case local >= 0 && local < len(r.pending):
		return r.pending[local]
	default:
		return r.pending[len(r.pending)-1]
	}
}

func (r *StreamResampler) discardConsumed() {
	keepFrom := int(math.Floor(r.nextPos))
	drop := keepFrom - r.pendingStart
	if drop <= 0 {
		return
	}
	if drop >= len(r.pending) {
		r.pending = r.pending[:0]
		r.pendingStart = keepFrom
		return
	}

	copy(r.pending, r.pending[drop:])
	r.pending = r.pending[:len(r.pending)-drop]
	r.pendingStart = keepFrom
}

func estimateOutputCap(inputLen, fromRate, toRate int, final bool) int {
	if inputLen <= 0 || fromRate <= 0 || toRate <= 0 {
		return 0
	}
	out := int(math.Ceil(float64(inputLen) * float64(toRate) / float64(fromRate)))
	if !final && out > 0 {
		out++
	}
	if out < 1 {
		out = 1
	}
	return out
}
