package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateKey(t *testing.T) {
	key1, entry1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	key2, entry2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() second error = %v", err)
	}

	if !strings.HasPrefix(key1, keyPrefix) {
		t.Fatalf("key prefix = %q, want %q", key1[:len(keyPrefix)], keyPrefix)
	}
	if len(key1) != len(keyPrefix)+keyHexLength {
		t.Fatalf("key length = %d, want %d", len(key1), len(keyPrefix)+keyHexLength)
	}
	if key1 == key2 {
		t.Fatal("expected unique generated keys")
	}
	if entry1.ID != key1[:keyIDLength] {
		t.Fatalf("entry ID = %q, want %q", entry1.ID, key1[:keyIDLength])
	}
	if entry1.KeyHash == entry2.KeyHash {
		t.Fatal("expected unique key hashes")
	}
}

func TestAuthMiddleware(t *testing.T) {
	validKey, validEntry, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(valid): %v", err)
	}
	validEntry.Name = "valid"
	validEntry.RateLimit = defaultRateLimit

	disabledKey, disabledEntry, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(disabled): %v", err)
	}
	disabledEntry.Name = "disabled"
	disabledEntry.Disabled = true
	disabledEntry.RateLimit = defaultRateLimit

	store := NewKeyStore(filepath.Join(t.TempDir(), keysFilename))
	if err := store.Save([]KeyEntry{validEntry, disabledEntry}); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	handler := AuthMiddleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantError  string
	}{
		{name: "valid key", authHeader: "Bearer " + validKey, wantStatus: http.StatusNoContent},
		{name: "missing header", wantStatus: http.StatusUnauthorized, wantError: "authorization required"},
		{name: "invalid key", authHeader: "Bearer cat_notavalidkey", wantStatus: http.StatusUnauthorized, wantError: "invalid API key"},
		{name: "disabled key", authHeader: "Bearer " + disabledKey, wantStatus: http.StatusForbidden, wantError: "API key revoked"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantError == "" {
				return
			}

			var resp errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Unmarshal(error body): %v", err)
			}
			if resp.Error != tc.wantError {
				t.Fatalf("error = %q, want %q", resp.Error, tc.wantError)
			}
		})
	}
}

func TestRateLimiter(t *testing.T) {
	key, entry, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(): %v", err)
	}
	entry.RateLimit = 2

	store := NewKeyStore(filepath.Join(t.TempDir(), keysFilename))
	if err := store.Save([]KeyEntry{entry}); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter()
	limiter.now = func() time.Time { return now }

	handler := authMiddleware(store, limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(); rec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec := request(); rec.Code != http.StatusNoContent {
		t.Fatalf("second status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec := request(); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	} else if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}

	now = now.Add(time.Minute + time.Second)
	if rec := request(); rec.Code != http.StatusNoContent {
		t.Fatalf("post-reset status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestKeyStoreHotReload(t *testing.T) {
	key1, entry1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(first): %v", err)
	}
	entry1.Name = "a"
	entry1.RateLimit = defaultRateLimit

	key2, entry2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey(second): %v", err)
	}
	entry2.Name = "reload-target"
	entry2.RateLimit = defaultRateLimit

	store := NewKeyStore(filepath.Join(t.TempDir(), keysFilename))
	if err := store.Save([]KeyEntry{entry1}); err != nil {
		t.Fatalf("Save(first): %v", err)
	}
	if err := store.Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}

	handler := authMiddleware(store, NewRateLimiter())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req1.Header.Set("Authorization", "Bearer "+key1)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("first key status = %d, want %d", rec1.Code, http.StatusNoContent)
	}

	if err := store.Save([]KeyEntry{entry2}); err != nil {
		t.Fatalf("Save(second): %v", err)
	}

	reqOld := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqOld.Header.Set("Authorization", "Bearer "+key1)
	recOld := httptest.NewRecorder()
	handler.ServeHTTP(recOld, reqOld)
	if recOld.Code != http.StatusUnauthorized {
		t.Fatalf("old key status = %d, want %d", recOld.Code, http.StatusUnauthorized)
	}

	reqNew := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqNew.Header.Set("Authorization", "Bearer "+key2)
	recNew := httptest.NewRecorder()
	handler.ServeHTTP(recNew, reqNew)
	if recNew.Code != http.StatusNoContent {
		t.Fatalf("new key status = %d, want %d", recNew.Code, http.StatusNoContent)
	}
}
