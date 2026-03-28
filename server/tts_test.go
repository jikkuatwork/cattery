package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jikkuatwork/cattery/tts"
)

func TestAudioSpeech(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{
		speak: func(w io.Writer, text string, opts tts.Options) error {
			if text != "Hello world" {
				t.Fatalf("text = %q, want %q", text, "Hello world")
			}
			if opts.Lang != "en-us" {
				t.Fatalf("lang = %q, want en-us", opts.Lang)
			}
			_, err := w.Write([]byte("RIFF1234WAVEfmt data"))
			return err
		},
	}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"input":"Hello world"}`))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("Content-Type = %q, want audio/wav", got)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatal("expected Content-Disposition to be omitted")
	}
	for _, header := range []string{
		"X-Model", "X-Model-ID", "X-Model-Name",
		"X-Voice", "X-Voice-ID", "X-Voice-Name",
		"X-Audio-Duration", "X-Processing-Time", "X-RTF",
	} {
		if rec.Header().Get(header) != "" {
			t.Fatalf("expected %s header to be omitted", header)
		}
	}
}

func TestAudioSpeechRejectsMissingInput(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"voice":"bella"}`))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp openAIErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Error.Message != "input is required" {
		t.Fatalf("message = %q, want %q", resp.Error.Message, "input is required")
	}
}

func TestAudioSpeechRejectsUnsupportedResponseFormat(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"input":"hi","response_format":"mp3"}`))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp openAIErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if resp.Error.Message != "unsupported response_format" {
		t.Fatalf("message = %q, want %q", resp.Error.Message, "unsupported response_format")
	}
}

func TestOldTTSRouteReturns404(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodPost, "/v1/tts", strings.NewReader(`{"input":"hi"}`))
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
