package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jikkuatwork/cattery/stt"
)

func TestAudioTranscriptionsJSON(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{
		transcribe: func(r io.Reader, opts stt.Options) (*stt.Result, error) {
			if opts.Lang != "en" {
				t.Fatalf("lang = %q, want en", opts.Lang)
			}
			return &stt.Result{Text: "transcribed text"}, nil
		},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := part.Write([]byte("wav-data")); err != nil {
		t.Fatalf("part.Write(): %v", err)
	}
	if err := writer.WriteField("language", "en"); err != nil {
		t.Fatalf("WriteField(language): %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var resp audioTranscriptionJSONResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Text != "transcribed text" {
		t.Fatalf("text = %q, want %q", resp.Text, "transcribed text")
	}
}

func TestAudioTranscriptionsTextFormat(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{
		transcribe: func(io.Reader, stt.Options) (*stt.Result, error) {
			return &stt.Result{Text: "plain transcript"}, nil
		},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := part.Write([]byte("wav-data")); err != nil {
		t.Fatalf("part.Write(): %v", err)
	}
	if err := writer.WriteField("response_format", "text"); err != nil {
		t.Fatalf("WriteField(response_format): %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if body := rec.Body.String(); body != "plain transcript" {
		t.Fatalf("body = %q, want %q", body, "plain transcript")
	}
}

func TestAudioTranscriptionsRejectsNonMultipart(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", strings.NewReader("raw"))
	req.Header.Set("Content-Type", "audio/wav")
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp openAIErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Error.Message != "multipart/form-data request is required" {
		t.Fatalf("message = %q, want %q", resp.Error.Message, "multipart/form-data request is required")
	}
}

func TestAudioTranscriptionsRejectsMissingFile(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close(): %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp openAIErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Error.Message != "file is required" {
		t.Fatalf("message = %q, want %q", resp.Error.Message, "file is required")
	}
}

func TestOldSTTRouteReturns404(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/stt", strings.NewReader(""))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
