// Package server provides a multi-modal REST API for cattery.
//
// Endpoints:
//
//	POST /v1/audio/speech — synthesize text, returns WAV audio
//	POST /v1/audio/transcriptions — transcribe audio, returns JSON
//	POST /v1/chat/completions — OpenAI-compatible chat completions
//	GET  /v1/models   — list available TTS + STT + LLM models
//	GET  /v1/status   — server health and pool stats
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jikkuatwork/cattery/download"
	"github.com/jikkuatwork/cattery/llm"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/tts"
	"github.com/jikkuatwork/cattery/tts/kokoro"
)

var ortDrain = ort.Drain

// Config holds server configuration.
type Config struct {
	Port         int
	TTSWorkers   int
	STTWorkers   int
	QueueMax     int
	MaxChars     int
	MemoryBudget int64
	IdleTimeout  time.Duration
	KeepAlive    bool
	Auth         bool
	ChunkSize    time.Duration
}

// Server is the cattery HTTP server.
type Server struct {
	cfg       Config
	mux       *http.ServeMux
	queue     chan struct{}
	startedAt time.Time
	dataDir   string
	ortLib    string
	authStore *KeyStore
	ttsModel  *registry.Model
	sttModel  *registry.Model
	llmModel  *registry.Model
	ttsPool   *Pool[tts.Engine]
	sttPool   *Pool[stt.Engine]
	llmPool   *Pool[llm.Engine]

	charsMu   sync.Mutex
	charsUsed int

	queued    atomic.Int64
	processed atomic.Int64
	failed    atomic.Int64
}

type requestRef struct {
	Value string
}

func (r *requestRef) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		r.Value = ""
		return nil
	}

	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		r.Value = strings.TrimSpace(value)
		return nil
	}

	var index int
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("must be a string or positive integer")
	}
	if index < 1 {
		return fmt.Errorf("must be a positive integer")
	}

	r.Value = strconv.Itoa(index)
	return nil
}

func (r requestRef) String() string {
	return strings.TrimSpace(r.Value)
}

type audioSpeechRequest struct {
	Input          string     `json:"input"`
	Voice          requestRef `json:"voice,omitempty"`
	Model          requestRef `json:"model,omitempty"`
	Speed          float64    `json:"speed,omitempty"`
	ResponseFormat string     `json:"response_format,omitempty"`
}

type openAIModelListResponse struct {
	Object string              `json:"object"`
	Data   []openAIModelObject `json:"data"`
}

type openAIModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ttsStatusResponse struct {
	Model        int    `json:"model"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
	MaxChars     int    `json:"max_chars"`
	CharsUsed    int    `json:"chars_used"`
}

type sttStatusResponse struct {
	Model        int    `json:"model"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
}

