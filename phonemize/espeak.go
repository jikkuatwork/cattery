// Package phonemize converts text to IPA phonemes for Kokoro TTS.
package phonemize

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// punctRe matches punctuation that Kokoro treats as tokens.
var punctRe = regexp.MustCompile(`([.,!?;:])`)

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

	cmd := exec.Command("espeak-ng", "-q", "--ipa=3", "--sep=", "-v", voice, text)
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

// Available returns true if espeak-ng is found on PATH.
func Available() bool {
	_, err := exec.LookPath("espeak-ng")
	return err == nil
}
