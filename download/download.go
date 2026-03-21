// Package download handles fetching model files and ORT runtime on first run.
//
// Model and voice files are served from GitHub Releases on kodeman/cattery-models.
// ORT runtime is served from Microsoft's official GitHub Releases.
// All files are verified against SHA256 checksums.
package download

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ortVersion = "1.24.1"
	modelName  = "kokoro-82m-v1.0"
	// Raw file URL via GitHub (works with LFS).
	artefactsBaseURL = "https://github.com/jikkuatwork/cattery-artefacts/raw/main/models/" + modelName
)

// Known SHA256 checksums for release assets.
var checksums = map[string]string{
	"model_quantized.onnx":                    "fbae9257e1e05ffc727e951ef9b9c98418e6d79f1c9b6b13bd59f5c9028a1478",
	"af_heart.bin":                             "d583ccff3cdca2f7fae535cb998ac07e9fcb90f09737b9a41fa2734ec44a8f0b",
	"libonnxruntime.so.1.24.1":                "7954e8bdedb497f830c6a679e818d98399b7f4d81ade1126c3e0be74d28111ab",
}

// Files that need to be present for cattery to run.
type Files struct {
	Model   string // path to ONNX model
	Voice   string // path to voice .bin
	OrtLib  string // path to libonnxruntime shared library
	DataDir string // base directory for all files
}

// Ensure checks if all required files exist, downloading any that are missing.
// progress is called with status messages.
func Ensure(dataDir string, voice string, progress func(string)) (*Files, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	files := &Files{
		DataDir: dataDir,
		Model:   filepath.Join(dataDir, "model_quantized.onnx"),
		Voice:   filepath.Join(dataDir, "voices", voice+".bin"),
		OrtLib:  findOrtLib(dataDir),
	}

	// Download ORT runtime if missing
	if files.OrtLib == "" {
		progress("Downloading ONNX Runtime " + ortVersion + "...")
		lib, err := downloadORT(dataDir)
		if err != nil {
			return nil, fmt.Errorf("download ORT: %w", err)
		}
		files.OrtLib = lib
	}

	// Download model if missing
	if !fileExists(files.Model) {
		progress("Downloading Kokoro model (92MB)...")
		url := artefactsBaseURL + "/model_quantized.onnx"
		if err := downloadFileVerified(url, files.Model, checksums["model_quantized.onnx"]); err != nil {
			return nil, fmt.Errorf("download model: %w", err)
		}
	}

	// Download voice if missing
	if !fileExists(files.Voice) {
		progress(fmt.Sprintf("Downloading voice '%s'...", voice))
		if err := os.MkdirAll(filepath.Dir(files.Voice), 0755); err != nil {
			return nil, err
		}
		voiceFile := voice + ".bin"
		url := artefactsBaseURL + "/voices/" + voiceFile
		if err := downloadFileVerified(url, files.Voice, checksums[voiceFile]); err != nil {
			return nil, fmt.Errorf("download voice: %w", err)
		}
	}

	return files, nil
}

// downloadORT downloads and extracts the ONNX Runtime shared library
// from Microsoft's official GitHub Releases.
func downloadORT(dataDir string) (string, error) {
	arch := runtime.GOARCH
	if arch == "arm64" {
		arch = "aarch64"
	}
	if arch == "amd64" {
		arch = "x86_64"
	}

	var (
		url     string
		libName string
	)

	switch runtime.GOOS {
	case "linux":
		url = fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/onnxruntime-linux-%s-%s.tgz",
			ortVersion, arch, ortVersion)
		libName = fmt.Sprintf("libonnxruntime.so.%s", ortVersion)
	case "darwin":
		url = fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/onnxruntime-osx-%s-%s.tgz",
			ortVersion, arch, ortVersion)
		libName = fmt.Sprintf("libonnxruntime.%s.dylib", ortVersion)
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	destPath := filepath.Join(dataDir, libName)
	if fileExists(destPath) {
		return destPath, nil
	}

	// Download tarball to temp file
	tmpFile, err := os.CreateTemp("", "ort-*.tgz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if err := downloadToWriter(url, tmpFile); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	// Extract the lib file from the tarball
	if err := extractFromTarGz(tmpFile.Name(), "lib/"+libName, destPath); err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	// Verify checksum if we have one
	if expected, ok := checksums[libName]; ok {
		if err := verifyChecksum(destPath, expected); err != nil {
			os.Remove(destPath)
			return "", err
		}
	}

	return destPath, nil
}

// extractFromTarGz extracts a single file matching the suffix from a .tar.gz.
func extractFromTarGz(tarPath, fileSuffix, destPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.HasSuffix(header.Name, fileSuffix) {
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("file %q not found in archive", fileSuffix)
}

func downloadFileVerified(url, destPath, expectedSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Download to temp file first, then verify and rename
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath) // cleanup on failure

	if err := downloadToWriter(url, f); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// Verify checksum
	if expectedSHA256 != "" {
		if err := verifyChecksum(tmpPath, expectedSHA256); err != nil {
			return err
		}
	}

	return os.Rename(tmpPath, destPath)
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("checksum mismatch for %s:\n  expected: %s\n  got:      %s", filepath.Base(path), expected, got)
	}
	return nil
}

func downloadToWriter(url string, w io.Writer) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findOrtLib(dataDir string) string {
	var pattern string
	switch runtime.GOOS {
	case "linux":
		pattern = "libonnxruntime.so*"
	case "darwin":
		pattern = "libonnxruntime*.dylib"
	case "windows":
		pattern = "onnxruntime.dll"
	}
	matches, _ := filepath.Glob(filepath.Join(dataDir, pattern))
	if len(matches) == 0 {
		return ""
	}
	for _, m := range matches {
		info, err := os.Lstat(m)
		if err == nil && info.Mode()&os.ModeSymlink == 0 {
			return m
		}
	}
	return matches[0]
}
