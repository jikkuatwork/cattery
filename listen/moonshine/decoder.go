package moonshine

import (
	"fmt"

	ortgo "github.com/yalue/onnxruntime_go"
)

const (
	decoderKey = iota
	decoderValue
	encoderKey
	encoderValue
	kvTensorsPerLayer
)

type decoder struct {
	session   *ortgo.DynamicAdvancedSession
	numLayers int
	numHeads  int
	headDim   int
	maxSteps  int
	bosToken  int64
	eosToken  int64
}

type layerCache [kvTensorsPerLayer]ortgo.Value

type kvCache struct {
	layers []layerCache
}

func newKVCache(numLayers int) kvCache {
	return kvCache{layers: make([]layerCache, numLayers)}
}

func (kv *kvCache) destroy() {
	if kv == nil {
		return
	}
	for i := range kv.layers {
		for j := range kv.layers[i] {
			if kv.layers[i][j] != nil {
				kv.layers[i][j].Destroy()
				kv.layers[i][j] = nil
			}
		}
	}
}

func (d *decoder) decode(encHidden ortgo.Value, encSeqLen int64) ([]int64, error) {
	if d == nil || d.session == nil {
		return nil, fmt.Errorf("decoder is closed")
	}
	if encHidden == nil {
		return nil, fmt.Errorf("missing encoder hidden state")
	}
	if encSeqLen <= 0 {
		return nil, fmt.Errorf("invalid encoder sequence length %d", encSeqLen)
	}

	tokens := []int64{d.bosToken}
	cache := newKVCache(d.numLayers)
	defer cache.destroy()

	for step := 0; step < d.maxSteps; step++ {
		inputTokens := tokens
		if step > 0 {
			inputTokens = tokens[len(tokens)-1:]
		}

		idsTensor, err := ortgo.NewTensor(ortgo.NewShape(1, int64(len(inputTokens))), inputTokens)
		if err != nil {
			return nil, fmt.Errorf("decoder step %d: create ids tensor: %w", step, err)
		}

		useCacheTensor, err := ortgo.NewTensor(ortgo.NewShape(1), []bool{step > 0})
		if err != nil {
			idsTensor.Destroy()
			return nil, fmt.Errorf("decoder step %d: create cache flag: %w", step, err)
		}

		kvInputs, ownKV, err := d.kvInputs(step, &cache)
		if err != nil {
			idsTensor.Destroy()
			useCacheTensor.Destroy()
			return nil, fmt.Errorf("decoder step %d: build kv inputs: %w", step, err)
		}

		inputs := make([]ortgo.Value, 0, 3+d.numLayers*kvTensorsPerLayer)
		inputs = append(inputs, idsTensor, encHidden)
		for _, layer := range kvInputs {
			inputs = append(inputs, layer[decoderKey], layer[decoderValue], layer[encoderKey], layer[encoderValue])
		}
		inputs = append(inputs, useCacheTensor)

		outputs := make([]ortgo.Value, 1+d.numLayers*kvTensorsPerLayer)
		runErr := d.session.Run(inputs, outputs)

		idsTensor.Destroy()
		useCacheTensor.Destroy()
		if ownKV {
			destroyLayers(kvInputs)
		}

		if runErr != nil {
			destroyValues(outputs)
			return nil, fmt.Errorf("decoder step %d: %w", step, runErr)
		}

		nextToken, err := nextToken(outputs[0])
		if err != nil {
			destroyValues(outputs)
			return nil, fmt.Errorf("decoder step %d: %w", step, err)
		}

		if err := cache.update(outputs, step); err != nil {
			destroyValues(outputs)
			return nil, fmt.Errorf("decoder step %d: %w", step, err)
		}

		if outputs[0] != nil {
			outputs[0].Destroy()
			outputs[0] = nil
		}

		if nextToken == d.eosToken {
			break
		}
		tokens = append(tokens, nextToken)
	}

	return tokens[1:], nil
}

func (d *decoder) kvInputs(step int, cache *kvCache) ([]layerCache, bool, error) {
	if step > 0 {
		return cache.layers, false, nil
	}

	layers := make([]layerCache, d.numLayers)
	shape := ortgo.NewShape(1, int64(d.numHeads), 0, int64(d.headDim))
	for i := range layers {
		for j := range layers[i] {
			tensor, err := ortgo.NewTensor(shape, []float32{})
			if err != nil {
				destroyLayers(layers)
				return nil, false, err
			}
			layers[i][j] = tensor
		}
	}
	return layers, true, nil
}