type llmStatusResponse struct {
	Model        int    `json:"model"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
}

type statusResponse struct {
	Status    string            `json:"status"`
	TTS       ttsStatusResponse `json:"tts"`
	STT       sttStatusResponse `json:"stt"`
	LLM       llmStatusResponse `json:"llm"`
	Queued    int64             `json:"queued"`
	Processed int64             `json:"processed"`
	Failed    int64             `json:"failed"`
	Uptime    string            `json:"uptime"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type synthMeta struct {
	duration float64
	elapsed  float64
	rtf      float64
}

// New creates and initializes a Server.
func New(cfg Config) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = 7100
	}
	if cfg.TTSWorkers == 0 {
		cfg.TTSWorkers = 1
	}
	if cfg.STTWorkers == 0 {
		cfg.STTWorkers = 1
	}
	if cfg.QueueMax == 0 {
		cfg.QueueMax = 5
	}
	if cfg.MaxChars == 0 {
		cfg.MaxChars = 500
	}
	if cfg.IdleTimeout == 0 && !cfg.KeepAlive {
		cfg.IdleTimeout = 300 * time.Second
	}
	ttsModel := registry.Default(registry.KindTTS)
	if ttsModel == nil {
		return nil, fmt.Errorf("no TTS models registered")
	}
	sttModel := registry.Default(registry.KindSTT)
	if sttModel == nil {
		return nil, fmt.Errorf("no STT models registered")
	}
	llmModel := registry.Default(registry.KindLLM)
	if llmModel == nil {
		return nil, fmt.Errorf("no LLM models registered")
	}
	resolvedChunkSize, err := resolveServerChunkSizeFromEnv(cfg.ChunkSize, os.Stderr)
	if err != nil {
		return nil, err
	}
	cfg.ChunkSize = resolvedChunkSize

	if ttsModel.Location != registry.Local {
		return nil, fmt.Errorf("remote TTS model %q is not supported yet", ttsModel.ID)
	}

	if sttModel.Location != registry.Local {
		return nil, fmt.Errorf("remote STT model %q is not supported yet", sttModel.ID)
	}

	if llmModel.Location != registry.Local {
		return nil, fmt.Errorf("remote LLM model %q is not supported yet", llmModel.ID)
	}
	preflight.WarnLowLLMMemory(os.Stderr, llmModel)

	dataDir := paths.DataDir()

	var authStore *KeyStore
	if cfg.Auth {
		authStore = DefaultKeyStore()
		if err := authStore.Load(); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("no API keys found; run 'cattery keys create' first")
			}
			return nil, fmt.Errorf("load API keys: %w", err)
		}
		if len(authStore.Entries()) == 0 {
			return nil, fmt.Errorf("no API keys found; run 'cattery keys create' first")
		}
	}

	ttsResult, err := download.Ensure(dataDir, ttsModel)
	if err != nil {
		return nil, fmt.Errorf("download TTS model: %w", err)
	}
	modelFile := ttsModel.PrimaryFile()
	if modelFile == nil {
		return nil, fmt.Errorf("model %q has no files", ttsModel.ID)
	}
	ttsModelPath := ttsResult.Files[modelFile.Filename]

	sttResult, err := download.Ensure(dataDir, sttModel)
	if err != nil {
		return nil, fmt.Errorf("download STT model: %w", err)
	}
	llmResult, err := download.Ensure(dataDir, llmModel)
	if err != nil {
		return nil, fmt.Errorf("download LLM model: %w", err)
	}

	ortLib := strings.TrimSpace(ttsResult.ORTLib)
	if ortLib == "" {
		ortLib = strings.TrimSpace(sttResult.ORTLib)
	}
	if ortLib == "" {
		ortLib = strings.TrimSpace(llmResult.ORTLib)
	}

	s := &Server{
		cfg:       cfg,
		mux:       http.NewServeMux(),
		startedAt: time.Now(),
		dataDir:   dataDir,
		ortLib:    ortLib,
		authStore: authStore,
		ttsModel:  ttsModel,
		sttModel:  sttModel,
		llmModel:  llmModel,
	}
	if cfg.QueueMax > 0 {
		s.queue = make(chan struct{}, cfg.QueueMax)
	}

	s.ttsPool = NewPool(PoolConfig[tts.Engine]{
		Name:        "tts",
		Workers:     cfg.TTSWorkers,
		IdleTimeout: cfg.IdleTimeout,
		KeepAlive:   cfg.KeepAlive,
		MemEstimate: ttsModel.TotalFileSize() * 2,
		Create: func() (tts.Engine, error) {
			if err := s.ensureORT(); err != nil {
				return nil, err
			}
			return kokoro.New(ttsModelPath, dataDir)
		},
		Close: func(eng tts.Engine) error {
			return eng.Close()
		},
		OnEmpty: s.onPoolsEmpty,
	})

	s.sttPool = NewPool(PoolConfig[stt.Engine]{
		Name:        "stt",
		Workers:     cfg.STTWorkers,
		IdleTimeout: cfg.IdleTimeout,
		KeepAlive:   cfg.KeepAlive,
		MemEstimate: sttModel.TotalFileSize() * 2,
		Create: func() (stt.Engine, error) {
			if err := s.ensureORT(); err != nil {
				return nil, err
			}
			return newSTTEngine(sttModel, dataDir)
		},
		Close: func(eng stt.Engine) error {
			return eng.Close()
		},
		OnEmpty: s.onPoolsEmpty,
	})

	s.llmPool = NewPool(PoolConfig[llm.Engine]{
		Name:        "llm",
		Workers:     1,
		IdleTimeout: cfg.IdleTimeout,
		KeepAlive:   cfg.KeepAlive,
		MemEstimate: llmModel.TotalFileSize() * 2,
		Create: func() (llm.Engine, error) {
			if err := s.ensureORT(); err != nil {
				return nil, err
			}
			return newLLMEngine(llmModel, dataDir)
		},
		Close:   s.closeLLMEngine,
		OnEmpty: s.onPoolsEmpty,
	})

	if cfg.KeepAlive {
		if err := s.ensureORT(); err != nil {
			return nil, fmt.Errorf("init ORT: %w", err)
		}
		if err := s.ttsPool.Prewarm(); err != nil {
			ort.Shutdown()
			return nil, fmt.Errorf("pre-warm TTS pool: %w", err)
		}
		if err := s.sttPool.Prewarm(); err != nil {
			s.Shutdown()
			return nil, fmt.Errorf("pre-warm STT pool: %w", err)
		}
		if err := s.llmPool.Prewarm(); err != nil {
			s.Shutdown()
			return nil, fmt.Errorf("pre-warm LLM pool: %w", err)
		}
	}

	protected := func(handler http.Handler) http.Handler {
		if s.authStore == nil {
			return handler
		}
		return AuthMiddleware(s.authStore)(handler)
	}

	s.mux.Handle("POST /v1/audio/speech", protected(http.HandlerFunc(s.handleAudioSpeech)))
	s.mux.Handle("POST /v1/audio/transcriptions", protected(http.HandlerFunc(s.handleAudioTranscriptions)))
	s.mux.Handle("POST /v1/chat/completions", protected(http.HandlerFunc(s.handleChatCompletions)))
	s.mux.Handle("GET /v1/models", protected(http.HandlerFunc(s.handleModels)))
	s.mux.HandleFunc("GET /v1/status", s.handleStatus)

	s.mux.Handle("GET /debug/pprof/", protected(http.HandlerFunc(pprof.Index)))
	s.mux.Handle("GET /debug/pprof/cmdline", protected(http.HandlerFunc(pprof.Cmdline)))
	s.mux.Handle("GET /debug/pprof/profile", protected(http.HandlerFunc(pprof.Profile)))
	s.mux.Handle("GET /debug/pprof/symbol", protected(http.HandlerFunc(pprof.Symbol)))
	s.mux.Handle("GET /debug/pprof/trace", protected(http.HandlerFunc(pprof.Trace)))
	s.mux.Handle("GET /debug/pprof/heap", protected(pprof.Handler("heap")))
	s.mux.Handle("GET /debug/pprof/allocs", protected(pprof.Handler("allocs")))
	s.mux.Handle("GET /debug/pprof/goroutine", protected(pprof.Handler("goroutine")))

	return s, nil
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	mode := fmt.Sprintf("idle timeout %s", s.cfg.IdleTimeout)
	if s.cfg.KeepAlive {
		mode = "keep-alive"
	}
	log.Printf(
		"cattery server listening on %s (tts %d, stt %d, max chars %d, queue %d, %s)",
		addr,
		s.cfg.TTSWorkers,
		s.cfg.STTWorkers,
		s.cfg.MaxChars,
		s.cfg.QueueMax,
		mode,
	)
	return http.ListenAndServe(addr, s.mux)
}

