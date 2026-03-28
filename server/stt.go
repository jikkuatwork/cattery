package server

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/preflight"
	"github.com/jikkuatwork/cattery/registry"
	"github.com/jikkuatwork/cattery/stt"
	"github.com/jikkuatwork/cattery/stt/moonshine"
)

type audioTranscriptionForm struct {
	File           multipart.File
	Filename       string
	Model          string
	Language       string
	ResponseFormat string
}

type audioTranscriptionJSONResponse struct {
	Text string `json:"text"`
}

func (s *Server) handleAudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	form, err := parseAudioTranscriptionForm(r)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer form.File.Close()

	if _, err := s.resolveSTTModel(form.Model); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	eng, err := s.borrowSTT(r.Context())
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
		writeOpenAIError(w, http.StatusInternalServerError, "transcription failed")
		return
	}
	defer s.sttPool.Return(eng)

	var result *stt.Result
	err = preflight.GuardMemoryError("transcription", func() error {
		var innerErr error
		result, innerErr = eng.Transcribe(form.File, stt.Options{
			Lang:      form.Language,
			ChunkSize: s.cfg.ChunkSize,
		})
		return innerErr
	})
	if err != nil {
		s.failed.Add(1)
		if preflight.IsMemoryError(err) {
			w.Header().Set("Retry-After", "30")
			writeOpenAIError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.processed.Add(1)
	writeAudioTranscriptionResponse(w, result.Text, form.ResponseFormat)
}

func parseAudioTranscriptionForm(r *http.Request) (audioTranscriptionForm, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return audioTranscriptionForm{}, fmt.Errorf("multipart/form-data request is required")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return audioTranscriptionForm{}, fmt.Errorf("file is required")
	}

	responseFormat := strings.TrimSpace(r.FormValue("response_format"))
	switch responseFormat {
	case "", "json", "text":
	default:
		file.Close()
		return audioTranscriptionForm{}, fmt.Errorf("unsupported response_format")
	}

	return audioTranscriptionForm{
		File:           file,
		Filename:       header.Filename,
		Model:          strings.TrimSpace(r.FormValue("model")),
		Language:       strings.TrimSpace(r.FormValue("language")),
		ResponseFormat: responseFormat,
	}, nil
}

func writeAudioTranscriptionResponse(w http.ResponseWriter, text string, format string) {
	if format == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(text))
		return
	}

	writeJSON(w, http.StatusOK, audioTranscriptionJSONResponse{Text: text})
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
