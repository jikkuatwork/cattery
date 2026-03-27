package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/unicode/norm"
)

const (
	tokenizerJSONURL   = "https://huggingface.co/onnx-community/Qwen3.5-4B-ONNX/resolve/main/tokenizer.json"
	tokenizerConfigURL = "https://huggingface.co/onnx-community/Qwen3.5-4B-ONNX/resolve/main/tokenizer_config.json"
	modelDir           = "models-data/qwen3.5-4b-v1.0"
	tokenizerJSONPath  = modelDir + "/tokenizer.json"
	tokenizerCfgPath   = modelDir + "/tokenizer_config.json"
)

var expectedSpecialIDs = map[string]int{
	"<|endoftext|>": 248044,
	"<|im_start|>":  248045,
	"<|im_end|>":    248046,
}

type hfTokenizerJSON struct {
	Normalizer struct {
		Type string `json:"type"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type          string `json:"type"`
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
		Special bool   `json:"special"`
	} `json:"added_tokens"`
	Model struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []any          `json:"merges"`
	} `json:"model"`
	Decoder struct {
		Type string `json:"type"`
	} `json:"decoder"`
}

type hfTokenizerConfig struct {
	TokenizerClass string `json:"tokenizer_class"`
}

type qwenTokenizer struct {
	vocab         map[string]int
	idToToken     map[int]string
	bpeRanks      map[string]int
	specialToID   map[string]int
	idToSpecial   map[int]string
	specialTokens []string
	splitRe       *regexp2.Regexp
	byteEncoder   [256]string
	byteDecoder   map[rune]byte
	cache         map[string][]int
}

func main() {
	if _, err := ensureFile(tokenizerJSONURL, tokenizerJSONPath); err != nil {
		fatalf("download tokenizer.json: %v", err)
	}
	if _, err := ensureFile(tokenizerConfigURL, tokenizerCfgPath); err != nil {
		fatalf("download tokenizer_config.json: %v", err)
	}

	tk, meta, cfg, err := loadTokenizer(tokenizerJSONPath, tokenizerCfgPath)
	if err != nil {
		fatalf("load tokenizer: %v", err)
	}

	fmt.Printf("tokenizer.json: %s\n", tokenizerJSONPath)
	fmt.Printf("tokenizer_config.json tokenizer_class=%q\n", cfg.TokenizerClass)
	fmt.Printf("model=%s decoder=%s normalizer=%s vocab=%d merges=%d\n",
		meta.Model.Type, meta.Decoder.Type, meta.Normalizer.Type, len(meta.Model.Vocab), len(meta.Model.Merges))
	fmt.Println("special token IDs:")
	for _, token := range []string{"<|im_start|>", "<|im_end|>", "<|endoftext|>"} {
		got, ok := tk.specialToID[token]
		want := expectedSpecialIDs[token]
		fmt.Printf("  %s => %d (found=%v match=%v)\n", token, got, ok, ok && got == want)
	}

	prompts := []struct {
		name   string
		prompt string
	}{
		{name: "basic", prompt: "Hello, world!"},
		{name: "query", prompt: "What is the capital of France?"},
		{name: "code", prompt: "func main() { fmt.Println(\"hello\") }"},
		{name: "unicode", prompt: "日本語のテスト"},
		{name: "chat-special", prompt: "<|im_start|>system\nYou are helpful.<|im_end|>\n<|im_start|>user\nHi<|im_end|>\n<|im_start|>assistant\n"},
		{name: "chat-template", prompt: formatQwenChat("You are helpful.", "Hi")},
	}

	for _, tc := range prompts {
		ids, tokens, err := tk.Encode(tc.prompt)
		if err != nil {
			fatalf("%s encode: %v", tc.name, err)
		}
		decoded, err := tk.Decode(ids)
		if err != nil {
			fatalf("%s decode: %v", tc.name, err)
		}

		fmt.Printf("\n== %s ==\n", tc.name)
		fmt.Printf("prompt: %q\n", tc.prompt)
		fmt.Printf("token IDs: %v\n", ids)
		fmt.Printf("tokens: %q\n", tokens)
		fmt.Printf("decoded: %q\n", decoded)
		fmt.Printf("round-trip match: %v\n", decoded == tc.prompt)

		if strings.Contains(tc.prompt, "<|im_") {
			verifySpecials(tc.prompt, ids, tk.specialToID)
		}
	}
}

func loadTokenizer(tokenizerPath, cfgPath string) (*qwenTokenizer, *hfTokenizerJSON, *hfTokenizerConfig, error) {
	tokenizerData, err := os.ReadFile(tokenizerPath)
	if err != nil {
		return nil, nil, nil, err
	}
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, nil, nil, err
	}

	var meta hfTokenizerJSON
	if err := json.Unmarshal(tokenizerData, &meta); err != nil {
		return nil, nil, nil, err
	}
	var cfg hfTokenizerConfig
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		return nil, nil, nil, err
	}

	regex := ""
	for _, pt := range meta.PreTokenizer.Pretokenizers {
		if pt.Type == "Split" {
			regex = pt.Pattern.Regex
			break
		}
	}
	if regex == "" {
		return nil, nil, nil, fmt.Errorf("no split regex found")
	}

	splitRe, err := regexp2.Compile(regex, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compile split regex: %w", err)
	}

	byteEncoder, byteDecoder := buildByteTables()
	idToToken := make(map[int]string, len(meta.Model.Vocab))
	for token, id := range meta.Model.Vocab {
		idToToken[id] = token
	}

	bpeRanks := make(map[string]int, len(meta.Model.Merges))
	for i, raw := range meta.Model.Merges {
		pair, err := mergePair(raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse merge %d: %w", i, err)
		}
		bpeRanks[pairKey(pair[0], pair[1])] = i
	}

	specialToID := make(map[string]int)
	idToSpecial := make(map[int]string)
	var specialTokens []string
	for _, item := range meta.AddedTokens {
		if !item.Special {
			continue
		}
		specialToID[item.Content] = item.ID
		idToSpecial[item.ID] = item.Content
		specialTokens = append(specialTokens, item.Content)
	}
	sort.Slice(specialTokens, func(i, j int) bool {
		if len(specialTokens[i]) == len(specialTokens[j]) {
			return specialTokens[i] < specialTokens[j]
		}
		return len(specialTokens[i]) > len(specialTokens[j])
	})

	return &qwenTokenizer{
		vocab:         meta.Model.Vocab,
		idToToken:     idToToken,
		bpeRanks:      bpeRanks,
		specialToID:   specialToID,
		idToSpecial:   idToSpecial,
		specialTokens: specialTokens,
		splitRe:       splitRe,
		byteEncoder:   byteEncoder,
		byteDecoder:   byteDecoder,
		cache:         make(map[string][]int),
	}, &meta, &cfg, nil
}

func (t *qwenTokenizer) Encode(input string) ([]int, []string, error) {
	input = norm.NFC.String(input)
	parts := t.splitSpecialTokens(input)

	var ids []int
	var tokens []string
	for _, part := range parts {
		if part.isSpecial {
			id := t.specialToID[part.text]
			ids = append(ids, id)
			tokens = append(tokens, part.text)
			continue
		}
		pieceIDs, pieceTokens, err := t.encodeOrdinary(part.text)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, pieceIDs...)
		tokens = append(tokens, pieceTokens...)
	}

	return ids, tokens, nil
}

func (t *qwenTokenizer) encodeOrdinary(input string) ([]int, []string, error) {
	if input == "" {
		return nil, nil, nil
	}

	var ids []int
	var tokens []string
	m, err := t.splitRe.FindStringMatch(input)
	if err != nil {
		return nil, nil, err
	}

	lastRune := 0
	for m != nil {
		matchStart := runeIndexToByteOffset(input, m.Index)

		if m.Index > lastRune {
			missed := input[runeIndexToByteOffset(input, lastRune):matchStart]
			missedIDs, missedTokens, err := t.encodeChunk(missed)
			if err != nil {
				return nil, nil, err
			}
			ids = append(ids, missedIDs...)
			tokens = append(tokens, missedTokens...)
		}

		chunkIDs, chunkTokens, err := t.encodeChunk(m.String())
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, chunkIDs...)
		tokens = append(tokens, chunkTokens...)

		lastRune = m.Index + m.Length
		m, err = t.splitRe.FindNextMatch(m)
		if err != nil {
			return nil, nil, err
		}
	}

	totalRunes := utf8.RuneCountInString(input)
	if lastRune < totalRunes {
		trailing := input[runeIndexToByteOffset(input, lastRune):]
		trailingIDs, trailingTokens, err := t.encodeChunk(trailing)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, trailingIDs...)
		tokens = append(tokens, trailingTokens...)
	}

	return ids, tokens, nil
}

func (t *qwenTokenizer) encodeChunk(chunk string) ([]int, []string, error) {
	if chunk == "" {
		return nil, nil, nil
	}
	encoded := t.byteEncode(chunk)
	ids, err := t.bpeEncode(encoded)
	if err != nil {
		return nil, nil, err
	}
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = t.idToToken[id]
	}
	return ids, tokens, nil
}

func (t *qwenTokenizer) bpeEncode(piece string) ([]int, error) {
	if cached, ok := t.cache[piece]; ok {
		out := make([]int, len(cached))
		copy(out, cached)
		return out, nil
	}

	symbols := splitRunes(piece)
	if len(symbols) == 0 {
		return nil, nil
	}

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
			if i < len(symbols)-1 && i == bestIdx {
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

	t.cache[piece] = append([]int(nil), ids...)
	return ids, nil
}

func (t *qwenTokenizer) Decode(ids []int) (string, error) {
	var out strings.Builder
	var buf []byte

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if !utf8.Valid(buf) {
			return fmt.Errorf("decoded bytes are not valid utf-8: %v", buf)
		}
		out.Write(buf)
		buf = buf[:0]
		return nil
	}

	for _, id := range ids {
		if special, ok := t.idToSpecial[id]; ok {
			if err := flush(); err != nil {
				return "", err
			}
			out.WriteString(special)
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

type segment struct {
	text      string
	isSpecial bool
}

func (t *qwenTokenizer) splitSpecialTokens(input string) []segment {
	var parts []segment
	for len(input) > 0 {
		nextIdx := -1
		nextToken := ""
		for _, tok := range t.specialTokens {
			idx := strings.Index(input, tok)
			if idx == -1 {
				continue
			}
			if nextIdx == -1 || idx < nextIdx || (idx == nextIdx && len(tok) > len(nextToken)) {
				nextIdx = idx
				nextToken = tok
			}
		}

		if nextIdx == -1 {
			parts = append(parts, segment{text: input})
			break
		}
		if nextIdx > 0 {
			parts = append(parts, segment{text: input[:nextIdx]})
		}
		parts = append(parts, segment{text: nextToken, isSpecial: true})
		input = input[nextIdx+len(nextToken):]
	}
	return parts
}

func (t *qwenTokenizer) byteEncode(s string) string {
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
	n := 0
	for b := 0; b < 256; b++ {
		if !containsInt(bs, b) {
			bs = append(bs, b)
			cs = append(cs, 256+n)
			n++
		}
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
	switch v := raw.(type) {
	case string:
		parts := strings.SplitN(v, " ", 2)
		if len(parts) != 2 {
			return [2]string{}, fmt.Errorf("invalid merge string %q", v)
		}
		return [2]string{parts[0], parts[1]}, nil
	case []any:
		if len(v) != 2 {
			return [2]string{}, fmt.Errorf("invalid merge %#v", v)
		}
		left, lok := v[0].(string)
		right, rok := v[1].(string)
		if !lok || !rok {
			return [2]string{}, fmt.Errorf("invalid merge %#v", v)
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

func verifySpecials(prompt string, ids []int, specialToID map[string]int) {
	for _, token := range []string{"<|im_start|>", "<|im_end|>", "<|endoftext|>"} {
		if !strings.Contains(prompt, token) {
			continue
		}
		id := specialToID[token]
		fmt.Printf("contains %s id=%d encoded=%v\n", token, id, containsInt(ids, id))
	}
}

func formatQwenChat(systemMessage, userMessage string) string {
	return "<|im_start|>system\n" + systemMessage + "<|im_end|>\n" +
		"<|im_start|>user\n" + userMessage + "<|im_end|>\n" +
		"<|im_start|>assistant\n"
}

func ensureFile(url, dst string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}

	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
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
