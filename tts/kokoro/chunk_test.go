package kokoro

import (
	"strings"
	"testing"
)

func TestChunkPhonemesKeepsShortInput(t *testing.T) {
	input := "aaaa."

	got := chunkPhonemes(input, 10)

	assertChunks(t, got, []string{input}, 10)
}

func TestChunkPhonemesSplitsOnSentenceBoundaries(t *testing.T) {
	input := "aaaa. bbbb?"

	got := chunkPhonemes(input, 8)

	assertChunks(t, got, []string{"aaaa.", "bbbb?"}, 8)
}

func TestChunkPhonemesSplitsOnClauseBoundaries(t *testing.T) {
	input := "aaaa, bbbb, cccc."

	got := chunkPhonemes(input, 11)

	assertChunks(t, got, []string{"aaaa, bbbb,", "cccc."}, 11)
}

func TestChunkPhonemesSplitsOnWordBoundaries(t *testing.T) {
	input := "aaaa bbbb cccc"

	got := chunkPhonemes(input, 9)

	assertChunks(t, got, []string{"aaaa bbbb", "cccc"}, 9)
}

func TestChunkPhonemesSplitsOversizeWordByTokenBudget(t *testing.T) {
	input := strings.Repeat("a", 25)

	got := chunkPhonemes(input, 10)

	assertChunks(t, got, []string{
		strings.Repeat("a", 10),
		strings.Repeat("a", 10),
		strings.Repeat("a", 5),
	}, 10)
}

func assertChunks(t *testing.T, got, want []string, maxTokens int) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(chunks) = %d, want %d (%q)", len(got), len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk[%d] = %q, want %q", i, got[i], want[i])
		}
		if tokens := tokenCount(got[i]); tokens > maxTokens {
			t.Fatalf("chunk[%d] has %d tokens, want <= %d", i, tokens, maxTokens)
		}
	}
}
