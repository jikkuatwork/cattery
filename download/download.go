// Package download handles fetching model files and ORT runtime on first run.
//
// Local TTS artefacts default to jikkuatwork/cattery-artefacts (Git LFS).
// STT artefacts may point at explicit auth-free URLs such as Hugging Face.
// ORT runtime is served from Microsoft's official GitHub Releases.
// Files are verified against SHA256 checksums where recorded.
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
	defaultORTVersion = "1.24.1"
	defaultORTBytes   = 20_000_000
	artefactsBaseURL  = "https://github.com/jikkuatwork/cattery-artefacts/raw/main/models"
)

// Result holds local paths for the ensured artefacts.
type Result struct {
	ORTLib string
	Files  map[string]string
}

type pendingDownload struct {
	label  string
	url    string
	dest   string
	size   int64
	sha256 string
}

// Ensure checks if a model's required files exist, downloading what is missing.
// Optional voices are downloaded too. Local models also ensure the shared ORT
// runtime is present.
func Ensure(dataDir string, model *registry.Model, voices ...*registry.Voice) (*Result, error) {
	if model == nil {
		return nil, fmt.Errorf("missing registry model")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	result := &Result{
		ORTLib: findOrtLib(paths.ORTLib(dataDir)),
		Files:  make(map[string]string),
	}

	pending := pendingModelDownloads(result, dataDir, model, voices)
	needORT := needsORT(model) && result.ORTLib == ""

	if !needORT && len(pending) == 0 {
		return result, nil
	}

	var (
		labels []string
		needed int64
	)
	if needORT {
		labels = append(labels, "Runtime")
		needed += defaultORTBytes
	}
	for _, item := range pending {
		labels = append(labels, item.label)
		needed += item.size
	}
	if err := checkDiskSpace(dataDir, needed); err != nil {
		return nil, err
	}

	style := &barStyle{labelWidth: maxLen(labels)}

	if needORT {
		lib, err := downloadORT(paths.ORTLib(dataDir), ortVersionFor(model), style)
		if err != nil {
			return nil, fmt.Errorf("download ORT: %w", err)
		}
		result.ORTLib = lib
	}

	for _, item := range pending {
		if err := downloadWithBar(style, item.label, item.url, item.dest, item.size, item.sha256); err != nil {
			return nil, fmt.Errorf("download %s: %w", item.label, err)
		}
	}

	return result, nil
}

// EnsureAll downloads a model and all of its voices.
func EnsureAll(dataDir string, model *registry.Model) error {
	_, err := Ensure(dataDir, model, model.VoiceRefs()...)
	return err
}

func pendingModelDownloads(result *Result, dataDir string, model *registry.Model, voices []*registry.Voice) []pendingDownload {
	var pending []pendingDownload

	for _, file := range model.Files {
		dest := paths.ArtefactFile(dataDir, model.ID, file.Filename)
		result.Files[file.Filename] = dest
		if fileExists(dest) {
			continue
		}
		pending = append(pending, pendingDownload{
			label:  artefactLabel(model, file),
			url:    artefactURL(model, file),
			dest:   dest,
			size:   file.SizeBytes,
			sha256: file.SHA256,
		})
	}

	seen := make(map[string]bool)
	for _, voice := range voices {
		if voice == nil {
			continue
		}
		key := voice.File.Filename
		if seen[key] {
			continue
		}
		seen[key] = true

		dest := paths.ArtefactFile(dataDir, model.ID, voice.File.Filename)
		result.Files[key] = dest
		if fileExists(dest) {
			continue
		}
		pending = append(pending, pendingDownload{
			label:  voice.Name,
			url:    artefactURL(model, voice.File),
			dest:   dest,
			size:   voice.File.SizeBytes,
			sha256: voice.File.SHA256,
		})
	}

	return pending
}

func needsORT(model *registry.Model) bool {
	if model == nil {
		return false
	}
	if model.Kind == registry.KindRuntime {
		return true
	}
	return model.Location == registry.Local
}

func ortVersionFor(model *registry.Model) string {
	if model != nil && model.Kind == registry.KindRuntime {
		return model.MetaString("version", defaultORTVersion)
	}
	runtimeModel := registry.Default(registry.KindRuntime)
	if runtimeModel == nil {
		return defaultORTVersion
	}
	return runtimeModel.MetaString("version", defaultORTVersion)
}

func artefactLabel(model *registry.Model, file registry.Artefact) string {
	name := filepath.Base(file.Filename)
	switch {
	case model != nil && model.Kind == registry.KindTTS && len(model.Files) == 1:
		return model.Name
	case strings.Contains(name, "encoder"):
		return "Encoder"
	case strings.Contains(name, "decoder"):
		return "Decoder"
	case name == "tokenizer.json":
		return "Tokenizer"
	default:
		return name
	}
}

func artefactURL(model *registry.Model, file registry.Artefact) string {
	if file.URL != "" {
		return file.URL
	}
	return fmt.Sprintf("%s/%s/%s", artefactsBaseURL, model.ID, filepath.ToSlash(file.Filename))
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

// downloadORT downloads and extracts the ONNX Runtime shared library.
func downloadORT(ortDir, version string, style *barStyle) (string, error) {
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
			version, arch, version)
		libName = fmt.Sprintf("libonnxruntime.so.%s", version)
	case "darwin":
		url = fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/onnxruntime-osx-%s-%s.tgz",
			version, arch, version)
		libName = fmt.Sprintf("libonnxruntime.%s.dylib", version)
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	destPath := filepath.Join(ortDir, libName)
	if fileExists(destPath) {
		return destPath, nil
	}

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
		return "", fmt.Errorf("ONNX Runtime not available for %s/%s at version %s", runtime.GOOS, arch, version)
	}
	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return "", fmt.Errorf("HTTP %d downloading ORT", resp.StatusCode)
	}

	total := int64(defaultORTBytes)
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
