package audio

import "math"

// Resample linearly resamples mono PCM samples between sample rates.
func Resample(samples []float32, fromRate, toRate int) []float32 {
	if len(samples) == 0 || fromRate <= 0 || toRate <= 0 {
		return nil
	}
	if fromRate == toRate {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	outLen := int(math.Round(float64(len(samples)) * float64(toRate) / float64(fromRate)))
	if outLen < 1 {
		outLen = 1
	}

	out := make([]float32, outLen)
	ratio := float64(fromRate) / float64(toRate)
	for i := range out {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := float32(srcPos - float64(idx))

		switch {
		case idx+1 < len(samples):
			out[i] = samples[idx]*(1-frac) + samples[idx+1]*frac
		case idx < len(samples):
			out[i] = samples[idx]
		default:
			out[i] = samples[len(samples)-1]
		}
	}

	return out
}
