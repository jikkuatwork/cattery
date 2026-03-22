package kokoro

import (
	"strings"
	"unicode"
)

const (
	// Leave room for the 2 padding tokens added in synthesize().
	chunkTokenLimit = 480
	chunkGapMillis  = 75
)

type phonemePiece struct {
	text      string
	sepBefore string
}

func chunkPhonemes(phonemes string, maxTokens int) []string {
	if maxTokens <= 0 {
		return nil
	}

	pieces := splitPhonemePieces(strings.TrimSpace(phonemes), maxTokens, "")
	chunks := packPhonemePieces(pieces, maxTokens)

	filtered := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if tokenCount(chunk) > 0 {
			filtered = append(filtered, chunk)
		}
	}

	return filtered
}

func splitPhonemePieces(text string, maxTokens int, sepBefore string) []phonemePiece {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if tokenCount(text) <= maxTokens {
		return []phonemePiece{{text: text, sepBefore: sepBefore}}
	}

	if parts := splitSentenceParts(text); len(parts) > 1 {
		return splitNestedPhonemePieces(parts, maxTokens, sepBefore, " ")
	}
	if parts := splitClauseParts(text); len(parts) > 1 {
		return splitNestedPhonemePieces(parts, maxTokens, sepBefore, " ")
	}

	words := strings.Fields(text)
	if len(words) > 1 {
		return splitNestedPhonemePieces(words, maxTokens, sepBefore, " ")
	}

	return splitTokenPieces(text, maxTokens, sepBefore)
}

func splitNestedPhonemePieces(parts []string, maxTokens int, firstSep, nextSep string) []phonemePiece {
	var pieces []phonemePiece
	for i, part := range parts {
		sep := nextSep
		if i == 0 {
			sep = firstSep
		}
		pieces = append(pieces, splitPhonemePieces(part, maxTokens, sep)...)
	}
	return pieces
}

func splitSentenceParts(text string) []string {
	return splitOnBoundary(text, func(r, next rune, atEnd bool) bool {
		if r != '.' && r != '!' && r != '?' {
			return false
		}
		return atEnd || unicode.IsSpace(next)
	})
}

func splitClauseParts(text string) []string {
	return splitOnBoundary(text, func(r, _ rune, _ bool) bool {
		switch r {
		case ',', ';', ':', '\u2014', '\u2026':
			return true
		default:
			return false
		}
	})
}

func splitOnBoundary(text string, isBoundary func(r, next rune, atEnd bool) bool) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	var parts []string
	start := 0
	for i, r := range runes {
		next := rune(0)
		atEnd := i == len(runes)-1
		if !atEnd {
			next = runes[i+1]
		}
		if !isBoundary(r, next, atEnd) {
			continue
		}

		if part := strings.TrimSpace(string(runes[start : i+1])); part != "" {
			parts = append(parts, part)
		}
		start = i + 1
	}

	if part := strings.TrimSpace(string(runes[start:])); part != "" {
		parts = append(parts, part)
	}
	if len(parts) <= 1 {
		return nil
	}

	return parts
}

func splitTokenPieces(text string, maxTokens int, sepBefore string) []phonemePiece {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	var pieces []phonemePiece
	start := 0
	count := 0
	nextSep := sepBefore

	for i, r := range runes {
		delta := 0
		if _, ok := vocab[r]; ok {
			delta = 1
		}

		if count > 0 && count+delta > maxTokens {
			if piece := strings.TrimSpace(string(runes[start:i])); piece != "" {
				pieces = append(pieces, phonemePiece{text: piece, sepBefore: nextSep})
				nextSep = ""
			}
			start = i
			count = 0
		}

		count += delta
	}

	if piece := strings.TrimSpace(string(runes[start:])); piece != "" {
		pieces = append(pieces, phonemePiece{text: piece, sepBefore: nextSep})
	}

	return pieces
}

func packPhonemePieces(pieces []phonemePiece, maxTokens int) []string {
	var chunks []string
	var current string
	currentTokens := 0

	for _, piece := range pieces {
		piece = trimPiece(piece)
		if piece.text == "" {
			continue
		}

		if current == "" {
			current = piece.text
			currentTokens = tokenCount(piece.text)
			continue
		}

		addition := piece.sepBefore + piece.text
		additionTokens := tokenCount(addition)
		if currentTokens+additionTokens > maxTokens {
			chunks = append(chunks, current)
			current = piece.text
			currentTokens = tokenCount(piece.text)
			continue
		}

		current += addition
		currentTokens += additionTokens
	}

	if current != "" {
		chunks = append(chunks, current)
	}

	return chunks
}

func trimPiece(piece phonemePiece) phonemePiece {
	piece.text = strings.TrimSpace(piece.text)
	if piece.text == "" {
		piece.sepBefore = ""
	}
	return piece
}

func tokenCount(phonemes string) int {
	return len(tokenize(phonemes))
}
