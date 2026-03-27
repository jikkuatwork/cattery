package qwen

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/unicode/norm"
)

// Tokenizer loads and runs the Qwen byte-level BPE tokenizer.
type Tokenizer struct {
	vocab       map[string]int
	idToToken   map[int]string
	bpeRanks    map[string]int
	addedToID   map[string]int
	idToAdded   map[int]string
	addedTokens []string
	splitRe     *regexp2.Regexp
	byteEncoder [256]string
	byteDecoder map[rune]byte
	cache       map[string][]int
}

type tokenizerJSON struct {
	PreTokenizer struct {
		Pretokenizers []struct {
			Type    string `json:"type"`
			Pattern struct {
				Regex string `json:"Regex"`
			} `json:"pattern"`
		} `json:"pretokenizers"`
	} `json:"pre_tokenizer"`
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
	Model struct {
		Vocab  map[string]int `json:"vocab"`
		Merges []any          `json:"merges"`
	} `json:"model"`
}

type tokenSegment struct {
	text    string
	isAdded bool
}

// LoadTokenizer loads tokenizer metadata from tokenizer.json.
func LoadTokenizer(path string) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta tokenizerJSON
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	regex := ""
	for _, pre := range meta.PreTokenizer.Pretokenizers {
		if pre.Type == "Split" {
			regex = pre.Pattern.Regex
			break
		}
	}
	if regex == "" {
		return nil, fmt.Errorf("tokenizer missing split regex")
	}

	splitRe, err := regexp2.Compile(regex, 0)
	if err != nil {
		return nil, fmt.Errorf("compile split regex: %w", err)
	}

	idToToken := make(map[int]string, len(meta.Model.Vocab)+len(meta.AddedTokens))
	for token, id := range meta.Model.Vocab {
		idToToken[id] = token
	}

	bpeRanks := make(map[string]int, len(meta.Model.Merges))
	for i, raw := range meta.Model.Merges {
		pair, err := mergePair(raw)
		if err != nil {
			return nil, fmt.Errorf("parse merge %d: %w", i, err)
		}
		bpeRanks[pairKey(pair[0], pair[1])] = i
	}

	addedToID := make(map[string]int, len(meta.AddedTokens))
	idToAdded := make(map[int]string, len(meta.AddedTokens))
	addedTokens := make([]string, 0, len(meta.AddedTokens))
	for _, item := range meta.AddedTokens {
		addedToID[item.Content] = item.ID
		idToAdded[item.ID] = item.Content
		idToToken[item.ID] = item.Content
		addedTokens = append(addedTokens, item.Content)
	}
	sort.Slice(addedTokens, func(i, j int) bool {
		if len(addedTokens[i]) == len(addedTokens[j]) {
			return addedTokens[i] < addedTokens[j]
		}
		return len(addedTokens[i]) > len(addedTokens[j])
	})

	byteEncoder, byteDecoder := buildByteTables()
	return &Tokenizer{
		vocab:       meta.Model.Vocab,
		idToToken:   idToToken,
		bpeRanks:    bpeRanks,
		addedToID:   addedToID,
		idToAdded:   idToAdded,
		addedTokens: addedTokens,
		splitRe:     splitRe,
		byteEncoder: byteEncoder,
		byteDecoder: byteDecoder,
		cache:       make(map[string][]int),
	}, nil
}

// Encode tokenizes input text.
func (t *Tokenizer) Encode(input string) []int {
	ids, err := t.encode(input)
	if err != nil {
		return nil
	}
	return ids
}

// Decode turns token IDs back into text.
func (t *Tokenizer) Decode(ids []int) string {
	text, err := t.decode(ids)
	if err != nil {
		return ""
	}
	return text
}

// FormatChat renders the minimal ChatML prompt shape Qwen expects.
func FormatChat(system, user string) string {
	var b strings.Builder
	if system != "" {
		b.WriteString("<|im_start|>system\n")
		b.WriteString(system)
		b.WriteString("<|im_end|>\n")
	}
	b.WriteString("<|im_start|>user\n")
	b.WriteString(user)
	b.WriteString("<|im_end|>\n")
	b.WriteString("<|im_start|>assistant\n")
	return b.String()
}

func (t *Tokenizer) encode(input string) ([]int, error) {
	input = norm.NFC.String(input)
	parts := t.splitAddedTokens(input)

	var ids []int
	for _, part := range parts {
		if part.isAdded {
			ids = append(ids, t.addedToID[part.text])
			continue
		}

		pieceIDs, err := t.encodeOrdinary(part.text)
		if err != nil {
			return nil, err
		}
		ids = append(ids, pieceIDs...)
	}
	return ids, nil
}

