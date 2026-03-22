package moonshine

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

const (
	chunkTargetSeconds       = 30.0
	chunkSearchWindowSeconds = 3.0
	chunkOverlapSeconds      = 0.5

	silenceFloorDBFS      = -40.0
	quietSilenceFloorDBFS = -50.0

	silenceMinDurationSeconds = 0.2
	silenceWindowSeconds      = 0.02
	silenceStepSeconds        = 0.01

	maxOverlapWords = 8
)

type sampleRange struct {
	start int
	end   int
}

func planAudioChunks(samples []float32, sampleRate int) []sampleRange {
	return planAudioChunksWithThreshold(samples, sampleRate, silenceFloorFor(samples))
}

func planAudioChunksWithThreshold(samples []float32, sampleRate int, threshold float64) []sampleRange {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}

	target := secondsToSamples(sampleRate, chunkTargetSeconds)
	if len(samples) <= target {
		return []sampleRange{{start: 0, end: len(samples)}}
	}

	search := secondsToSamples(sampleRate, chunkSearchWindowSeconds)
	overlap := secondsToSamples(sampleRate, chunkOverlapSeconds)
	silentRuns := findSilentRuns(samples, sampleRate, threshold)

	start := 0
	chunks := make([]sampleRange, 0, len(samples)/target+2)
	for start < len(samples) {
		remaining := len(samples) - start
		if remaining <= target {
			chunks = append(chunks, sampleRange{start: start, end: len(samples)})
			break
		}

		targetCut := start + target
		cut := targetCut
		searchSpan := sampleRange{
			start: maxInt(start+target-search, start),
			end:   minInt(start+target+search, len(samples)),
		}
		if nearest, ok := nearestSilenceCut(targetCut, searchSpan, silentRuns); ok {
			cut = nearest
		}
		if cut <= start {
			cut = minInt(start+1, len(samples))
		}

		chunks = append(chunks, sampleRange{start: start, end: cut})
		if cut >= len(samples) {
			break
		}

		nextStart := cut - overlap
		if nextStart <= start {
			nextStart = cut
		}
		start = nextStart
	}

	return chunks
}

func transcribeChunkedPCM(
	samples []float32,
	sampleRate int,
	transcribe func([]float32) (string, error),
) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	threshold := silenceFloorFor(samples)
	chunks := planAudioChunksWithThreshold(samples, sampleRate, threshold)

	texts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		part := samples[chunk.start:chunk.end]
		if chunkIsSilent(part, sampleRate, threshold) {
			continue
		}
		if transcribe == nil {
			return "", fmt.Errorf("missing chunk transcriber")
		}

		text, err := transcribe(part)
		if err != nil {
			return "", fmt.Errorf("transcribe chunk %d: %w", i+1, err)
		}
		text = strings.TrimSpace(text)
		if text != "" {
			texts = append(texts, text)
		}
	}

	return stitchChunkTexts(texts), nil
}

func stitchChunkTexts(texts []string) string {
	var stitched string
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if stitched == "" {
			stitched = text
			continue
		}
		stitched = stitchChunkPair(stitched, text)
	}
	return stitched
}

func stitchChunkPair(prev, next string) string {
	prevWords := strings.Fields(prev)
	nextWords := strings.Fields(next)
	if len(prevWords) == 0 {
		return strings.Join(nextWords, " ")
	}
	if len(nextWords) == 0 {
		return strings.Join(prevWords, " ")
	}

	maxWords := minInt(maxOverlapWords, minInt(len(prevWords), len(nextWords)))
	overlap := 0
	for n := maxWords; n >= 1; n-- {
		if matchBoundaryWords(prevWords[len(prevWords)-n:], nextWords[:n]) {
			overlap = n
			break
		}
	}

	rest := strings.Join(nextWords[overlap:], " ")
	if rest == "" {
		return prev
	}
	return prev + " " + rest
}

func matchBoundaryWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if normalizeBoundaryWord(a[i]) != normalizeBoundaryWord(b[i]) {
			return false
		}
	}
	return true
}

