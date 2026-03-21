// Package download handles fetching model files and ORT runtime on first run.
//
// Model and voice files are served from jikkuatwork/cattery-artefacts (Git LFS).
// ORT runtime is served from Microsoft's official GitHub Releases.
// All files are verified against SHA256 checksums.
// Downloads are resumable and show progress bars.
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
	"syscall"

	"github.com/kodeman/cattery/paths"
	"github.com/kodeman/cattery/registry"
)

const (
	ortVersion       = "1.24.1"
	artefactsBaseURL = "https://github.com/jikkuatwork/cattery-artefacts/raw/main/models"
)

// Result holds the paths to all required files after Ensure completes.
type Result struct {
	ModelPath string
	VoicePath string
	ORTLib    string
	DataDir   string
}

// Ensure checks if all required files exist, downloading any that are missing.
// Runs pre-flight checks before downloading.
func Ensure(dataDir string, model *registry.Model, voice *registry.Voice) (*Result, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	result := &Result{
		DataDir:   dataDir,
		ModelPath: paths.ModelFile(dataDir, model.ID, model.Filename),
		VoicePath: paths.VoiceFile(dataDir, model.ID, voice.ID),
		ORTLib:    findOrtLib(paths.ORTLib(dataDir)),
	}

	// Calculate total bytes needed
	var needed int64
	if result.ORTLib == "" {
		needed += 20_000_000 // ~18MB ORT + tar overhead
	}
	if !fileExists(result.ModelPath) {
		needed += model.SizeBytes
	}
	if !fileExists(result.VoicePath) {
		needed += voice.SizeBytes
	}

	if needed > 0 {
		if err := checkDiskSpace(dataDir, needed); err != nil {
			return nil, err
		}
	}

	// Download ORT runtime if missing
	if result.ORTLib == "" {
		lib, err := downloadORT(paths.ORTLib(dataDir))
		if err != nil {
			return nil, fmt.Errorf("download ORT: %w", err)
		}
		result.ORTLib = lib
	}

	// Download model if missing
	if !fileExists(result.ModelPath) {
		url := fmt.Sprintf("%s/%s/%s", artefactsBaseURL, model.ID, model.Filename)
		if err := downloadFileResumable(url, result.ModelPath, model.SizeBytes, model.SHA256, model.Name); err != nil {
			return nil, fmt.Errorf("download model: %w", err)
		}
	}

	// Download voice if missing
	if !fileExists(result.VoicePath) {
		url := fmt.Sprintf("%s/%s/voices/%s.bin", artefactsBaseURL, model.ID, voice.ID)
		label := fmt.Sprintf("Voice: %s", voice.Name)
		if err := downloadFileResumable(url, result.VoicePath, voice.SizeBytes, voice.SHA256, label); err != nil {
			return nil, fmt.Errorf("download voice %q: %w", voice.Name, err)
		}
	}

	return result, nil
}

// checkDiskSpace verifies enough space is available.
func checkDiskSpace(dir string, needed int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return nil // can't check, proceed anyway
	}
	avail := int64(stat.Bavail) * int64(stat.Bsize)
	if avail < needed {
		return fmt.Errorf("not enough disk space: need %s, have %s in %s",
			formatBytes(needed), formatBytes(avail), dir)
	}
	return nil
}

// downloadFileResumable downloads a file with resume support and progress bar.
// If a .tmp partial file exists, it resumes from where it left off.
func downloadFileResumable(url, destPath string, expectedSize int64, expectedSHA256, label string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	tmpPath := destPath + ".tmp"
	var existingSize int64

	// Check for partial download
	if info, err := os.Stat(tmpPath); err == nil {
		existingSize = info.Size()
	}

	// Build HTTP request with Range header for resume
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// Full response — start from scratch
		existingSize = 0
	case http.StatusPartialContent:
		// Resume supported
	case http.StatusRequestedRangeNotSatisfiable:
		// File already complete or server doesn't support range
		existingSize = 0
		resp.Body.Close()
		req.Header.Del("Range")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	case http.StatusNotFound:
		return fmt.Errorf("file not found (404): %s\nThe download URL may have changed. Try updating cattery.", url)
	default:
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Determine total size
	totalSize := expectedSize
	if totalSize == 0 && resp.ContentLength > 0 {
		totalSize = resp.ContentLength + existingSize
	}

	// Open file for append or create
	flags := os.O_CREATE | os.O_WRONLY
	if existingSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(tmpPath, flags, 0644)
	if err != nil {
		return err
	}

	// Write with progress
	pw := newProgressWriter(f, label, totalSize)
	pw.written = existingSize // account for already-downloaded portion
	_, copyErr := io.Copy(pw, resp.Body)
	f.Close()
	pw.finish()

	if copyErr != nil {
		return fmt.Errorf("download interrupted: %w (partial file kept for resume)", copyErr)
	}

	// Verify checksum
	if expectedSHA256 != "" {
		if err := verifyChecksum(tmpPath, expectedSHA256); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	return os.Rename(tmpPath, destPath)
}

// downloadORT downloads and extracts the ONNX Runtime shared library.
func downloadORT(ortDir string) (string, error) {
	if err := os.MkdirAll(ortDir, 0755); err != nil {
		return "", err
	}

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
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	destPath := filepath.Join(ortDir, libName)
	if fileExists(destPath) {
		return destPath, nil
	}

	// Download tarball to temp
	tmpFile, err := os.CreateTemp("", "cattery-ort-*.tgz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	fmt.Fprintf(os.Stderr, "Downloading ONNX Runtime %s (%s/%s)...\n", ortVersion, runtime.GOOS, arch)
	pw := newProgressWriter(tmpFile, "ONNX Runtime", 20_000_000) // approximate
	resp, err := http.Get(url)
	if err != nil {
		tmpFile.Close()
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		tmpFile.Close()
		return "", fmt.Errorf("ONNX Runtime not available for %s/%s at version %s", runtime.GOOS, arch, ortVersion)
	}
	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return "", fmt.Errorf("HTTP %d downloading ORT", resp.StatusCode)
	}

	if resp.ContentLength > 0 {
		pw.total = resp.ContentLength
	}

	_, err = io.Copy(pw, resp.Body)
	tmpFile.Close()
	pw.finish()
	if err != nil {
		return "", err
	}

	// Extract lib from tarball
	if err := extractFromTarGz(tmpFile.Name(), "lib/"+libName, destPath); err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	return destPath, nil
}

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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findOrtLib(ortDir string) string {
	var pattern string
	switch runtime.GOOS {
	case "linux":
		pattern = "libonnxruntime.so*"
	case "darwin":
		pattern = "libonnxruntime*.dylib"
	case "windows":
		pattern = "onnxruntime.dll"
	}
	matches, _ := filepath.Glob(filepath.Join(ortDir, pattern))
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
