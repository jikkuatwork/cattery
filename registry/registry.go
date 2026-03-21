// Package registry defines available models, voices, runtimes, and metadata.
package registry

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Kind describes a registered artefact family.
type Kind string

const (
	KindTTS       Kind = "tts"
	KindSTT       Kind = "stt"
	KindVoice     Kind = "voice"
	KindTokenizer Kind = "tokenizer"
	KindRuntime   Kind = "runtime"
)

// Location describes where a model runs.
type Location string

const (
	Local  Location = "local"
	Remote Location = "remote"
)

// Artefact is a downloadable file with an optional checksum and explicit URL.
type Artefact struct {
	Filename  string
	SizeBytes int64
	SHA256    string
	URL       string
}

// Model describes a model or runtime entry available to cattery.
type Model struct {
	Index       int
	ID          string
	Kind        Kind
	Location    Location
	Name        string
	Description string
	Lang        []string
	Files       []Artefact
	Voices      []Voice
	Meta        map[string]string
}

// Voice describes a voice available for a TTS model.
type Voice struct {
	ID          string
	Name        string
	Gender      string
	Accent      string
	Description string
	File        Artefact
}

const (
	kokoroModelID        = "kokoro-82m-v1.0"
	moonshineModelID     = "moonshine-tiny-v1.0"
	ortModelID           = "ort-1.24.1"
	defaultVoiceSize     = 522_240
	moonshineHFBaseURL   = "https://huggingface.co/onnx-community/moonshine-tiny-ONNX/resolve/main"
	moonshineONNXBaseURL = moonshineHFBaseURL + "/onnx"
)

var models = []*Model{
	{
		Index:       1,
		ID:          kokoroModelID,
		Kind:        KindTTS,
		Location:    Local,
		Name:        "Kokoro",
		Description: "High-quality int8 quantized TTS model",
		Lang:        []string{"en"},
		Files: []Artefact{
			{
				Filename:  "model_quantized.onnx",
				SizeBytes: 92_361_116,
				SHA256:    "fbae9257e1e05ffc727e951ef9b9c98418e6d79f1c9b6b13bd59f5c9028a1478",
			},
		},
		Voices: []Voice{
			// American female
			voice("af_heart", "Heart", "female", "American", "Warm, expressive", "d583ccff3cdca2f7fae535cb998ac07e9fcb90f09737b9a41fa2734ec44a8f0b"),
			voice("af_alloy", "Alloy", "female", "American", "Balanced, versatile", ""),
			voice("af_aoede", "Aoede", "female", "American", "Melodic, clear", ""),
			voice("af_bella", "Bella", "female", "American", "Bright, friendly", ""),
			voice("af_jessica", "Jessica", "female", "American", "Professional, neutral", ""),
			voice("af_kore", "Kore", "female", "American", "Youthful, energetic", ""),
			voice("af_nicole", "Nicole", "female", "American", "Smooth, conversational", ""),
			voice("af_nova", "Nova", "female", "American", "Modern, dynamic", ""),
			voice("af_river", "River", "female", "American", "Calm, flowing", ""),
			voice("af_sarah", "Sarah", "female", "American", "Warm, natural", ""),
			voice("af_sky", "Sky", "female", "American", "Light, airy", ""),
			// American male
			voice("am_adam", "Adam", "male", "American", "Deep, authoritative", ""),
			voice("am_echo", "Echo", "male", "American", "Resonant, clear", ""),
			voice("am_eric", "Eric", "male", "American", "Friendly, approachable", ""),
			voice("am_fenrir", "Fenrir", "male", "American", "Strong, commanding", ""),
			voice("am_liam", "Liam", "male", "American", "Young, casual", ""),
			voice("am_michael", "Michael", "male", "American", "Professional, steady", ""),
			voice("am_onyx", "Onyx", "male", "American", "Rich, deep", ""),
			voice("am_puck", "Puck", "male", "American", "Playful, lively", ""),
			// British female
			voice("bf_alice", "Alice", "female", "British", "Elegant, refined", ""),
			voice("bf_emma", "Emma", "female", "British", "Warm, classic", ""),
			voice("bf_isabella", "Isabella", "female", "British", "Sophisticated, poised", ""),
			voice("bf_lily", "Lily", "female", "British", "Gentle, soft", ""),
			// British male
			voice("bm_daniel", "Daniel", "male", "British", "Crisp, articulate", ""),
			voice("bm_fable", "Fable", "male", "British", "Storytelling, warm", ""),
			voice("bm_george", "George", "male", "British", "Distinguished, formal", ""),
			voice("bm_lewis", "Lewis", "male", "British", "Relaxed, modern", ""),
		},
		Meta: map[string]string{
			"sample_rate":   "24000",
			"style_dim":     "256",
			"max_tokens":    "510",
			"default_voice": "af_heart",
			"model_file":    "model_quantized.onnx",
		},
	},
	{
		Index:       1,
		ID:          moonshineModelID,
		Kind:        KindSTT,
		Location:    Local,
		Name:        "Moonshine Tiny",
		Description: "Quantized English Moonshine STT model",
		Lang:        []string{"en"},
		Files: []Artefact{
			{
				Filename:  "encoder_model_quantized.onnx",
				SizeBytes: 7_937_661,
				URL:       moonshineONNXBaseURL + "/encoder_model_quantized.onnx",
			},
			{
				Filename:  "decoder_model_merged_quantized.onnx",
				SizeBytes: 20_243_286,
				URL:       moonshineONNXBaseURL + "/decoder_model_merged_quantized.onnx",
			},
			{
				Filename:  "tokenizer.json",
				SizeBytes: 3_761_754,
				URL:       moonshineHFBaseURL + "/tokenizer.json",
			},
		},
		Meta: map[string]string{
			"sample_rate":    "16000",
			"encoder_file":   "encoder_model_quantized.onnx",
			"decoder_file":   "decoder_model_merged_quantized.onnx",
			"tokenizer_file": "tokenizer.json",
			"num_layers":     "6",
			"num_heads":      "8",
			"head_dim":       "36",
			"max_steps":      "448",
			"bos_token":      "1",
			"eos_token":      "2",
		},
	},
	{
		Index:       1,
		ID:          ortModelID,
		Kind:        KindRuntime,
		Location:    Local,
		Name:        "ONNX Runtime",
		Description: "Shared runtime for local ONNX models",
		Meta: map[string]string{
			"version": "1.24.1",
		},
	},
}

