package server

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/jikkuatwork/cattery/listen"
	"github.com/jikkuatwork/cattery/listen/moonshine"
	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
)

type listenResponse struct {
	Text           string  `json:"text"`
	Duration       float64 `json:"duration"`
	ProcessingTime float64 `json:"processing_time"`
	RTF            float64 `json:"rtf"`
	Model          int     `json:"model"`
	ModelID        string  `json:"model_id"`
	ModelName      string  `json:"model_name"`
}

func (s *Server) handleListen(w http.ResponseWriter, r *http.Request) {
	model, err := s.resolveListenModel(r.URL.Query().Get("model"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if err := validateListenContentType(contentType); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}

	eng, err := s.listenPool.Borrow(r.Context(), s.queue, &s.queued)
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
		writeError(w, http.StatusInternalServerError, "transcription failed")
		return
	}
	defer s.listenPool.Return(eng)

	var result *listen.Result
	err = preflight.GuardMemoryError("transcription", func() error {
		var innerErr error
		result, innerErr = eng.Transcribe(r.Body, listen.Options{
			Lang:      strings.TrimSpace(r.URL.Query().Get("lang")),
			ChunkSize: s.cfg.ChunkSize,
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.processed.Add(1)
	writeJSON(w, http.StatusOK, listenResponse{
		Text:           result.Text,
		Duration:       result.Duration,
		ProcessingTime: result.Elapsed,
		RTF:            result.RTF,
		Model:          model.Index,
		ModelID:        model.ID,
		ModelName:      model.Name,
	})
}

func (s *Server) resolveListenModel(ref string) (*registry.Model, error) {
	return resolveConfiguredModel(registry.KindSTT, ref, s.listenModel, "listen")
}

func newListenEngine(model *registry.Model, dataDir string) (listen.Engine, error) {
	if model == nil {
		return nil, fmt.Errorf("missing STT model")
	}

	switch model.ID {
	case "moonshine-tiny-v1.0":
		return moonshine.New(paths.ModelDir(dataDir, model.ID), model.Meta)
	default:
		return nil, fmt.Errorf("STT model %q is not supported yet", model.ID)
	}
}

func validateListenContentType(contentType string) error {
	if contentType == "" {
		return nil
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid Content-Type %q", contentType)
	}

	switch mediaType {
	case "audio/wav", "application/octet-stream":
		return nil
	default:
		return fmt.Errorf("unsupported Content-Type %q", mediaType)
	}
}
