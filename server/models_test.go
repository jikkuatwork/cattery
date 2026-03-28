package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsReturnsOpenAIEnvelope(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if raw["object"] != "list" {
		t.Fatalf("object = %v, want list", raw["object"])
	}

	data, ok := raw["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("data = %#v, want non-empty array", raw["data"])
	}

	for _, item := range data {
		model, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item = %#v, want object", item)
		}
		if model["object"] != "model" {
			t.Fatalf("model object = %v, want model", model["object"])
		}
		for _, field := range []string{"index", "kind", "name", "location", "downloaded", "size_bytes", "voices"} {
			if _, exists := model[field]; exists {
				t.Fatalf("unexpected legacy field %q in model object", field)
			}
		}
	}
}

func TestOldVoicesRouteReturns404(t *testing.T) {
	srv := newTestAPIServer(t, &stubTTSEngine{}, &stubSTTEngine{})

	req := httptest.NewRequest(http.MethodGet, "/v1/voices", nil)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
