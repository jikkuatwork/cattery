package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jikkuatwork/cattery/paths"
)

const (
	keyPrefix           = "cat_"
	keyHexLength        = 32
	keyIDLength         = 12
	defaultRateLimit    = 60
	defaultRateWindow   = time.Minute
	defaultRateEntryTTL = 10 * time.Minute
	keysFilename        = "keys.json"
)

var errInvalidAPIKey = errors.New("invalid API key")

const DefaultKeyRateLimit = defaultRateLimit

// KeyEntry is a stored API key descriptor.
type KeyEntry struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"key_hash"`
	Name      string    `json:"name"`
	Created   time.Time `json:"created"`
	RateLimit int       `json:"rate_limit"`
	Disabled  bool      `json:"disabled"`
}

// KeyStore loads and hot-reloads API keys from disk.
type KeyStore struct {
	path string

	mu      sync.RWMutex
	entries []KeyEntry
	byHash  map[string]KeyEntry
	modTime time.Time
	size    int64
}

// NewKeyStore returns a keystore for the given file path.
func NewKeyStore(path string) *KeyStore {
	return &KeyStore{
		path:   path,
		byHash: make(map[string]KeyEntry),
	}
}

// DefaultKeyStore returns the default keystore in ~/.cattery/keys.json.
func DefaultKeyStore() *KeyStore {
	return NewKeyStore(filepath.Join(paths.DataDir(), keysFilename))
}

// GenerateKey creates a new API key and its stored entry.
func GenerateKey() (string, KeyEntry, error) {
	var raw [keyHexLength / 2]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", KeyEntry{}, fmt.Errorf("generate key: %w", err)
	}

	fullKey := keyPrefix + hex.EncodeToString(raw[:])
	return fullKey, KeyEntry{
		ID:      fullKey[:keyIDLength],
		KeyHash: hashKey(fullKey),
		Created: time.Now().UTC(),
	}, nil
}

// Path returns the backing file path.
func (s *KeyStore) Path() string {
	return s.path
}

// Load reads the keystore file.
func (s *KeyStore) Load() error {
	info, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	return s.loadFromFile(info)
}

// Entries returns a copy of the cached entries.
func (s *KeyStore) Entries() []KeyEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneKeyEntries(s.entries)
}

// Save writes the provided entries to disk atomically and refreshes the cache.
func (s *KeyStore) Save(entries []KeyEntry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create keys directory: %w", err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), "keys-*.json")
	if err != nil {
		return fmt.Errorf("create temp keys file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp keys file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp keys file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace keys file: %w", err)
	}

	info, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("stat keys file: %w", err)
	}
	return s.setEntries(entries, info.ModTime(), info.Size())
}

// LookupToken reloads the keystore when needed and looks up a full API key.
func (s *KeyStore) LookupToken(token string) (KeyEntry, error) {
	if err := s.reloadIfChanged(); err != nil {
		return KeyEntry{}, err
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return KeyEntry{}, errInvalidAPIKey
	}

	keyHash := hashKey(token)

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.byHash[keyHash]
	if !ok {
		return KeyEntry{}, errInvalidAPIKey
	}
	return entry, nil
}

func (s *KeyStore) reloadIfChanged() error {
	info, err := os.Stat(s.path)
	if err != nil {
		return err
	}

	s.mu.RLock()
	unchanged := info.Size() == s.size && info.ModTime().Equal(s.modTime)
	s.mu.RUnlock()
	if unchanged {
		return nil
	}

	return s.loadFromFile(info)
}

func (s *KeyStore) loadFromFile(info os.FileInfo) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read keys file: %w", err)
	}

	var entries []KeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse keys file: %w", err)
	}

	return s.setEntries(entries, info.ModTime(), info.Size())
}

func (s *KeyStore) setEntries(entries []KeyEntry, modTime time.Time, size int64) error {
	byHash := make(map[string]KeyEntry, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.KeyHash) == "" {
			return fmt.Errorf("key %q is missing key_hash", entry.ID)
		}
		byHash[entry.KeyHash] = entry
	}

	s.mu.Lock()
	s.entries = cloneKeyEntries(entries)
	s.byHash = byHash
	s.modTime = modTime
	s.size = size
	s.mu.Unlock()
	return nil
}

func cloneKeyEntries(entries []KeyEntry) []KeyEntry {
	out := make([]KeyEntry, len(entries))
	copy(out, entries)
	return out
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func parseBearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", errors.New("authorization required")
	}

	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errInvalidAPIKey
	}
	if !strings.HasPrefix(parts[1], keyPrefix) {
		return "", errInvalidAPIKey
	}
	return parts[1], nil
}

// AuthMiddleware checks bearer API keys and applies per-key rate limits.
func AuthMiddleware(store *KeyStore) func(http.Handler) http.Handler {
	return authMiddleware(store, NewRateLimiter())
}

func authMiddleware(store *KeyStore, limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := parseBearerToken(r.Header.Get("Authorization"))
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer")
				if err.Error() == "authorization required" {
					writeError(w, http.StatusUnauthorized, err.Error())
					return
				}
				writeError(w, http.StatusUnauthorized, errInvalidAPIKey.Error())
				return
			}

			entry, err := store.LookupToken(token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer")
				if errors.Is(err, errInvalidAPIKey) {
					writeError(w, http.StatusUnauthorized, errInvalidAPIKey.Error())
					return
				}
				writeError(w, http.StatusInternalServerError, "auth store unavailable")
				return
			}
			if entry.Disabled {
				writeError(w, http.StatusForbidden, "API key revoked")
				return
			}

			allowed, retryAfter := limiter.Allow(entry.ID, entry.RateLimit)
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type rateLimitWindow struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

// RateLimiter applies per-key fixed-window limits.
type RateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	window  time.Duration
	ttl     time.Duration
	entries map[string]rateLimitWindow
}

// NewRateLimiter returns a 1-minute fixed-window limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		now:     time.Now,
		window:  defaultRateWindow,
		ttl:     defaultRateEntryTTL,
		entries: make(map[string]rateLimitWindow),
	}
}

// Allow reports whether a request is within the configured limit.
func (l *RateLimiter) Allow(keyID string, limit int) (bool, int) {
	if limit <= 0 {
		return true, 0
	}

	now := l.now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	for id, entry := range l.entries {
		if now.Sub(entry.lastSeen) > l.ttl {
			delete(l.entries, id)
		}
	}

	entry := l.entries[keyID]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		entry = rateLimitWindow{
			windowStart: now,
			lastSeen:    now,
		}
	}

	entry.lastSeen = now
	if entry.count >= limit {
		l.entries[keyID] = entry
		retryAfter := int(math.Ceil(entry.windowStart.Add(l.window).Sub(now).Seconds()))
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}

	entry.count++
	l.entries[keyID] = entry
	return true, 0
}
