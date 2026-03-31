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
	"sync"
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
	label   string
	url     string
	mirrors []MirrorSource
	dest    string
	size    int64
	sha256  string
	isVoice bool
}

var (
	mirrorIndexOnce sync.Once
	mirrorIndex     *MirrorIndex
)

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

	idx := getMirrorIndex()

	result := &Result{
		ORTLib: findOrtLib(paths.ORTLib(dataDir)),
		Files:  make(map[string]string),
	}

	pending := pendingModelDownloads(result, dataDir, model, voices, idx)
	needORT := needsORT(model) && result.ORTLib == ""

	if !needORT && len(pending) == 0 {
		return result, nil
	}

	var needed int64
	if needORT {
		needed += defaultORTBytes
	}
	for _, item := range pending {
		needed += item.size
	}
	if err := checkDiskSpace(dataDir, needed); err != nil {
		return nil, err
	}

	// Split pending into model files and voice files.
	var modelPending, voicePending []pendingDownload
	for _, item := range pending {
		if item.isVoice {
			voicePending = append(voicePending, item)
		} else {
			modelPending = append(modelPending, item)
		}
	}

	// Build label list for alignment (voices get one "Voices" label).
	var labels []string
	if needORT {
		labels = append(labels, "Runtime")
	}
	for _, item := range modelPending {
		labels = append(labels, item.label)
	}
	if len(voicePending) > 0 {
		labels = append(labels, "Voices")
	}

	style := &barStyle{labelWidth: maxLen(labels)}

	if needORT {
		lib, err := downloadORT(paths.ORTLib(dataDir), ortVersionFor(model), style)
		if err != nil {
			return nil, fmt.Errorf("download ORT: %w", err)
		}
		result.ORTLib = lib
	}

	for _, item := range modelPending {
		if err := downloadWithBar(style, item.label, item.url, item.mirrors, item.dest, item.size, item.sha256); err != nil {
			return nil, fmt.Errorf("download %s: %w", item.label, err)
		}
	}

	if len(voicePending) > 0 {
		if err := downloadVoicesBatch(style, voicePending); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// EnsureAll downloads a model and all of its voices.
func EnsureAll(dataDir string, model *registry.Model) error {
	_, err := Ensure(dataDir, model, model.VoiceRefs()...)
	return err
}

func pendingModelDownloads(result *Result, dataDir string, model *registry.Model, voices []*registry.Voice, idx *MirrorIndex) []pendingDownload {
	var pending []pendingDownload

	for _, file := range model.Files {
		dest := paths.ArtefactFile(dataDir, model.ID, file.Filename)
		result.Files[file.Filename] = dest
		if fileExists(dest) {
			continue
		}
		entry := mirrorEntry(idx, model, file)
		sha256 := file.SHA256
		if sha256 == "" && entry != nil {
			sha256 = entry.SHA256
		}
		pending = append(pending, pendingDownload{
			label:   artefactLabel(model, file),
			url:     artefactURL(model, file, idx),
			mirrors: mirrorSources(entry),
			dest:    dest,
			size:    file.SizeBytes,
			sha256:  sha256,
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
		entry := mirrorEntry(idx, model, voice.File)
		sha256 := voice.File.SHA256
		if sha256 == "" && entry != nil {
			sha256 = entry.SHA256
		}
		pending = append(pending, pendingDownload{
			label:   voice.Name,
			url:     artefactURL(model, voice.File, idx),
			mirrors: mirrorSources(entry),
			dest:    dest,
			size:    voice.File.SizeBytes,
			sha256:  sha256,
			isVoice: true,
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

func artefactURL(model *registry.Model, file registry.Artefact, idx *MirrorIndex) string {
	if file.URL != "" {
		return file.URL
	}
	if entry := mirrorEntry(idx, model, file); entry != nil && len(entry.Mirrors) > 0 {
		return entry.Mirrors[0].URL
	}
	return fmt.Sprintf("%s/%s/%s", artefactsBaseURL, model.ID, filepath.ToSlash(file.Filename))
}

// downloadWithBar downloads a file showing a byte-tracking progress bar.
func downloadWithBar(style *barStyle, label, url string, mirrors []MirrorSource, destPath string, expectedSize int64, expectedSHA256 string) error {
	return downloadArtefact(style, label, url, mirrors, destPath, expectedSize, expectedSHA256, true)
}

func downloadArtefact(style *barStyle, label, url string, mirrors []MirrorSource, destPath string, expectedSize int64, expectedSHA256 string, withBar bool) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	var lastErr error
	for _, source := range downloadSources(url, mirrors) {
		if err := downloadFromSource(style, label, source, destPath, expectedSize, expectedSHA256, withBar); err != nil {
			lastErr = err
			continue
		}
		if source.Label != "" {
			fmt.Fprintf(os.Stderr, "Using mirror %s for %s\n", source.Label, label)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no download source for %s", label)
}

// downloadVoicesBatch downloads all voices showing a single "Voices  3 / 27" counter bar.
func downloadVoicesBatch(style *barStyle, items []pendingDownload) error {
	total := int64(len(items))
	b := newBar("Voices", total, false, style)
	for i, item := range items {
		if err := downloadQuiet(item.label, item.url, item.mirrors, item.dest, item.sha256); err != nil {
			b.finish()
			return fmt.Errorf("download voice %s: %w", item.label, err)
		}
		b.set(int64(i + 1))
	}
	b.finish()
	return nil
}

// downloadQuiet downloads a file without any progress display.
func downloadQuiet(label, url string, mirrors []MirrorSource, destPath, expectedSHA256 string) error {
	return downloadArtefact(nil, label, url, mirrors, destPath, 0, expectedSHA256, false)
}

func getMirrorIndex() *MirrorIndex {
	mirrorIndexOnce.Do(func() {
		idx, err := fetchMirrorIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mirror index unavailable: %v\n", err)
			return
		}
		mirrorIndex = idx
	})
	return mirrorIndex
}

func mirrorEntry(idx *MirrorIndex, model *registry.Model, file registry.Artefact) *MirrorEntry {
	if idx == nil || model == nil || file.URL != "" {
		return nil
	}
	return idx.Lookup(model.ID, file.Filename)
}

func mirrorSources(entry *MirrorEntry) []MirrorSource {
	if entry == nil || len(entry.Mirrors) == 0 {
		return nil
	}
	return entry.Mirrors
}

func downloadSources(url string, mirrors []MirrorSource) []MirrorSource {
	if len(mirrors) > 0 {
		// If any mirror is marked default, put it first.
		for i, m := range mirrors {
			if m.Default && i != 0 {
				reordered := make([]MirrorSource, 0, len(mirrors))
				reordered = append(reordered, m)
				reordered = append(reordered, mirrors[:i]...)
				reordered = append(reordered, mirrors[i+1:]...)
				return reordered
			}
		}
		return mirrors
	}
	if url == "" {
		return nil
	}
	return []MirrorSource{{URL: url}}
}

func downloadFromSource(style *barStyle, label string, source MirrorSource, destPath string, expectedSize int64, expectedSHA256 string, withBar bool) error {
	resp, err := http.Get(source.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found (404): %s", source.URL)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, source.URL)
	}

	total := expectedSize
	if total == 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	var writer io.Writer
	var b *bar
	if withBar {
		b = newBar(label, total, true, style)
		writer = b
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		if b != nil {
			b.finish()
		}
		return err
	}

	if writer == nil {
		writer = f
	} else {
		writer = io.MultiWriter(f, writer)
	}

	_, copyErr := io.Copy(writer, resp.Body)
	f.Close()
	if b != nil {
		b.finish()
	}

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

	var (
		osName  string
		arch    string
		libName string
	)

	switch runtime.GOOS {
	case "linux":
		osName = "linux"
		arch = runtime.GOARCH
		if arch == "amd64" {
			arch = "x64"
		} else if arch == "arm64" {
			arch = "aarch64"
		}
		libName = fmt.Sprintf("libonnxruntime.so.%s", version)
	case "darwin":
		osName = "osx"
		arch = "arm64"
		// macOS ships arm64 only since ORT 1.20+
		libName = fmt.Sprintf("libonnxruntime.%s.dylib", version)
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	destPath := filepath.Join(ortDir, libName)
	if fileExists(destPath) {
		return destPath, nil
	}

	tgzName := fmt.Sprintf("onnxruntime-%s-%s-%s.tgz", osName, arch, version)
	mirrorKey := fmt.Sprintf("ort/v%s/%s", version, tgzName)
	microsoftURL := fmt.Sprintf(
		"https://github.com/microsoft/onnxruntime/releases/download/v%s/%s",
		version, tgzName,
	)

	tmpPath, err := reserveTempPath("cattery-ort-*.tgz")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath)

	// Resolve download source: mirror first, Microsoft fallback.
	var (
		url     string
		mirrors []MirrorSource
		size    int64
		sha256  string
	)
	if idx := getMirrorIndex(); idx != nil {
		if entry := idx.LookupRaw(mirrorKey); entry != nil {
			url = firstMirrorURL(entry)
			mirrors = mirrorSources(entry)
			size = entry.Size
			sha256 = entry.SHA256
		}
	}
	if url == "" {
		url = microsoftURL
		size = int64(defaultORTBytes)
	}

	if err := downloadWithBar(style, "Runtime", url, mirrors, tmpPath, size, sha256); err != nil {
		return "", err
	}

	if err := extractFromTarGz(tmpPath, "lib/"+libName, destPath); err != nil {
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
