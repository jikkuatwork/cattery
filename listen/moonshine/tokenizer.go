package moonshine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func loadTokenizer(path string) (map[int]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tok struct {
		Model struct {
			Vocab map[string]int `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}

	vocab := make(map[int]string, len(tok.Model.Vocab))
	for s, id := range tok.Model.Vocab {
		vocab[id] = s
	}
	return vocab, nil
}

func decodeTokens(vocab map[int]string, ids []int64) string {
	if len(ids) == 0 {
		return ""
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if piece, ok := vocab[int(id)]; ok {
			parts = append(parts, piece)
		}
	}

	text := strings.Join(parts, "")
	text = strings.ReplaceAll(text, "▁", " ")
	return strings.TrimSpace(text)
}
