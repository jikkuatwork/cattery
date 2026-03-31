// Package phonemize converts text to IPA phonemes for Kokoro TTS.
package phonemize

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// punctRe matches punctuation that Kokoro treats as tokens.
var punctRe = regexp.MustCompile(`([.,!?;:])`)

// BinPath holds the resolved espeak-ng binary path.
// Set by callers before first use, or falls back to system PATH.
var BinPath string

// DataPath holds the resolved espeak-ng data directory.
// When non-empty, ESPEAK_DATA_PATH is set on every invocation.
var DataPath string

// EspeakPhonemizer uses espeak-ng via os/exec to produce IPA phonemes.
type EspeakPhonemizer struct {
	Voice string // e.g. "en-us", "en-gb"
}

// Phonemize converts text to a phoneme string with preserved punctuation,
// suitable for Kokoro tokenization. Punctuation characters (.,!?;:) are
// kept inline between phoneme groups.
func (e *EspeakPhonemizer) Phonemize(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	// Split on punctuation, keeping delimiters
	// "Hello, world." -> ["Hello", ",", " world", ".", ""]
	parts := punctRe.Split(text, -1)
	puncts := punctRe.FindAllString(text, -1)

	var result strings.Builder
	pi := 0
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			phonemes, err := e.espeakIPA(part)
			if err != nil {
				return "", err
			}
			if result.Len() > 0 && phonemes != "" {
				result.WriteByte(' ')
			}
			result.WriteString(phonemes)
		}
		// Append the punctuation that followed this part
		if pi < len(puncts) && i < len(parts)-1 {
			result.WriteString(puncts[pi])
			pi++
		}
	}

	return result.String(), nil
}

// espeakIPA calls espeak-ng for a single text fragment (no punctuation).
func (e *EspeakPhonemizer) espeakIPA(text string) (string, error) {
	voice := e.Voice
	if voice == "" {
		voice = "en-us"
	}

	bin := BinPath
	if bin == "" {
		bin = "espeak-ng"
	}

	cmd := exec.Command(bin, "-q", "--ipa=3", "--sep=", "-v", voice, text)
	if DataPath != "" {
		cmd.Env = append(os.Environ(), "ESPEAK_DATA_PATH="+DataPath)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("espeak-ng: %w", err)
	}

	// espeak-ng outputs one line per clause; join with space
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}

	return strings.Join(parts, " "), nil
}

// Available returns true if espeak-ng is available.
func Available() bool {
	if BinPath != "" {
		_, err := os.Stat(BinPath)
		return err == nil
	}
	_, err := exec.LookPath("espeak-ng")
	return err == nil
}
