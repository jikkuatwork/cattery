package download

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

const mirrorIndexURL = "https://github.com/jikkuatwork/cattery-artefacts/raw/main/mirror.json"

type MirrorIndex struct {
	Version   int                    `json:"version"`
	Artefacts map[string]MirrorEntry `json:"artefacts"`
}

type MirrorEntry struct {
	Size    int64          `json:"size"`
	SHA256  string         `json:"sha256"`
	Mirrors []MirrorSource `json:"mirrors"`
}

type MirrorSource struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

func fetchMirrorIndex() (*MirrorIndex, error) {
	resp, err := http.Get(mirrorIndexURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching mirror index", resp.StatusCode)
	}

	return parseMirrorIndex(resp.Body)
}

func parseMirrorIndex(r io.Reader) (*MirrorIndex, error) {
	var idx MirrorIndex
	if err := json.NewDecoder(r).Decode(&idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func (idx *MirrorIndex) Lookup(modelID, filename string) *MirrorEntry {
	if idx == nil {
		return nil
	}
	key := path.Join("models", modelID, strings.ReplaceAll(filename, "\\", "/"))
	entry, ok := idx.Artefacts[key]
	if !ok {
		return nil
	}
	return &entry
}
