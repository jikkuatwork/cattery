// Package server provides a REST API for cattery TTS synthesis.
//
// Endpoints:
//
//	POST /v1/tts      — synthesize text, returns WAV audio
//	GET  /v1/voices   — list available voices
//	GET  /v1/status   — server health and queue depth
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jikkuatwork/cattery/download"
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
	Port        int           // listen port (default 7100)
	Workers     int           // max concurrent synthesis jobs (default 2)
	QueueMax    int           // max waiting requests before 503 (default 5)
	MaxChars    int           // shared character budget across all queued+active requests (default 500)
	IdleTimeout time.Duration // evict engines after this idle duration (default 300s)
	KeepAlive   bool          // if true, pre-warm engines and never evict
	ModelID     string        // model to use (default registry.Default)
}

// Server is the cattery TTS HTTP server.
type Server struct {
	cfg   Config
	model *registry.Model
	mux   *http.ServeMux

	// Worker pool: buffered channel acts as a semaphore.
	sem chan struct{}

	// Shared character budget — bounds total ORT memory.
	charsMu   sync.Mutex
	charsUsed int

	// Stats
	queued    atomic.Int64
	processed atomic.Int64
	failed    atomic.Int64

	// Lazy engine pool — engines created on demand, evicted after idle timeout.
	engines   chan speak.Engine // idle engines ready to use
	poolMu    sync.Mutex
	poolCount int // total engines created (idle + active)
	idleTimer *time.Timer

	dataDir   string
	modelPath string
	ortLib    string
}

// TTSRequest is the JSON body for POST /v1/tts.
type TTSRequest struct {
	Text   string  `json:"text"`
	Voice  string  `json:"voice,omitempty"`  // name, ID, or number; empty = random
	Gender string  `json:"gender,omitempty"` // "male" or "female"; empty = any
	Speed  float64 `json:"speed,omitempty"`  // 0.5–2.0; 0 = default 1.0
	Lang   string  `json:"lang,omitempty"`   // phonemizer language; empty = "en-us"
}

type voiceResponse struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Gender      string `json:"gender"`
	Accent      string `json:"accent"`
	Description string `json:"description"`
}

