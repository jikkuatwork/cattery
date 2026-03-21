package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type outputTarget struct {
	name   string
	writer io.Writer
	closer io.Closer
}

func (t *outputTarget) Close() error {
	if t == nil || t.closer == nil {
		return nil
	}
	return t.closer.Close()
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.count += int64(n)
	return n, err
}

func stdinHasData() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice == 0
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func resolveSpeakText(parts []string) (string, error) {
	if len(parts) == 1 && parts[0] == "-" {
		return readStdinText()
	}

	text := strings.TrimSpace(strings.Join(parts, " "))
	if text != "" {
		return text, nil
	}
	if stdinHasData() {
		return readStdinText()
	}
	return "", fmt.Errorf("no text provided\nUsage: cattery speak \"Hello, world.\"")
}

func readStdinText() (string, error) {
	if !stdinHasData() {
		return "", fmt.Errorf("stdin has no text input")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", fmt.Errorf("stdin text is empty")
	}
	return text, nil
}

func openSpeakOutput(path string) (*outputTarget, error) {
	path = strings.TrimSpace(path)
	if path == "-" || (path == "" && !stdoutIsTerminal()) {
		return &outputTarget{
			name:   "stdout",
			writer: os.Stdout,
		}, nil
	}
	if path == "" {
		path = "output.wav"
	}
	return createOutputFile(path)
}

func openTextOutput(path string) (*outputTarget, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return &outputTarget{
			name:   "stdout",
			writer: os.Stdout,
		}, nil
	}
	return createOutputFile(path)
}

func createOutputFile(path string) (*outputTarget, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &outputTarget{
		name:   path,
		writer: f,
		closer: f,
	}, nil
}

func openAudioInput(path, usage string) (io.ReadCloser, string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		if !stdinHasData() {
			return nil, "", fmt.Errorf("no audio provided\nUsage: %s", usage)
		}
		return io.NopCloser(os.Stdin), "stdin", nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}