func normalizeBoundaryWord(word string) string {
	word = strings.TrimSpace(word)
	trimmed := strings.TrimFunc(word, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	if trimmed != "" {
		word = trimmed
	}
	return strings.ToLower(word)
}

func silenceFloorFor(samples []float32) float64 {
	if clipDBFS(samples) < silenceFloorDBFS {
		return quietSilenceFloorDBFS
	}
	return silenceFloorDBFS
}

func clipDBFS(samples []float32) float64 {
	if len(samples) == 0 {
		return math.Inf(-1)
	}
	return rangeDBFS(squaredPrefix(samples), sampleRange{start: 0, end: len(samples)})
}

func chunkIsSilent(samples []float32, sampleRate int, threshold float64) bool {
	if len(samples) == 0 || sampleRate <= 0 {
		return true
	}

	window := maxInt(1, secondsToSamples(sampleRate, silenceWindowSeconds))
	spans := slidingWindows(len(samples), window, maxInt(1, secondsToSamples(sampleRate, silenceStepSeconds)))
	if len(spans) == 0 {
		return clipDBFS(samples) <= threshold
	}

	prefix := squaredPrefix(samples)
	for _, span := range spans {
		if rangeDBFS(prefix, span) > threshold {
			return false
		}
	}
	return true
}

func findSilentRuns(samples []float32, sampleRate int, threshold float64) []sampleRange {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}

	window := maxInt(1, secondsToSamples(sampleRate, silenceWindowSeconds))
	minRun := maxInt(1, secondsToSamples(sampleRate, silenceMinDurationSeconds))
	step := maxInt(1, secondsToSamples(sampleRate, silenceStepSeconds))
	spans := slidingWindows(len(samples), window, step)
	if len(spans) == 0 {
		if len(samples) >= minRun && clipDBFS(samples) <= threshold {
			return []sampleRange{{start: 0, end: len(samples)}}
		}
		return nil
	}

	prefix := squaredPrefix(samples)
	var runs []sampleRange
	runStart := -1
	runEnd := -1

	flush := func() {
		if runStart >= 0 && runEnd-runStart >= minRun {
			runs = append(runs, sampleRange{start: runStart, end: runEnd})
		}
		runStart = -1
		runEnd = -1
	}

	for _, span := range spans {
		if rangeDBFS(prefix, span) <= threshold {
			if runStart < 0 {
				runStart = span.start
			}
			runEnd = span.end
			continue
		}
		flush()
	}
	flush()

	return runs
}

func nearestSilenceCut(target int, search sampleRange, runs []sampleRange) (int, bool) {
	bestCut := 0
	bestDist := 0
	found := false

	for _, run := range runs {
		overlap, ok := intersectRanges(search, run)
		if !ok {
			continue
		}

		cut := clampInt(target, overlap.start, overlap.end)
		dist := absInt(cut - target)
		if !found || dist < bestDist || (dist == bestDist && cut < bestCut) {
			bestCut = cut
			bestDist = dist
			found = true
		}
	}

	return bestCut, found
}

func slidingWindows(length, window, step int) []sampleRange {
	if length <= 0 || window <= 0 || step <= 0 {
		return nil
	}
	if length <= window {
		return []sampleRange{{start: 0, end: length}}
	}

	spans := make([]sampleRange, 0, length/step+1)
	for start := 0; start+window <= length; start += step {
		spans = append(spans, sampleRange{start: start, end: start + window})
	}

	lastStart := length - window
	if spans[len(spans)-1].start != lastStart {
		spans = append(spans, sampleRange{start: lastStart, end: length})
	}

	return spans
}

func squaredPrefix(samples []float32) []float64 {
	prefix := make([]float64, len(samples)+1)
	for i, sample := range samples {
		value := float64(sample)
		prefix[i+1] = prefix[i] + value*value
	}
	return prefix
}

func rangeDBFS(prefix []float64, span sampleRange) float64 {
	if span.end <= span.start {
		return math.Inf(-1)
	}
	meanSquare := (prefix[span.end] - prefix[span.start]) / float64(span.end-span.start)
	if meanSquare <= 0 {
		return math.Inf(-1)
	}
	return 10 * math.Log10(meanSquare)
}

func secondsToSamples(sampleRate int, seconds float64) int {
	return int(math.Round(float64(sampleRate) * seconds))
}

func intersectRanges(a, b sampleRange) (sampleRange, bool) {
	start := maxInt(a.start, b.start)
	end := minInt(a.end, b.end)
	if end < start {
		return sampleRange{}, false
	}
	return sampleRange{start: start, end: end}, true
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
