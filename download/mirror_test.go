package download

import (
	"strings"
	"testing"
)

func TestParseMirrorIndexLookup(t *testing.T) {
	idx, err := parseMirrorIndex(strings.NewReader(`{
		"version": 1,
		"artefacts": {
			"models/kokoro-82m-v1.0/model_quantized.onnx": {
				"size": 92361116,
				"sha256": "abc123",
				"mirrors": [
					{ "url": "https://example.com/model_quantized.onnx", "label": "github" }
				]
			},
			"models/kokoro-82m-v1.0/voices/af_heart.bin": {
				"size": 522240,
				"sha256": "def456",
				"mirrors": [
					{ "url": "https://example.com/voices/af_heart.bin", "label": "backup" }
				]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("parseMirrorIndex: %v", err)
	}

	entry := idx.Lookup("kokoro-82m-v1.0", "model_quantized.onnx")
	if entry == nil {
		t.Fatal("Lookup returned nil")
	}
	if entry.Size != 92361116 {
		t.Fatalf("unexpected size: %d", entry.Size)
	}
	if entry.SHA256 != "abc123" {
		t.Fatalf("unexpected sha256: %q", entry.SHA256)
	}
	if len(entry.Mirrors) != 1 || entry.Mirrors[0].URL != "https://example.com/model_quantized.onnx" {
		t.Fatalf("unexpected mirrors: %#v", entry.Mirrors)
	}

	voice := idx.Lookup("kokoro-82m-v1.0", "voices\\af_heart.bin")
	if voice == nil {
		t.Fatal("Lookup did not normalize Windows-style path separators")
	}
	if voice.SHA256 != "def456" {
		t.Fatalf("unexpected voice sha256: %q", voice.SHA256)
	}

	if got := idx.Lookup("kokoro-82m-v1.0", "voices/missing.bin"); got != nil {
		t.Fatalf("unexpected lookup hit: %#v", got)
	}
}

func TestDownloadSourcesDefaultReorder(t *testing.T) {
	mirrors := []MirrorSource{
		{URL: "https://github.com/model.onnx", Label: "github"},
		{URL: "https://s3.example.com/model.onnx", Label: "s3", Default: true},
		{URL: "https://backup.example.com/model.onnx", Label: "backup"},
	}

	got := downloadSources("", mirrors)
	if len(got) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(got))
	}
	if got[0].Label != "s3" {
		t.Fatalf("expected default mirror first, got %q", got[0].Label)
	}
	if got[1].Label != "github" || got[2].Label != "backup" {
		t.Fatalf("unexpected order: %q, %q", got[1].Label, got[2].Label)
	}
}

func TestDownloadSourcesNoDefault(t *testing.T) {
	mirrors := []MirrorSource{
		{URL: "https://github.com/model.onnx", Label: "github"},
		{URL: "https://s3.example.com/model.onnx", Label: "s3"},
	}

	got := downloadSources("", mirrors)
	if got[0].Label != "github" {
		t.Fatalf("expected original order preserved, got %q first", got[0].Label)
	}
}