type statusResponse struct {
	Status       string `json:"status"`
	Model        string `json:"model"`
	Workers      int    `json:"workers"`
	EnginesReady int    `json:"engines_ready"`
	QueueMax     int    `json:"queue_max"`
	MaxChars     int    `json:"max_chars"`
	CharsUsed    int    `json:"chars_used"`
	Queued       int64  `json:"queued"`
	Processed    int64  `json:"processed"`
	Failed       int64  `json:"failed"`
	Uptime       string `json:"uptime"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var startTime time.Time

// New creates and initializes a Server. It downloads the model and ORT
// runtime, but only loads engines on demand (unless --keep-alive is set).
func New(cfg Config) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = 7100
	}
	if cfg.Workers == 0 {
		cfg.Workers = 2
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
	if cfg.ModelID == "" {
		cfg.ModelID = registry.Default
	}

	model := registry.Get(cfg.ModelID)
	if model == nil {
		return nil, fmt.Errorf("unknown model %q", cfg.ModelID)
	}

	if !phonemize.Available() {
		return nil, fmt.Errorf("espeak-ng not found (required)")
	}

	// Download model + ORT only. Voices are fetched lazily by Speak().
	dataDir := paths.DataDir()
	result, err := download.Ensure(dataDir, model, nil)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	engines := make(chan speak.Engine, cfg.Workers)

	s := &Server{
		cfg:       cfg,
		model:     model,
		mux:       http.NewServeMux(),
		sem:       make(chan struct{}, cfg.Workers+cfg.QueueMax),
		engines:   engines,
		dataDir:   dataDir,
		modelPath: result.ModelPath,
		ortLib:    result.ORTLib,
	}

	// Pre-warm: init ORT + create engines upfront.
	// Lazy mode: ORT loaded on first request.
	if cfg.KeepAlive {
		if err := ort.Init(result.ORTLib); err != nil {
			return nil, fmt.Errorf("init ORT: %w", err)
		}
		for i := 0; i < cfg.Workers; i++ {
			eng, err := kokoro.New(result.ModelPath, dataDir)
			if err != nil {
				return nil, fmt.Errorf("create engine %d: %w", i, err)
			}
			engines <- eng
			s.poolCount++
		}
		log.Printf("pre-warmed %d engine(s)", cfg.Workers)
	}

	s.mux.HandleFunc("POST /v1/tts", s.handleTTS)
	s.mux.HandleFunc("GET /v1/voices", s.handleVoices)
	s.mux.HandleFunc("GET /v1/status", s.handleStatus)

	// pprof endpoints for profiling
	s.mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	s.mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	s.mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	s.mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	s.mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	s.mux.Handle("GET /debug/pprof/allocs", pprof.Handler("allocs"))
	s.mux.Handle("GET /debug/pprof/goroutine", pprof.Handler("goroutine"))

	startTime = time.Now()
	return s, nil
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	mode := fmt.Sprintf("idle timeout %s", s.cfg.IdleTimeout)
	if s.cfg.KeepAlive {
		mode = "keep-alive"
	}
	log.Printf("cattery server listening on %s (%d workers, %d max chars, queue %d, %s)",
		addr, s.cfg.Workers, s.cfg.MaxChars, s.cfg.QueueMax, mode)
	return http.ListenAndServe(addr, s.mux)
}

// Shutdown releases all engine resources.
func (s *Server) Shutdown() {
	s.poolMu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.poolMu.Unlock()
	s.evictEngines()
	ort.Shutdown()
}

// --- handlers ---

func (s *Server) handleTTS(w http.ResponseWriter, r *http.Request) {
	var req TTSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Text) == "" {
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

	// Resolve voice.
	voice, err := kokoro.ResolveVoice(s.model, req.Voice, req.Gender)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Voice = voice.ID

	// Admission: check shared character budget, then queue slot.
	textLen := len(strings.TrimSpace(req.Text))

	s.charsMu.Lock()
	if s.charsUsed+textLen > s.cfg.MaxChars {
		avail := s.cfg.MaxChars - s.charsUsed
		s.charsMu.Unlock()
		w.Header().Set("Retry-After", "3")
		writeError(w, http.StatusServiceUnavailable,
			fmt.Sprintf("character budget exhausted (%d/%d used, need %d, %d available)",
				s.charsUsed, s.cfg.MaxChars, textLen, avail))
		return
	}
	s.charsUsed += textLen
	s.charsMu.Unlock()

	releaseChars := func() {
		s.charsMu.Lock()
		s.charsUsed -= textLen
		s.charsMu.Unlock()
	}

	// Queue slot: try to enqueue (non-blocking). If queue is full, reject.
	s.queued.Add(1)
	select {
	case s.sem <- struct{}{}:
	default:
		s.queued.Add(-1)
		releaseChars()
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable,
			fmt.Sprintf("queue full (%d max), try again shortly", s.cfg.QueueMax))
		return
	}
	defer func() {
		<-s.sem
		s.queued.Add(-1)
		releaseChars()
	}()

	// Preflight: check available memory before loading engine.
	if err := preflight.CheckAvailableMemory(0); err != nil {
		s.failed.Add(1)
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// Synthesize.
	wavBuf, meta, err := s.synthesize(req, voice)
	if err != nil {
		s.failed.Add(1)
		log.Printf("tts error: %v", err)
		writeError(w, http.StatusInternalServerError, "synthesis failed")
		return
	}
	s.processed.Add(1)

	filename := fmt.Sprintf("output-%d.wav", time.Now().Unix())

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", wavBuf.Len()))
	w.Header().Set("X-Voice", voice.Name)
	w.Header().Set("X-Audio-Duration", fmt.Sprintf("%.2fs", meta.duration))
	w.Header().Set("X-Processing-Time", fmt.Sprintf("%.2fs", meta.elapsed))
	w.Header().Set("X-RTF", fmt.Sprintf("%.2f", meta.rtf))
	w.WriteHeader(http.StatusOK)
	w.Write(wavBuf.Bytes())
}

func (s *Server) handleVoices(w http.ResponseWriter, r *http.Request) {
	voices := kokoro.Voices(s.model)
	resp := make([]voiceResponse, len(voices))
	for i, v := range voices {
		resp[i] = voiceResponse{
			ID:          i + 1,
			Key:         v.ID,
			Name:        v.Name,
			Gender:      v.Gender,
			Accent:      v.Accent,
			Description: v.Description,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.charsMu.Lock()
	charsUsed := s.charsUsed
	s.charsMu.Unlock()

	s.poolMu.Lock()
	enginesReady := len(s.engines)
	s.poolMu.Unlock()

	writeJSON(w, http.StatusOK, statusResponse{
		Status:       "ok",
		Model:        s.model.Name + " (" + s.model.ID + ")",
		Workers:      s.cfg.Workers,
		EnginesReady: enginesReady,
		QueueMax:     s.cfg.QueueMax,
		MaxChars:     s.cfg.MaxChars,
		CharsUsed:    charsUsed,
		Queued:       s.queued.Load(),
		Processed:    s.processed.Load(),
		Failed:       s.failed.Load(),
		Uptime:       time.Since(startTime).Round(time.Second).String(),
	})
}

// --- engine pool ---

// borrowEngine returns an engine, creating one on demand if needed.
// Re-initializes ORT if it was fully shut down after idle eviction.
// The caller MUST call returnEngine when done.
func (s *Server) borrowEngine() (speak.Engine, error) {
	s.poolMu.Lock()

	// Stop idle timer — we're active.
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	// Try to grab an idle engine.
	select {
	case eng := <-s.engines:
		s.poolMu.Unlock()
		return eng, nil
	default:
	}

	// None idle — create new if under capacity.
	if s.poolCount < s.cfg.Workers {
		s.poolCount++
		s.poolMu.Unlock()

		// Re-init ORT if it was shut down (idempotent if already loaded).
		if err := ort.Init(s.ortLib); err != nil {
			s.poolMu.Lock()
			s.poolCount--
			s.poolMu.Unlock()
			return nil, fmt.Errorf("re-init ORT: %w", err)
		}

		eng, err := kokoro.New(s.modelPath, s.dataDir)
		if err != nil {
			s.poolMu.Lock()
			s.poolCount--
			s.poolMu.Unlock()
			return nil, err
		}
		log.Printf("engine loaded (pool: %d)", s.poolCount)
		return eng, nil
	}

	// At capacity — wait for one to be returned.
	s.poolMu.Unlock()
	eng := <-s.engines
	return eng, nil
}

// returnEngine puts an engine back into the pool and resets the idle timer.
func (s *Server) returnEngine(eng speak.Engine) {
	s.engines <- eng

	if s.cfg.KeepAlive {
		return
	}

	s.poolMu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.cfg.IdleTimeout, s.evictEngines)
	s.poolMu.Unlock()
}

// evictEngines closes all idle engines and shuts down ORT entirely,
// releasing all C-level memory via dlclose. ORT is re-initialized
// on the next request.
func (s *Server) evictEngines() {
	s.poolMu.Lock()
	var closed int
	for {
		select {
		case eng := <-s.engines:
			if err := eng.Close(); err != nil {
				log.Printf("close engine: %v", err)
			}
			s.poolCount--
			closed++
		default:
			s.idleTimer = nil
			// If all engines evicted, fully shut down ORT to reclaim C memory.
			if s.poolCount == 0 {
				ort.Shutdown()
			}
			s.poolMu.Unlock()
			if closed > 0 {
				log.Printf("evicted %d engine(s), ORT unloaded", closed)
			}
			return
		}
	}
}

// --- synthesis ---

type synthMeta struct {
	duration float64
	elapsed  float64
	rtf      float64
}

func (s *Server) synthesize(req TTSRequest, voice speak.Voice) (*bytes.Buffer, *synthMeta, error) {
	// Borrow engine from pool (lazy-loaded, evicted on idle).
	eng, err := s.borrowEngine()
	if err != nil {
		return nil, nil, fmt.Errorf("load engine: %w", err)
	}
	defer s.returnEngine(eng)

	t0 := time.Now()
	var buf bytes.Buffer
	err = eng.Speak(&buf, req.Text, speak.Options{
		Voice:  voice.ID,
		Gender: req.Gender,
		Speed:  req.Speed,
		Lang:   req.Lang,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("synthesize: %w", err)
	}
	elapsed := time.Since(t0).Seconds()

	duration := wavDurationFromSize(int64(buf.Len()), s.model.SampleRate)
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

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func wavDurationFromSize(size int64, sampleRate int) float64 {
	if size <= 44 || sampleRate <= 0 {
		return 0
	}
	dataBytes := size - 44
	return float64(dataBytes/2) / float64(sampleRate)
}