// Shutdown releases pooled engine resources.
func (s *Server) Shutdown() {
	if s.ttsPool != nil {
		s.ttsPool.Shutdown()
	}
	if s.sttPool != nil {
		s.sttPool.Shutdown()
	}
	if s.llmPool != nil {
		s.llmPool.Shutdown()
	}
	ort.Shutdown()
}

func (s *Server) handleAudioSpeech(w http.ResponseWriter, r *http.Request) {
	var req audioSpeechRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req.Input = strings.TrimSpace(req.Input)
	if req.Input == "" {
		writeOpenAIError(w, http.StatusBadRequest, "input is required")
		return
	}
	if req.Speed == 0 {
		req.Speed = 1.0
	}
	if req.Speed < 0.5 || req.Speed > 2.0 {
		writeOpenAIError(w, http.StatusBadRequest, "speed must be between 0.5 and 2.0")
		return
	}
	if format := strings.TrimSpace(req.ResponseFormat); format != "" && format != "wav" {
		writeOpenAIError(w, http.StatusBadRequest, "unsupported response_format")
		return
	}

	model, err := s.resolveTTSModel(req.Model.String())
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	voice, _, err := resolveTTSVoice(model, req.Voice.String(), "")
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	textLen := len(req.Input)
	used, avail, ok := s.reserveChars(textLen)
	if !ok {
		w.Header().Set("Retry-After", "3")
		writeOpenAIError(w, http.StatusServiceUnavailable,
			fmt.Sprintf(
				"character budget exhausted (%d/%d used, need %d, %d available)",
				used,
				s.cfg.MaxChars,
				textLen,
				avail,
			))
		return
	}
	defer s.releaseChars(textLen)

	wavBuf, meta, err := s.synthesize(r.Context(), req, model, voice)
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			w.Header().Set("Retry-After", "2")
			writeOpenAIError(w, http.StatusServiceUnavailable,
				fmt.Sprintf("queue full (%d max), try again shortly", s.cfg.QueueMax))
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		s.failed.Add(1)
		if preflight.IsMemoryError(err) {
			w.Header().Set("Retry-After", "30")
			writeOpenAIError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		log.Printf("tts error: %v", err)
		writeOpenAIError(w, http.StatusInternalServerError, "synthesis failed")
		return
	}

	s.processed.Add(1)

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", wavBuf.Len()))
	w.WriteHeader(http.StatusOK)
	w.Write(wavBuf.Bytes())
	_ = meta
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := append(
		append(
			append([]*registry.Model{}, registry.GetByKind(registry.KindTTS)...),
			registry.GetByKind(registry.KindSTT)...,
		),
		registry.GetByKind(registry.KindLLM)...,
	)

	resp := make([]openAIModelObject, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}

		resp = append(resp, openAIModelObject{
			ID:      model.ID,
			Object:  "model",
			Created: 0,
			OwnedBy: "cattery",
		})
	}

	writeJSON(w, http.StatusOK, openAIModelListResponse{
		Object: "list",
		Data:   resp,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	charsUsed := s.currentChars()
	ttsStats := s.ttsPool.Stats()
	sttStats := s.sttPool.Stats()
	llmStats := s.llmPool.Stats()

	writeJSON(w, http.StatusOK, statusResponse{
		Status: "ok",
		TTS: ttsStatusResponse{
			Model:        s.ttsModel.Index,
			ModelID:      s.ttsModel.ID,
			ModelName:    s.ttsModel.Name,
			Workers:      ttsStats.Workers,
			EnginesReady: ttsStats.EnginesReady,
			MaxChars:     s.cfg.MaxChars,
			CharsUsed:    charsUsed,
		},
		STT: sttStatusResponse{
			Model:        s.sttModel.Index,
			ModelID:      s.sttModel.ID,
			ModelName:    s.sttModel.Name,
			Workers:      sttStats.Workers,
			EnginesReady: sttStats.EnginesReady,
		},
		LLM: llmStatusResponse{
			Model:        s.llmModel.Index,
			ModelID:      s.llmModel.ID,
			ModelName:    s.llmModel.Name,
			Workers:      llmStats.Workers,
			EnginesReady: llmStats.EnginesReady,
		},
		Queued:    s.queued.Load(),
		Processed: s.processed.Load(),
		Failed:    s.failed.Load(),
		Uptime:    time.Since(s.startedAt).Round(time.Second).String(),
	})
}

func (s *Server) resolveTTSModel(ref string) (*registry.Model, error) {
	return resolveConfiguredModel(registry.KindTTS, ref, s.ttsModel, "tts")
}

func resolveConfiguredModel(
	kind registry.Kind,
	ref string,
	configured *registry.Model,
	poolName string,
) (*registry.Model, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return configured, nil
	}

	model := registry.Resolve(kind, ref)
	if model == nil {
		return nil, fmt.Errorf("unknown %s model %q", strings.ToUpper(string(kind)), ref)
	}
	if configured != nil && model.ID != configured.ID {
		return nil, fmt.Errorf(
			"%s pool is configured for model %d (%s); restart the server to use model %d (%s)",
			poolName,
			configured.Index,
			configured.ID,
			model.Index,
			model.ID,
		)
	}
	return model, nil
}

