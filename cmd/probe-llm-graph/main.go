package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jikkuatwork/cattery/ort"
	ortgo "github.com/yalue/onnxruntime_go"
)

const (
	modelRepo = "https://huggingface.co/onnx-community/Qwen3.5-4B-ONNX/resolve/main"
	modelDir  = "models-data/qwen3.5-4b"

	decoderRelPath = "onnx/decoder_model_merged_q4.onnx"
	embedRelPath   = "onnx/embed_tokens_q4.onnx"
)

var modelFiles = []string{
	decoderRelPath,
	embedRelPath,
}

func main() {
	fmt.Println("=== Qwen3.5 4B Graph Probe ===")
	fmt.Printf("Model dir: %s\n", modelDir)

	if err := ensureFiles(); err != nil {
		fatalf("ensure files: %v", err)
	}

	ortLib := filepath.Join(os.Getenv("HOME"), ".cattery", "ort", "libonnxruntime.so.1.24.4")
	if _, err := os.Stat(ortLib); err != nil {
		fatalf("missing ORT shared library at %s: %v", ortLib, err)
	}
	if err := ort.Init(ortLib); err != nil {
		fatalf("init ORT: %v", err)
	}
	defer ort.Shutdown()

	embedInputs, embedOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, filepath.FromSlash(embedRelPath)))
	if err != nil {
		fatalf("embed I/O: %v", err)
	}
	decoderInputs, decoderOutputs, err := ortgo.GetInputOutputInfo(filepath.Join(modelDir, filepath.FromSlash(decoderRelPath)))
	if err != nil {
		fatalf("decoder I/O: %v", err)
	}

	fmt.Println("\n=== Graph I/O ===")
	printIO("Embed inputs", embedInputs)
	printIO("Embed outputs", embedOutputs)
	printIO("Decoder inputs", decoderInputs)
	printIO("Decoder outputs", decoderOutputs)

	printSummary(embedOutputs, decoderInputs, decoderOutputs)
}

func printSummary(embedOutputs, decoderInputs, decoderOutputs []ortgo.InputOutputInfo) {
	var (
		positionShapes  [][]int64
		convLayers      = map[int]bool{}
		recurrentLayers = map[int]bool{}
		kvLayers        = map[int]bool{}
		kvHeadShapes    []string
	)

	for _, info := range decoderInputs {
		switch {
		case strings.Contains(info.Name, "position_ids"):
			positionShapes = append(positionShapes, []int64(info.Dimensions))
		case strings.HasPrefix(info.Name, "past_conv."):
			if layer, ok := parseLayerIndex(info.Name, "past_conv."); ok {
				convLayers[layer] = true
			}
		case strings.HasPrefix(info.Name, "past_recurrent."):
			if layer, ok := parseLayerIndex(info.Name, "past_recurrent."); ok {
				recurrentLayers[layer] = true
			}
		case strings.HasPrefix(info.Name, "past_key_values."):
			if layer, ok := parseLayerIndex(info.Name, "past_key_values."); ok {
				kvLayers[layer] = true
				if strings.HasSuffix(info.Name, ".key") {
					kvHeadShapes = append(kvHeadShapes, fmt.Sprintf("%s %v", info.Name, []int64(info.Dimensions)))
				}
			}
		}
	}

	hiddenSize := int64(0)
	for _, info := range embedOutputs {
		if info.Name == "inputs_embeds" && len(info.Dimensions) >= 3 {
			hiddenSize = info.Dimensions[len(info.Dimensions)-1]
		}
	}

	fmt.Println("\n=== Derived Summary ===")
	if len(convLayers) == 0 && len(recurrentLayers) == 0 && len(kvLayers) > 0 {
		fmt.Println("Layout: standard transformer KV-cache only")
	} else {
		fmt.Println("Layout: hybrid state layout")
	}
	fmt.Printf("position_ids shapes: %s\n", joinShapes(positionShapes))
	fmt.Printf("embed hidden size: %d\n", hiddenSize)
	fmt.Printf("past_conv layers: %s\n", formatLayerSet(convLayers))
	fmt.Printf("past_recurrent layers: %s\n", formatLayerSet(recurrentLayers))
	fmt.Printf("past_key_values layers (%d): %s\n", len(kvLayers), formatLayerSet(kvLayers))
	if len(kvHeadShapes) > 0 {
		sort.Strings(kvHeadShapes)
		fmt.Println("KV key shapes:")
		for _, shape := range kvHeadShapes {
			fmt.Printf("  - %s\n", shape)
		}
	}

	presentCount := 0
	for _, info := range decoderOutputs {
		if strings.HasPrefix(info.Name, "present") {
			presentCount++
		}
	}
	fmt.Printf("decoder present-state outputs: %d\n", presentCount)
}

func parseLayerIndex(name, prefix string) (int, bool) {
	rest := strings.TrimPrefix(name, prefix)
	layerText := rest
	if dot := strings.IndexByte(rest, '.'); dot >= 0 {
		layerText = rest[:dot]
	}
	layer, err := strconv.Atoi(layerText)
	if err != nil {
		return 0, false
	}
	return layer, true
}

func joinShapes(shapes [][]int64) string {
	if len(shapes) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(shapes))
	for _, shape := range shapes {
		parts = append(parts, fmt.Sprintf("%v", shape))
	}
	return strings.Join(parts, ", ")
}

func formatLayerSet(set map[int]bool) string {
	if len(set) == 0 {
		return "<none>"
	}
	layers := make([]int, 0, len(set))
	for layer := range set {
		layers = append(layers, layer)
	}
	sort.Ints(layers)
	return fmt.Sprintf("%v", layers)
}

func printIO(label string, infos []ortgo.InputOutputInfo) {
	fmt.Println(label + ":")
	for _, info := range infos {
		fmt.Printf("  - %s | type=%s | elem=%s | shape=%v\n", info.Name, info.OrtValueType, info.DataType, []int64(info.Dimensions))
	}
}

func ensureFiles() error {
	for _, rel := range modelFiles {
		url := modelRepo + "/" + rel
		dest := filepath.Join(modelDir, filepath.FromSlash(rel))
		if _, err := ensureFile(url, dest); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	return nil
}

func ensureFile(url, path string) (string, error) {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		fmt.Printf("cached: %s\n", path)
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	fmt.Printf("download: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
