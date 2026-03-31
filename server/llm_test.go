package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/tts"
)

type fakeLLMEngine struct {
	generate func(context.Context, string, llm.Options) (*llm.Result, error)
	close    func() error
}

func (f *fakeLLMEngine) Generate(ctx context.Context, prompt string, opts llm.Options) (*llm.Result, error) {
	if f.generate != nil {
		return f.generate(ctx, prompt, opts)
	}
	return &llm.Result{Text: "ok", TokensUsed: 1, FinishReason: "stop"}, nil
}

func (f *fakeLLMEngine) Close() error {
	if f.close != nil {
		return f.close()
	}
	return nil
}

type fakeTTSEngine struct{}

func (f *fakeTTSEngine) Speak(_ io.Writer, _ string, _ tts.Options) error { return nil }
func (f *fakeTTSEngine) Voices() []tts.Voice                              { return nil }
func (f *fakeTTSEngine) Close() error                                     { return nil }

type fakeSTTEngine struct{}

func (f *fakeSTTEngine) Transcribe(_ io.Reader, _ stt.Options) (*stt.Result, error) {
	return nil, nil
}

func (f *fakeSTTEngine) SampleRate() int { return 16000 }
func (f *fakeSTTEngine) Close() error    { return nil }

func TestHandleChatCompletions(t *testing.T) {
	model := registry.Default(registry.KindLLM)
	if model == nil {
		t.Fatal("missing default LLM model")
	}

	srv := &Server{
		cfg:      Config{QueueMax: 2},
		llmModel: model,
		llmPool: NewPool(PoolConfig[llm.Engine]{
			Name:      "llm",
			Workers:   1,
			KeepAlive: true,
			Create: func() (llm.Engine, error) {
				return &fakeLLMEngine{
					generate: func(_ context.Context, prompt string, opts llm.Options) (*llm.Result, error) {
						if prompt != "Tell me a joke" {
							t.Fatalf("prompt = %q, want %q", prompt, "Tell me a joke")
						}
						if opts.System != "Be concise." {
							t.Fatalf("system = %q, want %q", opts.System, "Be concise.")
						}
						if opts.MaxTokens != 64 {
							t.Fatalf("max tokens = %d, want 64", opts.MaxTokens)
						}
						return &llm.Result{
							Text:         "A short joke.",
							TokensUsed:   12,
							FinishReason: "stop",
						}, nil
					},
				}, nil
			},
			Close: func(eng llm.Engine) error { return eng.Close() },
		}),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"qwen3.5-4b-v1.0",
		"messages":[
			{"role":"system","content":"Be concise."},
			{"role":"user","content":"Ignore this"},
			{"role":"user","content":"Tell me a joke"}
		],
		"max_tokens":64
	}`))
	rec := httptest.NewRecorder()

	srv.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Fatalf("object = %q, want chat.completion", resp.Object)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Fatalf("role = %q, want assistant", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].Message.Content != "A short joke." {
		t.Fatalf("content = %q, want %q", resp.Choices[0].Message.Content, "A short joke.")
	}
	if resp.Model != model.ID {
		t.Fatalf("model = %q, want %q", resp.Model, model.ID)
	}
}

func TestHandleChatCompletionsStream(t *testing.T) {
	model := registry.Default(registry.KindLLM)
	if model == nil {
		t.Fatal("missing default LLM model")
	}

	srv := &Server{
		cfg:      Config{QueueMax: 2},
		llmModel: model,
		llmPool: NewPool(PoolConfig[llm.Engine]{
			Name:      "llm",
			Workers:   1,
			KeepAlive: true,
			Create: func() (llm.Engine, error) {
				return &fakeLLMEngine{
					generate: func(_ context.Context, _ string, _ llm.Options) (*llm.Result, error) {
						return &llm.Result{
							Text:         "streamed response",
							TokensUsed:   4,
							FinishReason: "stop",
						}, nil
					},
				}, nil
			},
			Close: func(eng llm.Engine) error { return eng.Close() },
		}),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"messages":[{"role":"user","content":"Hi"}],
		"stream":true
	}`))
	rec := httptest.NewRecorder()

	srv.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
		t.Fatalf("stream body missing chunk object: %s", body)
	}
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("stream body missing assistant role: %s", body)
	}
	if !strings.Contains(body, `"content":"streamed response"`) {
		t.Fatalf("stream body missing content delta: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream body missing done marker: %s", body)
	}
}