func resolveTTSVoice(
	model *registry.Model,
	voiceRef string,
	gender string,
) (tts.Voice, int, error) {
	if model == nil {
		return tts.Voice{}, 0, fmt.Errorf("missing TTS model")
	}

	switch model.ID {
	case "kokoro-82m-v1.0":
		voice, err := kokoro.ResolveVoice(model, voiceRef, gender)
		if err != nil {
			return tts.Voice{}, 0, err
		}
		index := voiceIndex(model, voice.ID)
		if index == 0 {
			return tts.Voice{}, 0, fmt.Errorf("voice %q is not registered", voice.ID)
		}
		return voice, index, nil
	default:
		return tts.Voice{}, 0, fmt.Errorf("TTS model %q is not supported yet", model.ID)
	}
}

func voiceIndex(model *registry.Model, voiceID string) int {
	if model == nil {
		return 0
	}
	for i := range model.Voices {
		if model.Voices[i].ID == voiceID {
			return i + 1
		}
	}
	return 0
}

func (s *Server) synthesize(
	ctx context.Context,
	req audioSpeechRequest,
	model *registry.Model,
	voice tts.Voice,
) (*bytes.Buffer, synthMeta, error) {
	eng, err := s.borrowTTS(ctx)
	if err != nil {
		return nil, synthMeta{}, err
	}
	defer s.ttsPool.Return(eng)

	t0 := time.Now()
	var buf bytes.Buffer
	err = preflight.GuardMemoryError("speech synthesis", func() error {
		return eng.Speak(&buf, req.Input, tts.Options{
			Voice:     voice.ID,
			Gender:    "",
			Speed:     req.Speed,
			Lang:      "en-us",
			ChunkSize: s.cfg.ChunkSize,
		})
	})
	if err != nil {
		if preflight.IsMemoryError(err) {
			return nil, synthMeta{}, err
		}
		return nil, synthMeta{}, fmt.Errorf("synthesize: %w", err)
	}

	elapsed := time.Since(t0).Seconds()
	duration := wavDurationFromSize(int64(buf.Len()), model.MetaInt("sample_rate", 24000))
	rtf := 0.0
	if duration > 0 {
		rtf = elapsed / duration
	}

	return &buf, synthMeta{
		duration: duration,
		elapsed:  elapsed,
		rtf:      rtf,
	}, nil
}

