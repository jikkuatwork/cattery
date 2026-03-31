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
	return filepath.Join(dataDir, "artefacts", "models", modelID)
}

// ArtefactFile returns the path for a model artefact.
func ArtefactFile(dataDir, modelID, filename string) string {
	return filepath.Join(ModelDir(dataDir, modelID), filepath.FromSlash(filename))
}

// VoiceFile returns the path for a voice file.
func VoiceFile(dataDir, modelID, voiceID string) string {
	return ArtefactFile(dataDir, modelID, "voices/"+voiceID+".bin")
}

// ModelFile returns the path for a model file.
func ModelFile(dataDir, modelID, filename string) string {
	return ArtefactFile(dataDir, modelID, filename)
}

// ORTLib returns the directory that holds the ORT shared library.
func ORTLib(dataDir string) string {
	return filepath.Join(dataDir, "ort")
}

// EspeakDir returns the espeak-ng installation directory.
func EspeakDir(dataDir string) string {
	return filepath.Join(dataDir, "espeak-ng")
}

// EspeakBin returns the path to the bundled espeak-ng binary.
func EspeakBin(dataDir string) string {
	return filepath.Join(EspeakDir(dataDir), "bin", "espeak-ng")
}

// EspeakData returns the path to the bundled espeak-ng data directory.
func EspeakData(dataDir string) string {
	return filepath.Join(EspeakDir(dataDir), "data")
}
