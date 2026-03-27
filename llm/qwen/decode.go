package qwen

import (
	"context"
	"fmt"
	"strings"

	ortgo "github.com/yalue/onnxruntime_go"
)

type stateFamily int

const (
	stateFamilyConv stateFamily = iota
	stateFamilyRecurrent
	stateFamilyKV
)

type stateSpec struct {
	input      ortgo.InputOutputInfo
	outputName string
	family     stateFamily
}

type stateGroup struct {
	specs  []stateSpec
	values []ortgo.Value
}

type decodeState struct {
	conv      stateGroup
	recurrent stateGroup
	kv        stateGroup
}

func newDecodeState(spec *modelSpec) *decodeState {
	state := &decodeState{
		conv: stateGroup{
			specs:  append([]stateSpec(nil), spec.convStates...),
			values: make([]ortgo.Value, len(spec.convStates)),
		},
		recurrent: stateGroup{
			specs:  append([]stateSpec(nil), spec.recurrentStates...),
			values: make([]ortgo.Value, len(spec.recurrentStates)),
		},
		kv: stateGroup{
			specs:  append([]stateSpec(nil), spec.kvStates...),
			values: make([]ortgo.Value, len(spec.kvStates)),
		},
	}
	return state
}

func (s *decodeState) Destroy() {
	if s == nil {
		return
	}
	s.conv.destroy()
	s.recurrent.destroy()
	s.kv.destroy()
}

func (g *stateGroup) destroy() {
	for i := range g.values {
		if g.values[i] != nil {
			g.values[i].Destroy()
			g.values[i] = nil
		}
	}
}

func (s *decodeState) buildInputs(spec *modelSpec, embeds, mask, positions ortgo.Value) ([]ortgo.Value, []ortgo.Value, error) {
	inputs := make([]ortgo.Value, 0, len(spec.decoderInputs))
	var temp []ortgo.Value

	for _, info := range spec.decoderInputs {
		switch info.Name {
		case spec.inputsEmbedsName:
			inputs = append(inputs, embeds)
		case spec.attentionMaskName:
			if mask == nil {
				return nil, nil, fmt.Errorf("missing attention mask")
			}
			inputs = append(inputs, mask)
		case spec.positionIDsName:
			if positions == nil {
				return nil, nil, fmt.Errorf("missing position ids")
			}
			inputs = append(inputs, positions)
		default:
			group, idx := s.findInput(info.Name)
			if group == nil || idx < 0 {
				return nil, nil, fmt.Errorf("unsupported decoder input %q", info.Name)
			}
			if group.values[idx] == nil {
				zero, err := zeroStateTensor(group.specs[idx])
				if err != nil {
					return nil, nil, err
				}
				temp = append(temp, zero)
				inputs = append(inputs, zero)
				continue
			}
			inputs = append(inputs, group.values[idx])
		}
	}
	return inputs, temp, nil
}

func (s *decodeState) replace(outputs []ortgo.Value, spec *modelSpec) error {
	if err := s.replaceGroup(outputs, spec, &s.conv); err != nil {
		return err
	}
	if err := s.replaceGroup(outputs, spec, &s.recurrent); err != nil {
		return err
	}
	if err := s.replaceGroup(outputs, spec, &s.kv); err != nil {
		return err
	}
	return nil
}

func (s *decodeState) replaceGroup(outputs []ortgo.Value, spec *modelSpec, group *stateGroup) error {
	for i, state := range group.specs {
		outIdx := indexOfName(spec.decoderOutputs, state.outputName)
		if outIdx < 0 || outputs[outIdx] == nil {
			return fmt.Errorf("missing state output %q", state.outputName)
		}
		if group.values[i] != nil {
			group.values[i].Destroy()
		}
		group.values[i] = outputs[outIdx]
		outputs[outIdx] = nil
	}
	return nil
}

func (s *decodeState) findInput(name string) (*stateGroup, int) {
	for i, state := range s.conv.specs {
		if state.input.Name == name {
			return &s.conv, i
		}
	}
	for i, state := range s.recurrent.specs {
		if state.input.Name == name {
			return &s.recurrent, i
		}
	}
	for i, state := range s.kv.specs {
		if state.input.Name == name {
			return &s.kv, i
		}
	}
	return nil, -1
}