func TestBorrowLLMEvictsIdlePools(t *testing.T) {
	model := registry.Default(registry.KindLLM)
	if model == nil {
		t.Fatal("missing default LLM model")
	}

	var ttsClosed atomic.Int64
	var sttClosed atomic.Int64

	ttsPool := NewPool(PoolConfig[tts.Engine]{
		Name:      "tts",
		Workers:   1,
		KeepAlive: true,
		Create:    func() (tts.Engine, error) { return &fakeTTSEngine{}, nil },
		Close: func(tts.Engine) error {
			ttsClosed.Add(1)
			return nil
		},
	})
	sttPool := NewPool(PoolConfig[stt.Engine]{
		Name:      "stt",
		Workers:   1,
		KeepAlive: true,
		Create:    func() (stt.Engine, error) { return &fakeSTTEngine{}, nil },
		Close: func(stt.Engine) error {
			sttClosed.Add(1)
			return nil
		},
	})
	if err := ttsPool.Prewarm(); err != nil {
		t.Fatalf("ttsPool.Prewarm(): %v", err)
	}
	if err := sttPool.Prewarm(); err != nil {
		t.Fatalf("sttPool.Prewarm(): %v", err)
	}

	srv := &Server{
		llmModel: model,
		ttsPool:  ttsPool,
		sttPool:  sttPool,
		llmPool: NewPool(PoolConfig[llm.Engine]{
			Name:      "llm",
			Workers:   1,
			KeepAlive: true,
			Create: func() (llm.Engine, error) {
				return &fakeLLMEngine{}, nil
			},
			Close: func(eng llm.Engine) error { return eng.Close() },
		}),
	}

	eng, err := srv.borrowLLM(context.Background())
	if err != nil {
		t.Fatalf("borrowLLM(): %v", err)
	}
	srv.llmPool.Return(eng)

	if ttsClosed.Load() != 1 {
		t.Fatalf("tts evictions = %d, want 1", ttsClosed.Load())
	}
	if sttClosed.Load() != 1 {
		t.Fatalf("stt evictions = %d, want 1", sttClosed.Load())
	}
}

func TestBorrowLLMKeepsIdlePoolsWithinMemoryBudget(t *testing.T) {
	model := registry.Default(registry.KindLLM)
	if model == nil {
		t.Fatal("missing default LLM model")
	}

	var ttsClosed atomic.Int64
	var sttClosed atomic.Int64

	ttsPool := NewPool(PoolConfig[tts.Engine]{
		Name:        "tts",
		Workers:     1,
		KeepAlive:   true,
		MemEstimate: 10,
		Create:      func() (tts.Engine, error) { return &fakeTTSEngine{}, nil },
		Close: func(tts.Engine) error {
			ttsClosed.Add(1)
			return nil
		},
	})
	sttPool := NewPool(PoolConfig[stt.Engine]{
		Name:        "stt",
		Workers:     1,
		KeepAlive:   true,
		MemEstimate: 20,
		Create:      func() (stt.Engine, error) { return &fakeSTTEngine{}, nil },
		Close: func(stt.Engine) error {
			sttClosed.Add(1)
			return nil
		},
	})
	llmPool := NewPool(PoolConfig[llm.Engine]{
		Name:        "llm",
		Workers:     1,
		KeepAlive:   true,
		MemEstimate: 30,
		Create: func() (llm.Engine, error) {
			return &fakeLLMEngine{}, nil
		},
		Close: func(eng llm.Engine) error { return eng.Close() },
	})
	if err := ttsPool.Prewarm(); err != nil {
		t.Fatalf("ttsPool.Prewarm(): %v", err)
	}
	if err := sttPool.Prewarm(); err != nil {
		t.Fatalf("sttPool.Prewarm(): %v", err)
	}

	srv := &Server{
		cfg:      Config{MemoryBudget: 60},
		llmModel: model,
		ttsPool:  ttsPool,
		sttPool:  sttPool,
		llmPool:  llmPool,
	}

	eng, err := srv.borrowLLM(context.Background())
	if err != nil {
		t.Fatalf("borrowLLM(): %v", err)
	}
	srv.llmPool.Return(eng)

	if ttsClosed.Load() != 0 {
		t.Fatalf("tts evictions = %d, want 0", ttsClosed.Load())
	}
	if sttClosed.Load() != 0 {
		t.Fatalf("stt evictions = %d, want 0", sttClosed.Load())
	}
	if got := srv.ttsPool.Stats().EnginesReady; got != 1 {
		t.Fatalf("tts idle engines = %d, want 1", got)
	}
	if got := srv.sttPool.Stats().EnginesReady; got != 1 {
		t.Fatalf("stt idle engines = %d, want 1", got)
	}
}