func (s *Server) ensureORT() error {
	if strings.TrimSpace(s.ortLib) == "" {
		return nil
	}
	return ort.Init(s.ortLib)
}

func (s *Server) onPoolsEmpty() {
	if s.cfg.KeepAlive {
		return
	}
	if s.ttsPool != nil && !s.ttsPool.Empty() {
		return
	}
	if s.sttPool != nil && !s.sttPool.Empty() {
		return
	}
	if s.llmPool != nil && !s.llmPool.Empty() {
		return
	}
	ort.Shutdown()
}

func (s *Server) borrowLLM(ctx context.Context) (llm.Engine, error) {
	if !s.canCoResident(s.llmPool.MemEstimate()) {
		if s.ttsPool != nil {
			s.ttsPool.EvictIdle()
		}
		if s.sttPool != nil {
			s.sttPool.EvictIdle()
		}
	}
	return s.llmPool.Borrow(ctx, s.queue, &s.queued)
}

func (s *Server) borrowTTS(ctx context.Context) (tts.Engine, error) {
	if s.llmPool != nil && !s.canCoResident(s.ttsPool.MemEstimate()) {
		s.llmPool.EvictIdle()
	}
	return s.ttsPool.Borrow(ctx, s.queue, &s.queued)
}

func (s *Server) borrowSTT(ctx context.Context) (stt.Engine, error) {
	if s.llmPool != nil && !s.canCoResident(s.sttPool.MemEstimate()) {
		s.llmPool.EvictIdle()
	}
	return s.sttPool.Borrow(ctx, s.queue, &s.queued)
}

