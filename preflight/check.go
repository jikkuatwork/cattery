// Package preflight provides a system readiness check for cattery TTS.
//
// Call Check before attempting synthesis to get a clean error instead of
// destabilizing the host under low-resource conditions.
package preflight

import (
	"fmt"
	"os"
	"strings"

	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/phonemize"
	"github.com/jikkuatwork/cattery/registry"
)

// Status describes why the system cannot run TTS.
type Status struct {
	OK     bool     // true if all checks pass
	Errors []string // human-readable reasons (empty if OK)
}

func (s *Status) Error() string {
	if s.OK {
		return ""
	}
	return "cattery: not ready: " + strings.Join(s.Errors, "; ")
}

func (s *Status) addError(msg string) {
	s.Errors = append(s.Errors, msg)
}

// MinMemoryMB is the minimum free RAM (in MB) required to safely load
// the ORT runtime and run inference. Conservative default based on
// profiling: idle ORT ~150 MB + model ~92 MB + inference headroom.
const MinMemoryMB = 300

// Check verifies that the system can run TTS right now.
// It checks: available RAM, espeak-ng, model files, and ORT library.
// The minMemMB parameter overrides MinMemoryMB (pass 0 for default).
func Check(minMemMB int) *Status {
	if minMemMB <= 0 {
		minMemMB = MinMemoryMB
	}

	s := &Status{OK: true}

	// 1. Available memory
	availMB := AvailableMemoryMB()
	if availMB >= 0 && availMB < minMemMB {
		s.OK = false
		s.addError(fmt.Sprintf("insufficient memory: %d MB available, need %d MB", availMB, minMemMB))
	}

	// 2. espeak-ng
	if !phonemize.Available() {
		s.OK = false
		s.addError("espeak-ng not found on PATH")
	}

	// 3. Model files
	dataDir := paths.DataDir()
	model := registry.Default(registry.KindTTS)
	if model != nil {
		for _, file := range model.Files {
			modelPath := paths.ModelFile(dataDir, model.ID, file.Filename)
			if _, err := os.Stat(modelPath); err != nil {
				s.OK = false
				s.addError(fmt.Sprintf("model not downloaded: %s (%s)", model.ID, file.Filename))
				break
			}
		}
	}

	// 4. ORT shared library
	ortDir := paths.ORTLib(dataDir)
	if !ortLibExists(ortDir) {
		s.OK = false
		s.addError("ORT runtime not downloaded")
	}

	return s
}

// CheckAvailableMemory checks only the memory constraint.
// Useful for a quick gate before each synthesis without re-checking
// static dependencies.
func CheckAvailableMemory(minMemMB int) error {
	if minMemMB <= 0 {
		minMemMB = MinMemoryMB
	}
	availMB := AvailableMemoryMB()
	if availMB >= 0 && availMB < minMemMB {
		return fmt.Errorf("cattery: insufficient memory: %d MB available, need %d MB", availMB, minMemMB)
	}
	return nil
}

// ortLibExists checks if any ORT shared library is present in the directory.
func ortLibExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "libonnxruntime") {
			return true
		}
	}
	return false
}