func TestCloseLLMEngineDrainsORT(t *testing.T) {
	var drained atomic.Int64
	originalDrain := ortDrain
	ortDrain = func() {
		drained.Add(1)
	}
	defer func() {
		ortDrain = originalDrain
	}()

	var closed atomic.Int64
	srv := &Server{}
	err := srv.closeLLMEngine(&fakeLLMEngine{
		close: func() error {
			closed.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("closeLLMEngine() error = %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("llm close count = %d, want 1", closed.Load())
	}
	if drained.Load() != 1 {
		t.Fatalf("ort drain count = %d, want 1", drained.Load())
	}
}

func TestSequentialSwapPathEvictsIdlePools(t *testing.T) {
	model := registry.Default(registry.KindLLM)
	if model == nil {
		t.Fatal("missing default LLM model")
	}

	var ttsClosed atomic.Int64
	var sttClosed atomic.Int64
	var llmClosed atomic.Int64

	ttsPool := NewPool(PoolConfig[tts.Engine]{
		Name:      "tts",
		Workers:   1,
		KeepAlive: true,
		Create:    func() (tts.Engine, error) { return &fakeTTSEngine{}, nil },
		Close: func(eng tts.Engine) error {
			ttsClosed.Add(1)
			return eng.Close()
		},
	})
	sttPool := NewPool(PoolConfig[stt.Engine]{
		Name:      "stt",
		Workers:   1,
		KeepAlive: true,
		Create:    func() (stt.Engine, error) { return &fakeSTTEngine{}, nil },
		Close: func(eng stt.Engine) error {
			sttClosed.Add(1)
			return eng.Close()
		},
	})
	llmPool := NewPool(PoolConfig[llm.Engine]{
		Name:      "llm",
		Workers:   1,
		KeepAlive: true,
		Create: func() (llm.Engine, error) {
			return &fakeLLMEngine{
				close: func() error {
					llmClosed.Add(1)
					return nil
				},
			}, nil
		},
		Close: func(eng llm.Engine) error {
			return eng.Close()
		},
	})

	if err := ttsPool.Prewarm(); err != nil {
		t.Fatalf("ttsPool.Prewarm(): %v", err)
	}
	if err := sttPool.Prewarm(); err != nil {
		t.Fatalf("sttPool.Prewarm(): %v", err)
	}

	srv := &Server{
		llmModel: model,
		ttsPool:  ttsPool,
		sttPool:  sttPool,
		llmPool:  llmPool,
	}

	sttEng, err := srv.borrowSTT(context.Background())
	if err != nil {
		t.Fatalf("borrowSTT() error = %v", err)
	}
	srv.sttPool.Return(sttEng)

	llmEng, err := srv.borrowLLM(context.Background())
	if err != nil {
		t.Fatalf("borrowLLM() error = %v", err)
	}
	if got := srv.ttsPool.Stats().EnginesReady; got != 0 {
		t.Fatalf("tts idle after llm borrow = %d, want 0", got)
	}
	if got := srv.sttPool.Stats().EnginesReady; got != 0 {
		t.Fatalf("stt idle after llm borrow = %d, want 0", got)
	}
	srv.llmPool.Return(llmEng)

	ttsEng, err := srv.borrowTTS(context.Background())
	if err != nil {
		t.Fatalf("borrowTTS() error = %v", err)
	}
	if got := srv.llmPool.Stats().EnginesReady; got != 0 {
		t.Fatalf("llm idle after tts borrow = %d, want 0", got)
	}
	srv.ttsPool.Return(ttsEng)

	if ttsClosed.Load() != 1 {
		t.Fatalf("tts evictions = %d, want 1", ttsClosed.Load())
	}
	if sttClosed.Load() != 1 {
		t.Fatalf("stt evictions = %d, want 1", sttClosed.Load())
	}
	if llmClosed.Load() != 1 {
		t.Fatalf("llm evictions = %d, want 1", llmClosed.Load())
	}
}
