package moonshine

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

const (
	defaultChunkTarget       = 30 * time.Second
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

type chunkPlan struct {
	chunk     sampleRange
	nextStart int
	threshold float64
	needMore  bool
	final     bool
}

func planAudioChunks(samples []float32, sampleRate int) []sampleRange {
	return planAudioChunksWithTarget(samples, sampleRate, 0)
}

func planAudioChunksWithTarget(samples []float32, sampleRate int, target time.Duration) []sampleRange {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}

	var chunks []sampleRange
	for offset := 0; offset < len(samples); {
		plan := planNextChunkWithTarget(samples[offset:], sampleRate, target, true)
		if plan.chunk.end <= plan.chunk.start {
			break
		}
		chunks = append(chunks, sampleRange{
			start: offset + plan.chunk.start,
			end:   offset + plan.chunk.end,
		})
		if plan.final {
			break
		}
		offset += plan.nextStart
	}
	return chunks
}

func planNextChunk(samples []float32, sampleRate int, atEOF bool) chunkPlan {
	return planNextChunkWithTarget(samples, sampleRate, 0, atEOF)
}

func planNextChunkWithTarget(
	samples []float32,
	sampleRate int,
	target time.Duration,
	atEOF bool,
) chunkPlan {
	return planNextChunkWithThreshold(samples, sampleRate, silenceFloorFor(samples), target, atEOF)
}

func planNextChunkWithThreshold(
	samples []float32,
	sampleRate int,
	threshold float64,
	target time.Duration,
	atEOF bool,
) chunkPlan {
	if len(samples) == 0 || sampleRate <= 0 {
		return chunkPlan{threshold: threshold, needMore: !atEOF}
	}

	target = normalizeChunkTarget(target)
	search := secondsToSamples(sampleRate, chunkSearchWindowSeconds)
	targetSamples := secondsToSamples(sampleRate, target.Seconds())
	overlap := secondsToSamples(sampleRate, chunkOverlapSeconds)

	if !atEOF && len(samples) < targetSamples+search {
		return chunkPlan{
			threshold: threshold,
			needMore:  true,
		}
	}

	if len(samples) <= targetSamples {
		return chunkPlan{
			chunk:     sampleRange{start: 0, end: len(samples)},
			nextStart: len(samples),
			threshold: threshold,
			final:     atEOF,
		}
	}

	cut := targetSamples
	searchSpan := sampleRange{
		start: maxInt(targetSamples-search, 0),
		end:   minInt(targetSamples+search, len(samples)),
	}
	if nearest, ok := nearestSilenceCut(targetSamples, searchSpan, findSilentRuns(samples, sampleRate, threshold)); ok {
		cut = nearest
	}
	if cut <= 0 {
		cut = minInt(1, len(samples))
	}

	nextStart := cut - overlap
	if nextStart <= 0 {
		nextStart = cut
	}

	return chunkPlan{
		chunk:     sampleRange{start: 0, end: cut},
		nextStart: nextStart,
		threshold: threshold,
		final:     atEOF && cut >= len(samples),
	}
}

func transcribeChunkedPCM(
	samples []float32,
	sampleRate int,
	transcribe func([]float32) (string, error),
) (string, error) {
	return transcribeChunkedPCMWithTarget(samples, sampleRate, 0, transcribe)
}

func transcribeChunkedPCMWithTarget(
	samples []float32,
	sampleRate int,
	target time.Duration,
	transcribe func([]float32) (string, error),
) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	if transcribe == nil {
		return "", fmt.Errorf("missing chunk transcriber")
	}

	estChunks := 2
	if sampleRate > 0 {
		targetSamples := secondsToSamples(sampleRate, normalizeChunkTarget(target).Seconds())
		if targetSamples > 0 {
			estChunks = len(samples)/targetSamples + 2
		}
	}
	texts := make([]string, 0, estChunks)
	for offset, chunkIndex := 0, 0; offset < len(samples); {
		plan := planNextChunkWithTarget(samples[offset:], sampleRate, target, true)
		if plan.chunk.end <= plan.chunk.start {
			break
		}

		chunkIndex++
		part := samples[offset+plan.chunk.start : offset+plan.chunk.end]
		if chunkIsSilent(part, sampleRate, plan.threshold) {
			if plan.final {
				break
			}
			offset += plan.nextStart
			continue
		}

		text, err := transcribe(part)
		if err != nil {
			return "", fmt.Errorf("transcribe chunk %d: %w", chunkIndex, err)
		}
		text = strings.TrimSpace(text)
		if text != "" {
			texts = append(texts, text)
		}

		if plan.final {
			break
		}
		offset += plan.nextStart
	}

	return stitchChunkTexts(texts), nil
}

func normalizeChunkTarget(target time.Duration) time.Duration {
	if target <= 0 {
		return defaultChunkTarget
	}
	return target
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