func generateTokens(
	ctx context.Context,
	embedSession *ortgo.DynamicAdvancedSession,
	decoderSession *ortgo.DynamicAdvancedSession,
	spec *modelSpec,
	promptIDs []int64,
	maxTokens int,
) ([]int64, string, error) {
	if len(promptIDs) == 0 {
		return nil, "", fmt.Errorf("prompt encoded to zero tokens")
	}
	if maxTokens <= 0 {
		return nil, "length", nil
	}

	initialEmbeds, err := embedTokens(embedSession, spec, promptIDs)
	if err != nil {
		return nil, "", err
	}
	defer initialEmbeds.Destroy()

	state := newDecodeState(spec)
	defer state.Destroy()

	totalSeq := int64(len(promptIDs))
	stepEmbeds := ortgo.Value(initialEmbeds)
	var generated []int64
	finishReason := "length"

	for step := 0; step < maxTokens; step++ {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}

		stepSeq := embeddingsSeqLen(stepEmbeds)
		if stepSeq <= 0 {
			return nil, "", fmt.Errorf("invalid embeddings seq len at step %d", step)
		}

		mask, positions, err := buildAuxTensors(spec, totalSeq, stepSeq)
		if err != nil {
			return nil, "", fmt.Errorf("step %d aux tensors: %w", step, err)
		}

		inputs, temp, err := state.buildInputs(spec, stepEmbeds, mask, positions)
		if err != nil {
			destroyMaybe(mask)
			destroyMaybe(positions)
			return nil, "", fmt.Errorf("step %d decoder inputs: %w", step, err)
		}

		outputs := make([]ortgo.Value, len(spec.decoderOutputs))
		runErr := decoderSession.Run(inputs, outputs)

		destroyValues(temp)
		destroyMaybe(mask)
		destroyMaybe(positions)
		if runErr != nil {
			destroyValues(outputs)
			return nil, "", fmt.Errorf("decoder run step %d: %w", step, runErr)
		}

		logitsIdx := indexOfName(spec.decoderOutputs, spec.logitsName)
		if logitsIdx < 0 {
			destroyValues(outputs)
			return nil, "", fmt.Errorf("decoder logits output missing")
		}
		next, err := greedyArgmax(outputs[logitsIdx])
		if err != nil {
			destroyValues(outputs)
			return nil, "", fmt.Errorf("decoder logits step %d: %w", step, err)
		}

		if err := state.replace(outputs, spec); err != nil {
			destroyValues(outputs)
			return nil, "", fmt.Errorf("decoder state step %d: %w", step, err)
		}
		destroyValues(outputs)

		generated = append(generated, next)
		if spec.eosTokenIDs[next] {
			finishReason = "eos"
			break
		}

		nextEmbeds, err := embedTokens(embedSession, spec, []int64{next})
		if err != nil {
			return nil, "", fmt.Errorf("embed next token step %d: %w", step, err)
		}
		if stepEmbeds != nil && stepEmbeds != initialEmbeds {
			stepEmbeds.Destroy()
		}
		stepEmbeds = nextEmbeds
		totalSeq++
	}

	if stepEmbeds != nil && stepEmbeds != initialEmbeds {
		stepEmbeds.Destroy()
	}
	return generated, finishReason, nil
}

func buildAuxTensors(spec *modelSpec, totalSeq, stepSeq int64) (ortgo.Value, ortgo.Value, error) {
	var mask ortgo.Value
	var positions ortgo.Value

	if spec.attentionMaskName != "" {
		values := make([]int64, totalSeq)
		for i := range values {
			values[i] = 1
		}
		tensor, err := ortgo.NewTensor(ortgo.NewShape(1, totalSeq), values)
		if err != nil {
			return nil, nil, err
		}
		mask = tensor
	}

	if spec.positionIDsName != "" {
		start := totalSeq - stepSeq
		values := make([]int64, 3*stepSeq)
		for group := int64(0); group < 3; group++ {
			for i := int64(0); i < stepSeq; i++ {
				values[group*stepSeq+i] = start + i
			}
		}
		tensor, err := ortgo.NewTensor(ortgo.NewShape(3, 1, stepSeq), values)
		if err != nil {
			destroyMaybe(mask)
			return nil, nil, err
		}
		positions = tensor
	}

	return mask, positions, nil
}

