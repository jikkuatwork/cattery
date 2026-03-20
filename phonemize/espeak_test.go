package phonemize

import (
	"strings"
	"testing"
)

func TestEspeakAvailable(t *testing.T) {
	if !Available() {
		t.Skip("espeak-ng not installed")
	}
}

func TestPhonemize(t *testing.T) {
	if !Available() {
		t.Skip("espeak-ng not installed")
	}

	p := &EspeakPhonemizer{Voice: "en-us"}

	tests := []struct {
		input     string
		wantPunct []string // punctuation that should appear in output
	}{
		{"Hello", nil},
		{"Hello, world.", []string{",", "."}},
		{"How are you?", []string{"?"}},
		{"Wait! What?", []string{"!", "?"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := p.Phonemize(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got == "" {
				t.Fatal("empty output")
			}
			t.Logf("%q -> %q", tt.input, got)

			for _, p := range tt.wantPunct {
				if !strings.Contains(got, p) {
					t.Errorf("missing punctuation %q in output %q", p, got)
				}
			}
		})
	}
}
