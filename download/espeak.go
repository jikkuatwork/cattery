package download

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jikkuatwork/cattery/paths"
)

const espeakVersion = "1.51"

// EnsureEspeak downloads and installs bundled espeak-ng binaries and data.
func EnsureEspeak(dataDir string) error {
	espeakDir := paths.EspeakDir(dataDir)
	versionFile := filepath.Join(espeakDir, ".version")

	if data, err := os.ReadFile(versionFile); err == nil {
		if strings.TrimSpace(string(data)) == espeakVersion {
			return nil
		}
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	idx := getMirrorIndex()
	if idx == nil {
		return fmt.Errorf("mirror index unavailable")
	}

	platformKey := fmt.Sprintf("espeak-ng-v%s/%s_%s.tar.gz", espeakVersion, runtime.GOOS, runtime.GOARCH)
	binEntry := idx.LookupRaw(platformKey)
	if binEntry == nil {
		return fmt.Errorf("espeak-ng is not available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	dataKey := fmt.Sprintf("espeak-ng-v%s/data.tar.gz", espeakVersion)
	dataEntry := idx.LookupRaw(dataKey)
	if dataEntry == nil {
		return fmt.Errorf("espeak-ng data archive %q not found in mirror index", dataKey)
	}

	if err := checkDiskSpace(dataDir, binEntry.Size+dataEntry.Size); err != nil {
		return err
	}

	style := &barStyle{labelWidth: maxLen([]string{"espeak-ng", "espeak-data"})}

	binTar, err := reserveTempPath("cattery-espeak-bin-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(binTar)

	dataTar, err := reserveTempPath("cattery-espeak-data-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(dataTar)

	if err := downloadWithBar(style, "espeak-ng", firstMirrorURL(binEntry), mirrorSources(binEntry), binTar, binEntry.Size, binEntry.SHA256); err != nil {
		return fmt.Errorf("download espeak-ng binary: %w", err)
	}
	if err := downloadWithBar(style, "espeak-data", firstMirrorURL(dataEntry), mirrorSources(dataEntry), dataTar, dataEntry.Size, dataEntry.SHA256); err != nil {
		return fmt.Errorf("download espeak-ng data: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(espeakDir), 0755); err != nil {
		return err
	}

	stageDir, err := os.MkdirTemp(filepath.Dir(espeakDir), "espeak-ng-*")
	if err != nil {
		return err
	}
	defer func() {
		if stageDir != "" {
			_ = os.RemoveAll(stageDir)
		}
	}()

	if err := extractTarGzAll(binTar, stageDir); err != nil {
		return fmt.Errorf("extract espeak-ng binary: %w", err)
	}
	if err := extractTarGzAll(dataTar, stageDir); err != nil {
		return fmt.Errorf("extract espeak-ng data: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, ".version"), []byte(espeakVersion+"\n"), 0644); err != nil {
		return err
	}

	if err := os.RemoveAll(espeakDir); err != nil {
		return err
	}
	if err := os.Rename(stageDir, espeakDir); err != nil {
		return err
	}
	stageDir = ""

	return nil
}

func reserveTempPath(pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return name, nil
}

func firstMirrorURL(entry *MirrorEntry) string {
	if entry == nil || len(entry.Mirrors) == 0 {
		return ""
	}
	return entry.Mirrors[0].URL
}

func extractTarGzAll(tarPath, destDir string) error {
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
			return nil
		}
		if err != nil {
			return err
		}

		target, err := safeTarPath(destDir, header.Name)
		if err != nil {
			return err
		}
		if target == "" {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			if err := os.Chmod(target, dirMode(header)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode(header))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q (%c)", header.Name, header.Typeflag)
		}
	}
}

func safeTarPath(destDir, name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == "" {
		return "", nil
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}

	target := filepath.Join(destDir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return target, nil
}

func dirMode(header *tar.Header) fs.FileMode {
	mode := fs.FileMode(header.Mode) & fs.ModePerm
	if mode == 0 {
		return 0755
	}
	return mode
}

func fileMode(header *tar.Header) fs.FileMode {
	mode := fs.FileMode(header.Mode) & fs.ModePerm
	if mode&0111 != 0 {
		return 0755
	}
	if mode == 0 {
		return 0644
	}
	return mode
}
