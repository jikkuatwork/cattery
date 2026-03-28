package server

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/tts"
)

type stubTTSEngine struct {
	speak func(io.Writer, string, tts.Options) error
}

func (s *stubTTSEngine) Speak(w io.Writer, text string, opts tts.Options) error {
	if s.speak != nil {
		return s.speak(w, text, opts)
	}
	_, err := w.Write([]byte("RIFF0000WAVEfmt "))
	return err
}

func (s *stubTTSEngine) Voices() []tts.Voice { return nil }
func (s *stubTTSEngine) Close() error        { return nil }

type stubSTTEngine struct {
	transcribe func(io.Reader, stt.Options) (*stt.Result, error)
}

func (s *stubSTTEngine) Transcribe(r io.Reader, opts stt.Options) (*stt.Result, error) {
	if s.transcribe != nil {
		return s.transcribe(r, opts)
	}
	return &stt.Result{Text: "ok"}, nil
}

func (s *stubSTTEngine) SampleRate() int { return 16000 }
func (s *stubSTTEngine) Close() error    { return nil }

type stubLLMEngine struct{}

func (s *stubLLMEngine) Generate(context.Context, string, llm.Options) (*llm.Result, error) {
	return &llm.Result{Text: "ok", TokensUsed: 1, FinishReason: "stop"}, nil
}

func (s *stubLLMEngine) Close() error { return nil }

func newTestAPIServer(t *testing.T, ttsEngine tts.Engine, sttEngine stt.Engine) *Server {
	t.Helper()

	ttsModel := registry.Default(registry.KindTTS)
	sttModel := registry.Default(registry.KindSTT)
	llmModel := registry.Default(registry.KindLLM)
	if ttsModel == nil || sttModel == nil || llmModel == nil {
		t.Fatal("missing default registry models")
	}

	srv := &Server{
		cfg:      Config{QueueMax: 2, MaxChars: 500},
		mux:      http.NewServeMux(),
		ttsModel: ttsModel,
		sttModel: sttModel,
		llmModel: llmModel,
		ttsPool: NewPool(PoolConfig[tts.Engine]{
			Name:      "tts",
			Workers:   1,
			KeepAlive: true,
			Create: func() (tts.Engine, error) {
				return ttsEngine, nil
			},
			Close: func(eng tts.Engine) error { return eng.Close() },
		}),
		sttPool: NewPool(PoolConfig[stt.Engine]{
			Name:      "stt",
			Workers:   1,
			KeepAlive: true,
			Create: func() (stt.Engine, error) {
				return sttEngine, nil
			},
			Close: func(eng stt.Engine) error { return eng.Close() },
		}),
		llmPool: NewPool(PoolConfig[llm.Engine]{
			Name:      "llm",
			Workers:   1,
			KeepAlive: true,
			Create: func() (llm.Engine, error) {
				return &stubLLMEngine{}, nil
			},
			Close: func(eng llm.Engine) error { return eng.Close() },
		}),
	}

	srv.mux.Handle("POST /v1/audio/speech", http.HandlerFunc(srv.handleAudioSpeech))
	srv.mux.Handle("POST /v1/audio/transcriptions", http.HandlerFunc(srv.handleAudioTranscriptions))
	srv.mux.Handle("POST /v1/chat/completions", http.HandlerFunc(srv.handleChatCompletions))
	srv.mux.Handle("GET /v1/models", http.HandlerFunc(srv.handleModels))
	srv.mux.HandleFunc("GET /v1/status", srv.handleStatus)

	return srv
}
