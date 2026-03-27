package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/llm/qwen"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
)

type ChatCompletionRequest struct {
	Model       string        `json:"model,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatCompletionChunkResponse struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []chatCompletionChunkChoice `json:"choices"`
}

type chatCompletionChunkChoice struct {
	Index        int                 `json:"index"`
	Delta        chatCompletionDelta `json:"delta"`
	FinishReason string              `json:"finish_reason,omitempty"`
}

type chatCompletionDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	systemPrompt, userPrompt, err := extractChatPrompts(req.Messages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	model, err := s.resolveLLMModel(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	eng, err := s.borrowLLM(r.Context())
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusServiceUnavailable,
				fmt.Sprintf("queue full (%d max), try again shortly", s.cfg.QueueMax))
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		s.failed.Add(1)
		writeError(w, http.StatusInternalServerError, "completion failed")
		return
	}
	defer s.llmPool.Return(eng)

	var result *llm.Result
	err = preflight.GuardMemoryError("text generation", func() error {
		var innerErr error
		result, innerErr = eng.Generate(r.Context(), userPrompt, llm.Options{
			System:    systemPrompt,
			MaxTokens: req.MaxTokens,
		})
		return innerErr
	})
	if err != nil {
		s.failed.Add(1)
		if preflight.IsMemoryError(err) {
			w.Header().Set("Retry-After", "30")
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		writeError(w, http.StatusInternalServerError, "completion failed")
		return
	}
	if result == nil {
		s.failed.Add(1)
		writeError(w, http.StatusInternalServerError, "completion failed")
		return
	}

	s.processed.Add(1)

	responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	if req.Stream {
		s.streamChatCompletion(w, r, responseID, created, model, result)
		return
	}

	writeJSON(w, http.StatusOK, ChatCompletionResponse{
		ID:      responseID,
		Object:  "chat.completion",
		Created: created,
		Model:   model.ID,
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: result.Text,
				},
				FinishReason: normalizeFinishReason(result.FinishReason),
			},
		},
		Usage: buildUsage(result),
	})
}

func (s *Server) streamChatCompletion(
	w http.ResponseWriter,
	r *http.Request,
	responseID string,
	created int64,
	model *registry.Model,
	result *llm.Result,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if err := writeSSEData(w, chatCompletionChunkResponse{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model.ID,
		Choices: []chatCompletionChunkChoice{
			{
				Index: 0,
				Delta: chatCompletionDelta{Role: "assistant"},
			},
		},
	}); err != nil {
		return
	}
	flusher.Flush()

	for _, delta := range splitStreamDeltas(result.Text) {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		if err := writeSSEData(w, chatCompletionChunkResponse{
			ID:      responseID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model.ID,
			Choices: []chatCompletionChunkChoice{
				{
					Index: 0,
					Delta: chatCompletionDelta{Content: delta},
				},
			},
		}); err != nil {
			return
		}
		flusher.Flush()
	}

	_ = writeSSEData(w, chatCompletionChunkResponse{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model.ID,
		Choices: []chatCompletionChunkChoice{
			{
				Index:        0,
				Delta:        chatCompletionDelta{},
				FinishReason: normalizeFinishReason(result.FinishReason),
			},
		},
	})
	flusher.Flush()
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) resolveLLMModel(ref string) (*registry.Model, error) {
	return resolveConfiguredModel(registry.KindLLM, ref, s.llmModel, "llm")
}

func newLLMEngine(model *registry.Model, dataDir string) (llm.Engine, error) {
	if model == nil {
		return nil, fmt.Errorf("missing LLM model")
	}

	switch model.ID {
	case "qwen3.5-4b-v1.0":
		return qwen.New(paths.ModelDir(dataDir, model.ID), model)
	default:
		return nil, fmt.Errorf("LLM model %q is not supported yet", model.ID)
	}
}

func extractChatPrompts(messages []ChatMessage) (string, string, error) {
	if len(messages) == 0 {
		return "", "", fmt.Errorf("messages are required")
	}

	var systemPrompt string
	var userPrompt string
	for _, message := range messages {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch role {
		case "system":
			systemPrompt = content
		case "user":
			userPrompt = content
		}
	}

	if userPrompt == "" {
		return "", "", fmt.Errorf("messages must include a user message")
	}
	return systemPrompt, userPrompt, nil
}

func normalizeFinishReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "stop"
	}
	return reason
}

func buildUsage(result *llm.Result) ChatCompletionUsage {
	if result == nil {
		return ChatCompletionUsage{}
	}
	completionTokens := result.TokensUsed
	if completionTokens < 0 {
		completionTokens = 0
	}
	return ChatCompletionUsage{
		PromptTokens:     0,
		CompletionTokens: completionTokens,
		TotalTokens:      completionTokens,
	}
}

func splitStreamDeltas(text string) []string {
	if text == "" {
		return nil
	}

	const chunkSize = 32
	runes := []rune(text)
	deltas := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		deltas = append(deltas, string(runes[start:end]))
	}
	return deltas
}

func writeSSEData(w http.ResponseWriter, payload any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(buf.String()))
	return err
}
