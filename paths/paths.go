// Package paths resolves platform-appropriate data directories.
package paths

import (
	"os"
	"path/filepath"
)

// DataDir returns the data directory for cattery (~/.cattery).
func DataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cattery")
	}
	return ".cattery"
}

// ModelDir returns the path for a model's files within the data directory.
func ModelDir(dataDir, modelID string) string {
	return filepath.Join(dataDir, "models", modelID)
}

// VoiceFile returns the path for a voice file.
func VoiceFile(dataDir, modelID, voiceID string) string {
	return filepath.Join(dataDir, "models", modelID, "voices", voiceID+".bin")
}

// ModelFile returns the path for the model ONNX file.
func ModelFile(dataDir, modelID, filename string) string {
	return filepath.Join(dataDir, "models", modelID, filename)
}

// ORTLib returns a glob pattern to find the ORT shared library.
func ORTLib(dataDir string) string {
	return filepath.Join(dataDir, "ort")
}
