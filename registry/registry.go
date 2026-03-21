// Package registry defines available models, voices, and their metadata.
package registry

// Model describes a TTS model available for download.
type Model struct {
	ID          string  // e.g. "kokoro-82m-v1.0"
	Name        string  // e.g. "Kokoro 82M"
	Description string  // e.g. "High-quality 82M parameter model, int8 quantized"
	SizeBytes   int64   // expected file size
	Filename    string  // e.g. "model_quantized.onnx"
	SHA256      string  // checksum
	SampleRate  int     // audio output sample rate
	StyleDim    int     // voice embedding dimension
	MaxTokens   int     // max token sequence length
	Voices      []Voice // available voices for this model
}

// Voice describes a voice available for a model.
type Voice struct {
	ID          string // e.g. "af_heart"
	Name        string // e.g. "Heart"
	Gender      string // "female" or "male"
	Accent      string // e.g. "American", "British"
	Description string // e.g. "Warm, expressive American female"
	SizeBytes   int64
	SHA256      string
}

// Default is the default model ID.
const Default = "kokoro-82m-v1.0"

// DefaultVoice is the default voice ID.
const DefaultVoice = "af_heart"

// Models is the registry of all available models.
var Models = map[string]*Model{
	"kokoro-82m-v1.0": {
		ID:          "kokoro-82m-v1.0",
		Name:        "Kokoro 82M",
		Description: "High-quality 82M parameter model, int8 quantized",
		SizeBytes:   92_361_116,
		Filename:    "model_quantized.onnx",
		SHA256:      "fbae9257e1e05ffc727e951ef9b9c98418e6d79f1c9b6b13bd59f5c9028a1478",
		SampleRate:  24000,
		StyleDim:    256,
		MaxTokens:   510,
		Voices: []Voice{
			// American female
			{ID: "af_heart", Name: "Heart", Gender: "female", Accent: "American", Description: "Warm, expressive", SizeBytes: 522240, SHA256: "d583ccff3cdca2f7fae535cb998ac07e9fcb90f09737b9a41fa2734ec44a8f0b"},
			{ID: "af_alloy", Name: "Alloy", Gender: "female", Accent: "American", Description: "Balanced, versatile"},
			{ID: "af_aoede", Name: "Aoede", Gender: "female", Accent: "American", Description: "Melodic, clear"},
			{ID: "af_bella", Name: "Bella", Gender: "female", Accent: "American", Description: "Bright, friendly"},
			{ID: "af_jessica", Name: "Jessica", Gender: "female", Accent: "American", Description: "Professional, neutral"},
			{ID: "af_kore", Name: "Kore", Gender: "female", Accent: "American", Description: "Youthful, energetic"},
			{ID: "af_nicole", Name: "Nicole", Gender: "female", Accent: "American", Description: "Smooth, conversational"},
			{ID: "af_nova", Name: "Nova", Gender: "female", Accent: "American", Description: "Modern, dynamic"},
			{ID: "af_river", Name: "River", Gender: "female", Accent: "American", Description: "Calm, flowing"},
			{ID: "af_sarah", Name: "Sarah", Gender: "female", Accent: "American", Description: "Warm, natural"},
			{ID: "af_sky", Name: "Sky", Gender: "female", Accent: "American", Description: "Light, airy"},
			// American male
			{ID: "am_adam", Name: "Adam", Gender: "male", Accent: "American", Description: "Deep, authoritative"},
			{ID: "am_echo", Name: "Echo", Gender: "male", Accent: "American", Description: "Resonant, clear"},
			{ID: "am_eric", Name: "Eric", Gender: "male", Accent: "American", Description: "Friendly, approachable"},
			{ID: "am_fenrir", Name: "Fenrir", Gender: "male", Accent: "American", Description: "Strong, commanding"},
			{ID: "am_liam", Name: "Liam", Gender: "male", Accent: "American", Description: "Young, casual"},
			{ID: "am_michael", Name: "Michael", Gender: "male", Accent: "American", Description: "Professional, steady"},
			{ID: "am_onyx", Name: "Onyx", Gender: "male", Accent: "American", Description: "Rich, deep"},
			{ID: "am_puck", Name: "Puck", Gender: "male", Accent: "American", Description: "Playful, lively"},
			// British female
			{ID: "bf_alice", Name: "Alice", Gender: "female", Accent: "British", Description: "Elegant, refined"},
			{ID: "bf_emma", Name: "Emma", Gender: "female", Accent: "British", Description: "Warm, classic"},
			{ID: "bf_isabella", Name: "Isabella", Gender: "female", Accent: "British", Description: "Sophisticated, poised"},
			{ID: "bf_lily", Name: "Lily", Gender: "female", Accent: "British", Description: "Gentle, soft"},
			// British male
			{ID: "bm_daniel", Name: "Daniel", Gender: "male", Accent: "British", Description: "Crisp, articulate"},
			{ID: "bm_fable", Name: "Fable", Gender: "male", Accent: "British", Description: "Storytelling, warm"},
			{ID: "bm_george", Name: "George", Gender: "male", Accent: "British", Description: "Distinguished, formal"},
			{ID: "bm_lewis", Name: "Lewis", Gender: "male", Accent: "British", Description: "Relaxed, modern"},
		},
	},
}

// Get returns a model by ID, or nil if not found.
func Get(id string) *Model {
	return Models[id]
}

// GetVoice finds a voice by ID within a model.
func (m *Model) GetVoice(voiceID string) *Voice {
	for i := range m.Voices {
		if m.Voices[i].ID == voiceID {
			return &m.Voices[i]
		}
	}
	return nil
}

// VoicesByAccent returns voices grouped by accent.
func (m *Model) VoicesByAccent() map[string][]Voice {
	groups := make(map[string][]Voice)
	for _, v := range m.Voices {
		groups[v.Accent] = append(groups[v.Accent], v)
	}
	return groups
}
