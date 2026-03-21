// Package paths resolves platform-appropriate data directories.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir returns the default data directory for cattery.
//
//	Linux:   $XDG_DATA_HOME/cattery (default ~/.local/share/cattery)
//	macOS:   ~/Library/Application Support/cattery
//	Windows: %APPDATA%/cattery
//	Other:   ~/.cattery
func DataDir() string {
	switch runtime.GOOS {
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "cattery")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "cattery")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "cattery")
		}
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "cattery")
		}
	}

	// Fallback
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