func (t *Tokenizer) decode(ids []int) (string, error) {
	var out strings.Builder
	var buf []byte

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if !utf8.Valid(buf) {
			return fmt.Errorf("decoded bytes are not valid utf-8")
		}
		out.Write(buf)
		buf = buf[:0]
		return nil
	}

	for _, id := range ids {
		if token, ok := t.idToAdded[id]; ok {
			if err := flush(); err != nil {
				return "", err
			}
			out.WriteString(token)
			continue
		}

		token, ok := t.idToToken[id]
		if !ok {
			return "", fmt.Errorf("unknown token id %d", id)
		}
		for _, r := range token {
			b, ok := t.byteDecoder[r]
			if !ok {
				return "", fmt.Errorf("token %q contains unmapped rune %q", token, r)
			}
			buf = append(buf, b)
		}
	}

	if err := flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (t *Tokenizer) encodeOrdinary(input string) ([]int, error) {
	if input == "" {
		return nil, nil
	}

	var ids []int
	match, err := t.splitRe.FindStringMatch(input)
	if err != nil {
		return nil, err
	}

	lastRune := 0
	for match != nil {
		start := runeIndexToByteOffset(input, match.Index)
		if match.Index > lastRune {
			missed := input[runeIndexToByteOffset(input, lastRune):start]
			missedIDs, err := t.encodeChunk(missed)
			if err != nil {
				return nil, err
			}
			ids = append(ids, missedIDs...)
		}

		chunk := input[start:runeIndexToByteOffset(input, match.Index+match.Length)]
		chunkIDs, err := t.encodeChunk(chunk)
		if err != nil {
			return nil, err
		}
		ids = append(ids, chunkIDs...)

		lastRune = match.Index + match.Length
		match, err = t.splitRe.FindNextMatch(match)
		if err != nil {
			return nil, err
		}
	}

	totalRunes := utf8.RuneCountInString(input)
	if lastRune < totalRunes {
		tail := input[runeIndexToByteOffset(input, lastRune):]
		tailIDs, err := t.encodeChunk(tail)
		if err != nil {
			return nil, err
		}
		ids = append(ids, tailIDs...)
	}

	return ids, nil
}

func (t *Tokenizer) encodeChunk(chunk string) ([]int, error) {
	if chunk == "" {
		return nil, nil
	}

	encoded := t.byteEncode(chunk)
	if cached, ok := t.cache[encoded]; ok {
		return append([]int(nil), cached...), nil
	}

	symbols := splitRunes(encoded)
	for {
		bestRank := -1
		bestIdx := -1
		for i := 0; i < len(symbols)-1; i++ {
			rank, ok := t.bpeRanks[pairKey(symbols[i], symbols[i+1])]
			if !ok {
				continue
			}
			if bestRank == -1 || rank < bestRank {
				bestRank = rank
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			break
		}

		merged := make([]string, 0, len(symbols)-1)
		for i := 0; i < len(symbols); {
			if i == bestIdx && i < len(symbols)-1 {
				merged = append(merged, symbols[i]+symbols[i+1])
				i += 2
				continue
			}
			merged = append(merged, symbols[i])
			i++
		}
		symbols = merged
	}

	ids := make([]int, len(symbols))
	for i, symbol := range symbols {
		id, ok := t.vocab[symbol]
		if !ok {
			return nil, fmt.Errorf("token %q missing from vocab", symbol)
		}
		ids[i] = id
	}
	t.cache[encoded] = append([]int(nil), ids...)
	return ids, nil
}

func (t *Tokenizer) splitAddedTokens(input string) []tokenSegment {
	if input == "" {
		return nil
	}

	var parts []tokenSegment
	for len(input) > 0 {
		nextIdx := -1
		nextToken := ""
		for _, token := range t.addedTokens {
			idx := strings.Index(input, token)
			if idx == -1 {
				continue
			}
			if nextIdx == -1 || idx < nextIdx || (idx == nextIdx && len(token) > len(nextToken)) {
				nextIdx = idx
				nextToken = token
			}
		}

		if nextIdx == -1 {
			parts = append(parts, tokenSegment{text: input})
			break
		}
		if nextIdx > 0 {
			parts = append(parts, tokenSegment{text: input[:nextIdx]})
		}
		parts = append(parts, tokenSegment{text: nextToken, isAdded: true})
		input = input[nextIdx+len(nextToken):]
	}
	return parts
}

func (t *Tokenizer) byteEncode(s string) string {
	var b strings.Builder
	for _, raw := range []byte(s) {
		b.WriteString(t.byteEncoder[raw])
	}
	return b.String()
}

func buildByteTables() ([256]string, map[rune]byte) {
	var bs []int
	for i := int('!'); i <= int('~'); i++ {
		bs = append(bs, i)
	}
	for i := int(0xA1); i <= int(0xAC); i++ {
		bs = append(bs, i)
	}
	for i := int(0xAE); i <= int(0xFF); i++ {
		bs = append(bs, i)
	}

	cs := append([]int(nil), bs...)
	extra := 0
	for b := 0; b < 256; b++ {
		if containsInt(bs, b) {
			continue
		}
		bs = append(bs, b)
		cs = append(cs, 256+extra)
		extra++
	}

	var encoder [256]string
	decoder := make(map[rune]byte, 256)
	for i, b := range bs {
		r := rune(cs[i])
		encoder[byte(b)] = string(r)
		decoder[r] = byte(b)
	}
	return encoder, decoder
}

func mergePair(raw any) ([2]string, error) {
	switch pair := raw.(type) {
	case string:
		parts := strings.SplitN(pair, " ", 2)
		if len(parts) != 2 {
			return [2]string{}, fmt.Errorf("invalid merge string %q", pair)
		}
		return [2]string{parts[0], parts[1]}, nil
	case []any:
		if len(pair) != 2 {
			return [2]string{}, fmt.Errorf("expected pair len 2, got %d", len(pair))
		}
		left, ok := pair[0].(string)
		if !ok {
			return [2]string{}, fmt.Errorf("left merge token is %T", pair[0])
		}
		right, ok := pair[1].(string)
		if !ok {
			return [2]string{}, fmt.Errorf("right merge token is %T", pair[1])
		}
		return [2]string{left, right}, nil
	default:
		return [2]string{}, fmt.Errorf("unsupported merge type %T", raw)
	}
}

func pairKey(left, right string) string {
	return left + "\x00" + right
}

func splitRunes(s string) []string {
	out := make([]string, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func runeIndexToByteOffset(s string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	byteOffset := 0
	for i := 0; i < runeIndex && byteOffset < len(s); i++ {
		_, size := utf8.DecodeRuneInString(s[byteOffset:])
		byteOffset += size
	}
	return byteOffset
}
