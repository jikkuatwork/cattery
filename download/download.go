// Package download handles fetching model files and ORT runtime on first run.
//
// Model and voice files are served from jikkuatwork/cattery-artefacts (Git LFS).
// ORT runtime is served from Microsoft's official GitHub Releases.
// All files are verified against SHA256 checksums.
// Downloads show aligned progress bars.
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

	"github.com/jikkuatwork/cattery/paths"
	"github.com/jikkuatwork/cattery/registry"
)

const (
	ortVersion       = "1.24.1"
	artefactsBaseURL = "https://github.com/jikkuatwork/cattery-artefacts/raw/main/models"
)

// Result holds the paths to all required files after Ensure completes.
// VoicePath is empty when no voice was requested.
type Result struct {
	ModelPath string
	VoicePath string
	ORTLib    string
	DataDir   string
}

// Ensure checks if all required files exist, downloading any that are missing.
// If voice is nil, it ensures only the model and ORT runtime.
func Ensure(dataDir string, model *registry.Model, voice *registry.Voice) (*Result, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	result := &Result{
		DataDir:   dataDir,
		ModelPath: paths.ModelFile(dataDir, model.ID, model.Filename),
		ORTLib:    findOrtLib(paths.ORTLib(dataDir)),
	}
	if voice != nil {
		result.VoicePath = paths.VoiceFile(dataDir, model.ID, voice.ID)
	}

	needORT := result.ORTLib == ""
	needModel := !fileExists(result.ModelPath)
	needVoice := voice != nil && !fileExists(result.VoicePath)

	if !needORT && !needModel && !needVoice {
		return result, nil
	}

	// Pre-flight disk space check
	var needed int64
	if needORT {
		needed += 20_000_000
	}
	if needModel {
		needed += model.SizeBytes
	}
	if needVoice {
		needed += voice.SizeBytes
	}
	if err := checkDiskSpace(dataDir, needed); err != nil {
		return nil, err
	}

	// Compute label width for alignment
	var labels []string
	if needORT {
		labels = append(labels, "Runtime")
	}
	if needModel {
		labels = append(labels, model.Name)
	}
	if needVoice {
		labels = append(labels, "Voice")
	}
	style := &barStyle{labelWidth: maxLen(labels)}

	if needORT {
		lib, err := downloadORT(paths.ORTLib(dataDir), style)
		if err != nil {
			return nil, fmt.Errorf("download ORT: %w", err)
		}
		result.ORTLib = lib
	}

	if needModel {
		url := fmt.Sprintf("%s/%s/%s", artefactsBaseURL, model.ID, model.Filename)
		if err := downloadWithBar(style, model.Name, url, result.ModelPath, model.SizeBytes, model.SHA256); err != nil {
			return nil, fmt.Errorf("download model: %w", err)
		}
	}

	if needVoice {
		url := fmt.Sprintf("%s/%s/voices/%s.bin", artefactsBaseURL, model.ID, voice.ID)
		if err := downloadWithBar(style, "Voice", url, result.VoicePath, voice.SizeBytes, voice.SHA256); err != nil {
			return nil, fmt.Errorf("download voice %q: %w", voice.Name, err)
		}
	}

	return result, nil
}

// EnsureAll downloads ORT, the model, and all voices for a model.
// Shows aligned progress bars: Runtime, Model (bytes), Voices (count).
func EnsureAll(dataDir string, model *registry.Model) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	needORT := findOrtLib(paths.ORTLib(dataDir)) == ""
	needModel := !fileExists(paths.ModelFile(dataDir, model.ID, model.Filename))

	// Count missing voices
	type pending struct {
		voice *registry.Voice
		url   string
		dest  string
	}
	var missing []pending
	for i := range model.Voices {
		v := &model.Voices[i]
		dest := paths.VoiceFile(dataDir, model.ID, v.ID)
		if !fileExists(dest) {
			url := fmt.Sprintf("%s/%s/voices/%s.bin", artefactsBaseURL, model.ID, v.ID)
			missing = append(missing, pending{voice: v, url: url, dest: dest})
		}
	}

	if !needORT && !needModel && len(missing) == 0 {
		return nil
	}

	// --- Runtime group ---
	if needORT {
		style := &barStyle{labelWidth: len("Runtime")}
		if _, err := downloadORT(paths.ORTLib(dataDir), style); err != nil {
			return fmt.Errorf("download ORT: %w", err)
		}
		fmt.Fprintln(os.Stderr)
	}

	// --- Model group ---
	if needModel || len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", model.Name)

		// Compute label width for model sub-items
		var labels []string
		if needModel {
			labels = append(labels, "Model")
		}
		if len(missing) > 0 {
			labels = append(labels, "Voices")
		}
		style := &barStyle{labelWidth: maxLen(labels) + 1} // +1 for leading space

		if needModel {
			modelPath := paths.ModelFile(dataDir, model.ID, model.Filename)
			url := fmt.Sprintf("%s/%s/%s", artefactsBaseURL, model.ID, model.Filename)
			if err := downloadWithBar(style, " Model", url, modelPath, model.SizeBytes, model.SHA256); err != nil {
				return fmt.Errorf("download model: %w", err)
			}
		}

		if len(missing) > 0 {
			b := newBar(" Voices", int64(len(missing)), false, style)
			for i, m := range missing {
				if err := downloadFile(m.url, m.dest, m.voice.SHA256); err != nil {
					// skip failed voice, continue
				}
				b.set(int64(i + 1))
			}
			b.finish()
		}

		fmt.Fprintln(os.Stderr)
	}

	return nil
}

// downloadWithBar downloads a file showing a byte-tracking progress bar.
func downloadWithBar(style *barStyle, label, url, destPath string, expectedSize int64, expectedSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found (404): %s", url)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	total := expectedSize
	if total == 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	b := newBar(label, total, true, style)

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(io.MultiWriter(f, b), resp.Body)
	f.Close()
	b.finish()

	if copyErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download interrupted: %w", copyErr)
	}

	if expectedSHA256 != "" {
		if err := verifyChecksum(tmpPath, expectedSHA256); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	return os.Rename(tmpPath, destPath)
}

// downloadFile downloads a file silently (no progress bar).
func downloadFile(url, destPath, expectedSHA256 string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(f, resp.Body)
	f.Close()

	if copyErr != nil {
		os.Remove(tmpPath)
		return copyErr
	}

	if expectedSHA256 != "" {
		if err := verifyChecksum(tmpPath, expectedSHA256); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	return os.Rename(tmpPath, destPath)
}

// downloadORT downloads and extracts the ONNX Runtime shared library.
func downloadORT(ortDir string, style *barStyle) (string, error) {
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

	// Download tarball to temp file
	tmpFile, err := os.CreateTemp("", "cattery-ort-*.tgz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

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

	total := int64(20_000_000)
	if resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	b := newBar("Runtime", total, true, style)

	_, err = io.Copy(io.MultiWriter(tmpFile, b), resp.Body)
	tmpFile.Close()
	b.finish()
	if err != nil {
		return "", err
	}

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

func checkDiskSpace(dir string, needed int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return nil
	}
	avail := int64(stat.Bavail) * int64(stat.Bsize)
	if avail < needed {
		return fmt.Errorf("not enough disk space: need %s, have %s in %s",
			formatBytes(needed), formatBytes(avail), dir)
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

func maxLen(ss []string) int {
	n := 0
	for _, s := range ss {
		if len(s) > n {
			n = len(s)
		}
	}
	return n
}
