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
