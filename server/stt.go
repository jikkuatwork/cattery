package server

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/stt/moonshine"
)

type sttResponse struct {
	Text           string  `json:"text"`
	Duration       float64 `json:"duration"`
	ProcessingTime float64 `json:"processing_time"`
	RTF            float64 `json:"rtf"`
	Model          int     `json:"model"`
	ModelID        string  `json:"model_id"`
	ModelName      string  `json:"model_name"`
}

func (s *Server) handleSTT(w http.ResponseWriter, r *http.Request) {
	model, err := s.resolveSTTModel(r.URL.Query().Get("model"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if err := validateSTTContentType(contentType); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}

	eng, err := s.sttPool.Borrow(r.Context(), s.queue, &s.queued)
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
	defer s.sttPool.Return(eng)

	var result *stt.Result
	err = preflight.GuardMemoryError("transcription", func() error {
		var innerErr error
		result, innerErr = eng.Transcribe(r.Body, stt.Options{
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
	writeJSON(w, http.StatusOK, sttResponse{
		Text:           result.Text,
		Duration:       result.Duration,
		ProcessingTime: result.Elapsed,
		RTF:            result.RTF,
		Model:          model.Index,
		ModelID:        model.ID,
		ModelName:      model.Name,
	})
}

func (s *Server) resolveSTTModel(ref string) (*registry.Model, error) {
	return resolveConfiguredModel(registry.KindSTT, ref, s.sttModel, "stt")
}

func newSTTEngine(model *registry.Model, dataDir string) (stt.Engine, error) {
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

func validateSTTContentType(contentType string) error {
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
