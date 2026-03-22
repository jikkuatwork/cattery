// Package server provides a multi-modal REST API for cattery.
//
// Endpoints:
//
//	POST /v1/speak    — synthesize text, returns WAV audio
//	POST /v1/tts      — alias for /v1/speak
//	POST /v1/listen   — transcribe audio, returns JSON
//	GET  /v1/models   — list available TTS + STT models
//	GET  /v1/voices   — list available TTS voices
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
	"github.com/jikkuatwork/cattery/listen"
	"github.com/jikkuatwork/cattery/ort"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/speak"
	"github.com/jikkuatwork/cattery/speak/kokoro"
)

// Config holds server configuration.
type Config struct {
	Port          int
	SpeakWorkers  int
	ListenWorkers int
	QueueMax      int
	MaxChars      int
	IdleTimeout   time.Duration
	KeepAlive     bool
	SpeakModel    int
	ListenModel   int
	ChunkSize     time.Duration
}

// Server is the cattery HTTP server.
type Server struct {
	cfg         Config
	mux         *http.ServeMux
	queue       chan struct{}
	startedAt   time.Time
	dataDir     string
	ortLib      string
	speakModel  *registry.Model
	listenModel *registry.Model
	speakPool   *Pool[speak.Engine]
	listenPool  *Pool[listen.Engine]

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

type speakRequest struct {
	Text   string     `json:"text"`
	Voice  requestRef `json:"voice,omitempty"`
	Model  requestRef `json:"model,omitempty"`
	Gender string     `json:"gender,omitempty"`
	Speed  float64    `json:"speed,omitempty"`
	Lang   string     `json:"lang,omitempty"`
}

type modelResponse struct {
	Index      int    `json:"index"`
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	Downloaded *bool  `json:"downloaded"`
	SizeBytes  *int64 `json:"size_bytes,omitempty"`
	Voices     *int   `json:"voices,omitempty"`
}

type voiceResponse struct {
	Index       int    `json:"index"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Gender      string `json:"gender"`
	Accent      string `json:"accent"`
	Description string `json:"description"`
	Model       int    `json:"model"`
	ModelID     string `json:"model_id"`
}

type speakStatusResponse struct {
	Model        int    `json:"model"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
	MaxChars     int    `json:"max_chars"`
	CharsUsed    int    `json:"chars_used"`
}

type listenStatusResponse struct {
	Model        int    `json:"model"`
	ModelID      string `json:"model_id"`
	ModelName    string `json:"model_name"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
}

type statusResponse struct {
	Status    string               `json:"status"`
	Speak     speakStatusResponse  `json:"speak"`
	Listen    listenStatusResponse `json:"listen"`
	Queued    int64                `json:"queued"`
	Processed int64                `json:"processed"`
	Failed    int64                `json:"failed"`
	Uptime    string               `json:"uptime"`
}

type errorResponse struct {
	Error string `json:"error"`
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
	if cfg.SpeakWorkers == 0 {
		cfg.SpeakWorkers = 1
	}
	if cfg.ListenWorkers == 0 {
		cfg.ListenWorkers = 1
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
	if cfg.SpeakModel == 0 {
		model := registry.Default(registry.KindTTS)
		if model == nil {
			return nil, fmt.Errorf("no TTS models registered")
		}
		cfg.SpeakModel = model.Index
	}
	if cfg.ListenModel == 0 {
		model := registry.Default(registry.KindSTT)
		if model == nil {
			return nil, fmt.Errorf("no STT models registered")
		}
		cfg.ListenModel = model.Index
	}
	resolvedChunkSize, err := resolveServerChunkSizeFromEnv(cfg.ChunkSize, os.Stderr)
	if err != nil {
		return nil, err
	}
	cfg.ChunkSize = resolvedChunkSize

	speakModel := registry.GetByIndex(registry.KindTTS, cfg.SpeakModel)
	if speakModel == nil {
		return nil, fmt.Errorf("unknown TTS model %d", cfg.SpeakModel)
	}
	if speakModel.Location != registry.Local {
		return nil, fmt.Errorf("remote TTS model %q is not supported yet", speakModel.ID)
	}

	listenModel := registry.GetByIndex(registry.KindSTT, cfg.ListenModel)
	if listenModel == nil {
		return nil, fmt.Errorf("unknown STT model %d", cfg.ListenModel)
	}
	if listenModel.Location != registry.Local {
		return nil, fmt.Errorf("remote STT model %q is not supported yet", listenModel.ID)
	}

	if !phonemize.Available() {
		return nil, fmt.Errorf("espeak-ng not found (required)")
	}

	dataDir := paths.DataDir()

	speakResult, err := download.Ensure(dataDir, speakModel)
	if err != nil {
		return nil, fmt.Errorf("download speak model: %w", err)
	}
	modelFile := speakModel.PrimaryFile()
	if modelFile == nil {
		return nil, fmt.Errorf("model %q has no files", speakModel.ID)
	}
	speakModelPath := speakResult.Files[modelFile.Filename]

	listenResult, err := download.Ensure(dataDir, listenModel)
	if err != nil {
		return nil, fmt.Errorf("download listen model: %w", err)
	}

	ortLib := strings.TrimSpace(speakResult.ORTLib)
	if ortLib == "" {
		ortLib = strings.TrimSpace(listenResult.ORTLib)
	}

	s := &Server{
		cfg:         cfg,
		mux:         http.NewServeMux(),
		startedAt:   time.Now(),
		dataDir:     dataDir,
		ortLib:      ortLib,
		speakModel:  speakModel,
		listenModel: listenModel,
	}
	if cfg.QueueMax > 0 {
		s.queue = make(chan struct{}, cfg.QueueMax)
	}

	s.speakPool = NewPool(PoolConfig[speak.Engine]{
		Name:        "speak",
		Workers:     cfg.SpeakWorkers,
		IdleTimeout: cfg.IdleTimeout,
		KeepAlive:   cfg.KeepAlive,
		Create: func() (speak.Engine, error) {
			if err := s.ensureORT(); err != nil {
				return nil, err
			}
			return kokoro.New(speakModelPath, dataDir)
		},
		Close: func(eng speak.Engine) error {
			return eng.Close()
		},
		OnEmpty: s.onPoolsEmpty,
	})

	s.listenPool = NewPool(PoolConfig[listen.Engine]{
		Name:        "listen",
		Workers:     cfg.ListenWorkers,
		IdleTimeout: cfg.IdleTimeout,
		KeepAlive:   cfg.KeepAlive,
		Create: func() (listen.Engine, error) {
			if err := s.ensureORT(); err != nil {
				return nil, err
			}
			return newListenEngine(listenModel, dataDir)
		},
		Close: func(eng listen.Engine) error {
			return eng.Close()
		},
		OnEmpty: s.onPoolsEmpty,
	})

	if cfg.KeepAlive {
		if err := s.ensureORT(); err != nil {
			return nil, fmt.Errorf("init ORT: %w", err)
		}
		if err := s.speakPool.Prewarm(); err != nil {
			ort.Shutdown()
			return nil, fmt.Errorf("pre-warm speak pool: %w", err)
		}
		if err := s.listenPool.Prewarm(); err != nil {
			s.Shutdown()
			return nil, fmt.Errorf("pre-warm listen pool: %w", err)
		}
	}

	s.mux.HandleFunc("POST /v1/speak", s.handleSpeak)
	s.mux.HandleFunc("POST /v1/tts", s.handleSpeak)
	s.mux.HandleFunc("POST /v1/listen", s.handleListen)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("GET /v1/voices", s.handleVoices)
	s.mux.HandleFunc("GET /v1/status", s.handleStatus)

	s.mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	s.mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	s.mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	s.mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	s.mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	s.mux.Handle("GET /debug/pprof/allocs", pprof.Handler("allocs"))
	s.mux.Handle("GET /debug/pprof/goroutine", pprof.Handler("goroutine"))

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
		"cattery server listening on %s (speak %d, listen %d, max chars %d, queue %d, %s)",
		addr,
		s.cfg.SpeakWorkers,
		s.cfg.ListenWorkers,
		s.cfg.MaxChars,
		s.cfg.QueueMax,
		mode,
	)
	return http.ListenAndServe(addr, s.mux)
}

// Shutdown releases pooled engine resources.
func (s *Server) Shutdown() {
	if s.speakPool != nil {
		s.speakPool.Shutdown()
	}
	if s.listenPool != nil {
		s.listenPool.Shutdown()
	}
	ort.Shutdown()
}

func (s *Server) handleSpeak(w http.ResponseWriter, r *http.Request) {
	var req speakRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if req.Speed == 0 {
		req.Speed = 1.0
	}
	if req.Speed < 0.5 || req.Speed > 2.0 {
		writeError(w, http.StatusBadRequest, "speed must be between 0.5 and 2.0")
		return
	}
	if req.Lang == "" {
		req.Lang = "en-us"
	}

	model, err := s.resolveSpeakModel(req.Model.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	voice, voiceIndex, err := resolveSpeakVoice(model, req.Voice.String(), req.Gender)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	textLen := len(req.Text)
	used, avail, ok := s.reserveChars(textLen)
	if !ok {
		w.Header().Set("Retry-After", "3")
		writeError(w, http.StatusServiceUnavailable,
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
			writeError(w, http.StatusServiceUnavailable,
				fmt.Sprintf("queue full (%d max), try again shortly", s.cfg.QueueMax))
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		s.failed.Add(1)
		if preflight.IsMemoryError(err) {
			w.Header().Set("Retry-After", "30")
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		log.Printf("speak error: %v", err)
		writeError(w, http.StatusInternalServerError, "synthesis failed")
		return
	}

	s.processed.Add(1)

	filename := fmt.Sprintf("output-%d.wav", time.Now().Unix())
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", wavBuf.Len()))
	w.Header().Set("X-Model", strconv.Itoa(model.Index))
	w.Header().Set("X-Model-ID", model.ID)
	w.Header().Set("X-Model-Name", model.Name)
	w.Header().Set("X-Voice", strconv.Itoa(voiceIndex))
	w.Header().Set("X-Voice-ID", voice.ID)
	w.Header().Set("X-Voice-Name", voice.Name)
	w.Header().Set("X-Audio-Duration", fmt.Sprintf("%.2fs", meta.duration))
	w.Header().Set("X-Processing-Time", fmt.Sprintf("%.2fs", meta.elapsed))
	w.Header().Set("X-RTF", fmt.Sprintf("%.2f", meta.rtf))
	w.WriteHeader(http.StatusOK)
	w.Write(wavBuf.Bytes())
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := append(
		append([]*registry.Model{}, registry.GetByKind(registry.KindTTS)...),
		registry.GetByKind(registry.KindSTT)...,
	)

	resp := make([]modelResponse, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}

		item := modelResponse{
			Index:    model.Index,
			ID:       model.ID,
			Kind:     string(model.Kind),
			Name:     model.Name,
			Location: string(model.Location),
		}

		if model.Location == registry.Local {
			downloaded := modelFilesDownloaded(s.dataDir, model)
			item.Downloaded = &downloaded

			sizeBytes := modelFilesSize(model)
			item.SizeBytes = &sizeBytes
		}

		if model.Kind == registry.KindTTS {
			voices := len(model.Voices)
			item.Voices = &voices
		}

		resp = append(resp, item)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVoices(w http.ResponseWriter, r *http.Request) {
	modelRef := strings.TrimSpace(r.URL.Query().Get("model"))
	models := registry.GetByKind(registry.KindTTS)

	if modelRef != "" {
		model := registry.Resolve(registry.KindTTS, modelRef)
		if model == nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown TTS model %q", modelRef))
			return
		}
		models = []*registry.Model{model}
	}

	var resp []voiceResponse
	for _, model := range models {
		for i := range model.Voices {
			voice := model.Voices[i]
			resp = append(resp, voiceResponse{
				Index:       i + 1,
				ID:          voice.ID,
				Name:        voice.Name,
				Gender:      voice.Gender,
				Accent:      voice.Accent,
				Description: voice.Description,
				Model:       model.Index,
				ModelID:     model.ID,
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	charsUsed := s.currentChars()
	speakStats := s.speakPool.Stats()
	listenStats := s.listenPool.Stats()

	writeJSON(w, http.StatusOK, statusResponse{
		Status: "ok",
		Speak: speakStatusResponse{
			Model:        s.speakModel.Index,
			ModelID:      s.speakModel.ID,
			ModelName:    s.speakModel.Name,
			Workers:      speakStats.Workers,
			EnginesReady: speakStats.EnginesReady,
			MaxChars:     s.cfg.MaxChars,
			CharsUsed:    charsUsed,
		},
		Listen: listenStatusResponse{
			Model:        s.listenModel.Index,
			ModelID:      s.listenModel.ID,
			ModelName:    s.listenModel.Name,
			Workers:      listenStats.Workers,
			EnginesReady: listenStats.EnginesReady,
		},
		Queued:    s.queued.Load(),
		Processed: s.processed.Load(),
		Failed:    s.failed.Load(),
		Uptime:    time.Since(s.startedAt).Round(time.Second).String(),
	})
}

func (s *Server) resolveSpeakModel(ref string) (*registry.Model, error) {
	return resolveConfiguredModel(registry.KindTTS, ref, s.speakModel, "speak")
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

func resolveSpeakVoice(
	model *registry.Model,
	voiceRef string,
	gender string,
) (speak.Voice, int, error) {
	if model == nil {
		return speak.Voice{}, 0, fmt.Errorf("missing TTS model")
	}

	switch model.ID {
	case "kokoro-82m-v1.0":
		voice, err := kokoro.ResolveVoice(model, voiceRef, gender)
		if err != nil {
			return speak.Voice{}, 0, err
		}
		index := voiceIndex(model, voice.ID)
		if index == 0 {
			return speak.Voice{}, 0, fmt.Errorf("voice %q is not registered", voice.ID)
		}
		return voice, index, nil
	default:
		return speak.Voice{}, 0, fmt.Errorf("TTS model %q is not supported yet", model.ID)
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
	req speakRequest,
	model *registry.Model,
	voice speak.Voice,
) (*bytes.Buffer, *synthMeta, error) {
	eng, err := s.speakPool.Borrow(ctx, s.queue, &s.queued)
	if err != nil {
		return nil, nil, err
	}
	defer s.speakPool.Return(eng)

	t0 := time.Now()
	var buf bytes.Buffer
	err = preflight.GuardMemoryError("speech synthesis", func() error {
		return eng.Speak(&buf, req.Text, speak.Options{
			Voice:     voice.ID,
			Gender:    req.Gender,
			Speed:     req.Speed,
			Lang:      req.Lang,
			ChunkSize: s.cfg.ChunkSize,
		})
	})
	if err != nil {
		if preflight.IsMemoryError(err) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("synthesize: %w", err)
	}

	elapsed := time.Since(t0).Seconds()
	duration := wavDurationFromSize(int64(buf.Len()), model.MetaInt("sample_rate", 24000))
	rtf := 0.0
	if duration > 0 {
		rtf = elapsed / duration
	}

	return &buf, &synthMeta{
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
	if s.speakPool != nil && !s.speakPool.Empty() {
		return
	}
	if s.listenPool != nil && !s.listenPool.Empty() {
		return
	}
	ort.Shutdown()
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