func (kv *kvCache) update(outputs []ortgo.Value, step int) error {
	if len(outputs) != 1+len(kv.layers)*kvTensorsPerLayer {
		return fmt.Errorf("unexpected decoder output count %d", len(outputs))
	}

	if step == 0 {
		kv.destroy()
		for layer := range kv.layers {
			for idx := range kv.layers[layer] {
				outIdx := 1 + layer*kvTensorsPerLayer + idx
				if outputs[outIdx] == nil {
					return fmt.Errorf("missing kv output %d", outIdx)
				}
				kv.layers[layer][idx] = outputs[outIdx]
				outputs[outIdx] = nil
			}
		}
		return nil
	}

	for layer := range kv.layers {
		decKeyIdx := 1 + layer*kvTensorsPerLayer + decoderKey
		decValIdx := 1 + layer*kvTensorsPerLayer + decoderValue
		encKeyIdx := 1 + layer*kvTensorsPerLayer + encoderKey
		encValIdx := 1 + layer*kvTensorsPerLayer + encoderValue

		if outputs[decKeyIdx] == nil || outputs[decValIdx] == nil ||
			outputs[encKeyIdx] == nil || outputs[encValIdx] == nil {
			return fmt.Errorf("missing kv output for layer %d", layer)
		}

		if kv.layers[layer][decoderKey] != nil {
			kv.layers[layer][decoderKey].Destroy()
		}
		if kv.layers[layer][decoderValue] != nil {
			kv.layers[layer][decoderValue].Destroy()
		}

		kv.layers[layer][decoderKey] = outputs[decKeyIdx]
		kv.layers[layer][decoderValue] = outputs[decValIdx]
		outputs[decKeyIdx] = nil
		outputs[decValIdx] = nil

		outputs[encKeyIdx].Destroy()
		outputs[encValIdx].Destroy()
		outputs[encKeyIdx] = nil
		outputs[encValIdx] = nil
	}

	return nil
}

func nextToken(logitsValue ortgo.Value) (int64, error) {
	logitsTensor, ok := logitsValue.(*ortgo.Tensor[float32])
	if !ok {
		return 0, fmt.Errorf("unexpected logits type %T", logitsValue)
	}

	shape := logitsTensor.GetShape()
	if len(shape) == 0 {
		return 0, fmt.Errorf("empty logits shape")
	}

	vocabSize := int(shape[len(shape)-1])
	logits := logitsTensor.GetData()
	if vocabSize <= 0 || len(logits) < vocabSize {
		return 0, fmt.Errorf("invalid logits size")
	}

	best := 0
	bestVal := logits[len(logits)-vocabSize]
	start := len(logits) - vocabSize
	for i := 1; i < vocabSize; i++ {
		if logits[start+i] > bestVal {
			bestVal = logits[start+i]
			best = i
		}
	}

	return int64(best), nil
}

func destroyLayers(layers []layerCache) {
	for i := range layers {
		for j := range layers[i] {
			if layers[i][j] != nil {
				layers[i][j].Destroy()
				layers[i][j] = nil
			}
		}
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

func decoderIONames(numLayers int) (inputs, outputs []string) {
	inputs = []string{"input_ids", "encoder_hidden_states"}
	outputs = []string{"logits"}

	for layer := 0; layer < numLayers; layer++ {
		inputs = append(inputs,
			fmt.Sprintf("past_key_values.%d.decoder.key", layer),
			fmt.Sprintf("past_key_values.%d.decoder.value", layer),
			fmt.Sprintf("past_key_values.%d.encoder.key", layer),
			fmt.Sprintf("past_key_values.%d.encoder.value", layer),
		)
		outputs = append(outputs,
			fmt.Sprintf("present.%d.decoder.key", layer),
			fmt.Sprintf("present.%d.decoder.value", layer),
			fmt.Sprintf("present.%d.encoder.key", layer),
			fmt.Sprintf("present.%d.encoder.value", layer),
		)
	}

	inputs = append(inputs, "use_cache_branch")
	return inputs, outputs
}