func (s *Server) canCoResident(borrowingEstimate int64) bool {
	if s.cfg.MemoryBudget == 0 {
		return false
	}

	total := int64(0)
	borrowingPresent := false

	for _, pool := range []interface {
		Empty() bool
		MemEstimate() int64
	}{s.ttsPool, s.sttPool, s.llmPool} {
		if pool == nil || pool.Empty() {
			continue
		}
		estimate := pool.MemEstimate()
		total += estimate
		if !borrowingPresent && estimate == borrowingEstimate {
			borrowingPresent = true
		}
	}

	if !borrowingPresent {
		total += borrowingEstimate
	}

	return total <= s.cfg.MemoryBudget
}

func (s *Server) closeLLMEngine(eng llm.Engine) error {
	if eng == nil {
		ortDrain()
		return nil
	}

	err := eng.Close()
	ortDrain()
	return err
}

func (s *Server) reserveChars(n int) (used int, avail int, ok bool) {
	s.charsMu.Lock()
	defer s.charsMu.Unlock()

	if s.charsUsed+n > s.cfg.MaxChars {
		return s.charsUsed, s.cfg.MaxChars - s.charsUsed, false
	}

	s.charsUsed += n
	return s.charsUsed - n, s.cfg.MaxChars - s.charsUsed, true
}

func (s *Server) releaseChars(n int) {
	s.charsMu.Lock()
	s.charsUsed -= n
	s.charsMu.Unlock()
}

func (s *Server) currentChars() int {
	s.charsMu.Lock()
	defer s.charsMu.Unlock()
	return s.charsUsed
}

func modelFilesDownloaded(dataDir string, model *registry.Model) bool {
	if model == nil || model.Location != registry.Local {
		return false
	}
	for _, file := range model.Files {
		if _, err := os.Stat(paths.ArtefactFile(dataDir, model.ID, file.Filename)); err != nil {
			return false
		}
	}
	return true
}

func modelFilesSize(model *registry.Model) int64 {
	if model == nil {
		return 0
	}
	var total int64
	for _, file := range model.Files {
		total += file.SizeBytes
	}
	return total
}

func wavDurationFromSize(size int64, sampleRate int) float64 {
	if size <= 44 || sampleRate <= 0 {
		return 0
	}
	dataBytes := size - 44
	return float64(dataBytes/2) / float64(sampleRate)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeOpenAIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, openAIErrorEnvelope{
		Error: openAIError{
			Message: msg,
			Type:    "invalid_request_error",
			Code:    nil,
		},
	})
}