func zeroStateTensor(state stateSpec) (*ortgo.Tensor[float32], error) {
	shape := make([]int64, len(state.input.Dimensions))
	size := int64(1)
	for i, dim := range state.input.Dimensions {
		switch {
		case dim > 0:
			shape[i] = dim
		case i == 0:
			shape[i] = 1
		case state.family == stateFamilyKV && i == 2:
			shape[i] = 0
		default:
			shape[i] = 1
		}
		size *= shape[i]
	}
	if size == 0 {
		return ortgo.NewTensor(ortgo.Shape(shape), []float32{})
	}
	return ortgo.NewTensor(ortgo.Shape(shape), make([]float32, size))
}

func embedTokens(session *ortgo.DynamicAdvancedSession, spec *modelSpec, ids []int64) (*ortgo.Tensor[float32], error) {
	idsTensor, err := ortgo.NewTensor(ortgo.NewShape(1, int64(len(ids))), ids)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer idsTensor.Destroy()

	outputs := make([]ortgo.Value, len(spec.embedOutputs))
	if err := session.Run([]ortgo.Value{idsTensor}, outputs); err != nil {
		return nil, fmt.Errorf("run embed session: %w", err)
	}

	if len(outputs) != 1 {
		destroyValues(outputs)
		return nil, fmt.Errorf("unexpected embed output count %d", len(outputs))
	}
	tensor, ok := outputs[0].(*ortgo.Tensor[float32])
	if !ok {
		destroyValues(outputs)
		return nil, fmt.Errorf("unexpected embed output type %T", outputs[0])
	}
	return tensor, nil
}

func greedyArgmax(v ortgo.Value) (int64, error) {
	tensor, ok := v.(*ortgo.Tensor[float32])
	if !ok {
		return 0, fmt.Errorf("unexpected logits type %T", v)
	}
	shape := tensor.GetShape()
	if len(shape) == 0 {
		return 0, fmt.Errorf("empty logits shape")
	}
	vocabSize := int(shape[len(shape)-1])
	logits := tensor.GetData()
	if vocabSize <= 0 || len(logits) < vocabSize {
		return 0, fmt.Errorf("invalid logits buffer size")
	}

	start := len(logits) - vocabSize
	best := 0
	bestVal := logits[start]
	for i := 1; i < vocabSize; i++ {
		if logits[start+i] > bestVal {
			best = i
			bestVal = logits[start+i]
		}
	}
	return int64(best), nil
}

func embeddingsSeqLen(v ortgo.Value) int64 {
	tensor, ok := v.(*ortgo.Tensor[float32])
	if !ok {
		return 0
	}
	shape := tensor.GetShape()
	if len(shape) < 2 {
		return 0
	}
	return shape[1]
}

func destroyMaybe(v ortgo.Value) {
	if v != nil {
		v.Destroy()
	}
}

func destroyValues(values []ortgo.Value) {
	for i := range values {
		if values[i] != nil {
			values[i].Destroy()
			values[i] = nil
		}
	}
}

func indexOfName(infos []ortgo.InputOutputInfo, want string) int {
	for i, info := range infos {
		if info.Name == want {
			return i
		}
	}
	return -1
}

func stateOutputName(inputName string) string {
	switch {
	case strings.HasPrefix(inputName, "past_conv."):
		return strings.Replace(inputName, "past_conv.", "present_conv.", 1)
	case strings.HasPrefix(inputName, "past_recurrent."):
		return strings.Replace(inputName, "past_recurrent.", "present_recurrent.", 1)
	case strings.HasPrefix(inputName, "past_key_values."):
		return strings.Replace(inputName, "past_key_values.", "present.", 1)
	default:
		return ""
	}
}