func voice(id, name, gender, accent, description, sha string) Voice {
	return Voice{
		ID:          id,
		Name:        name,
		Gender:      gender,
		Accent:      accent,
		Description: description,
		File: Artefact{
			Filename:  "voices/" + id + ".bin",
			SizeBytes: defaultVoiceSize,
			SHA256:    sha,
		},
	}
}

// Get returns a visible model by exact ID, or nil if not found.
func Get(id string) *Model {
	for _, model := range models {
		if model.ID == id && visible(model) {
			return model
		}
	}
	return nil
}

// GetByIndex returns a visible model by kind-specific 1-based index.
func GetByIndex(kind Kind, index int) *Model {
	for _, model := range models {
		if model.Kind == kind && model.Index == index && visible(model) {
			return model
		}
	}
	return nil
}

// GetByKind returns all visible models for a kind in registry order.
func GetByKind(kind Kind) []*Model {
	var out []*Model
	for _, model := range models {
		if model.Kind == kind && visible(model) {
			out = append(out, model)
		}
	}
	return out
}

// Default returns the first visible model for a kind.
func Default(kind Kind) *Model {
	for _, model := range models {
		if model.Kind == kind && visible(model) {
			return model
		}
	}
	return nil
}

// Resolve resolves a kind-specific ref. Numeric refs use the stable index.
func Resolve(kind Kind, ref string) *Model {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Default(kind)
	}
	if index, err := strconv.Atoi(ref); err == nil {
		if model := GetByIndex(kind, index); model != nil {
			return model
		}
	}
	model := Get(ref)
	if model == nil || model.Kind != kind {
		return nil
	}
	return model
}

// ListModels returns all visible models in registry order.
func ListModels() []Model {
	var out []Model
	for _, model := range models {
		if visible(model) {
			out = append(out, *model)
		}
	}
	return out
}

// GetVoice finds a voice by numeric index (1-based), ID, or short name.
func (m *Model) GetVoice(name string) *Voice {
	if m == nil {
		return nil
	}

	if n, err := strconv.Atoi(name); err == nil && n >= 1 && n <= len(m.Voices) {
		return &m.Voices[n-1]
	}

	lower := strings.ToLower(name)
	for i := range m.Voices {
		if m.Voices[i].ID == lower || strings.ToLower(m.Voices[i].Name) == lower {
			return &m.Voices[i]
		}
	}
	return nil
}

// VoiceNumber returns the 1-based index of a voice, formatted as "01", "02".
func (m *Model) VoiceNumber(v *Voice) string {
	if m == nil || v == nil {
		return "??"
	}
	for i := range m.Voices {
		if m.Voices[i].ID == v.ID {
			return fmt.Sprintf("%02d", i+1)
		}
	}
	return "??"
}

// VoicesByAccent returns voices grouped by accent.
func (m *Model) VoicesByAccent() map[string][]Voice {
	groups := make(map[string][]Voice)
	if m == nil {
		return groups
	}
	for _, v := range m.Voices {
		groups[v.Accent] = append(groups[v.Accent], v)
	}
	return groups
}

// VoiceRefs returns addressable voice pointers in stable order.
func (m *Model) VoiceRefs() []*Voice {
	if m == nil {
		return nil
	}
	out := make([]*Voice, 0, len(m.Voices))
	for i := range m.Voices {
		out = append(out, &m.Voices[i])
	}
	return out
}

// File returns a model file by exact filename.
func (m *Model) File(name string) *Artefact {
	if m == nil {
		return nil
	}
	for i := range m.Files {
		if m.Files[i].Filename == name {
			return &m.Files[i]
		}
	}
	return nil
}

// PrimaryFile returns the first registered file for a model, if any.
func (m *Model) PrimaryFile() *Artefact {
	if m == nil || len(m.Files) == 0 {
		return nil
	}
	return &m.Files[0]
}

// MetaString returns a metadata value or fallback.
func (m *Model) MetaString(key, fallback string) string {
	if m == nil || m.Meta == nil {
		return fallback
	}
	value := strings.TrimSpace(m.Meta[key])
	if value == "" {
		return fallback
	}
	return value
}

// MetaInt returns an integer metadata value or fallback.
func (m *Model) MetaInt(key string, fallback int) int {
	value := m.MetaString(key, "")
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func visible(model *Model) bool {
	if model == nil {
		return false
	}
	if model.Location != Remote {
		return true
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != ""
}
