package main

import (
	"bytes"
	"io"
	"testing"
)

func TestCountingWriterForwardsSeekAndTracksLogicalSize(t *testing.T) {
	dst := &seekRecorder{}
	w := &countingWriter{writer: dst}

	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatalf("Write(initial): %v", err)
	}
	if got, want := w.count, int64(10); got != want {
		t.Fatalf("count after initial write = %d, want %d", got, want)
	}

	pos, err := w.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek(start): %v", err)
	}
	if pos != 0 {
		t.Fatalf("Seek(start) = %d, want 0", pos)
	}

	if _, err := w.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write(rewrite): %v", err)
	}
	if got, want := w.count, int64(10); got != want {
		t.Fatalf("count after rewrite = %d, want %d", got, want)
	}

	pos, err = w.Seek(8, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek(extend): %v", err)
	}
	if pos != 8 {
		t.Fatalf("Seek(extend) = %d, want 8", pos)
	}

	if _, err := w.Write([]byte("wxyz")); err != nil {
		t.Fatalf("Write(extend): %v", err)
	}
	if got, want := w.count, int64(12); got != want {
		t.Fatalf("count after extend = %d, want %d", got, want)
	}

	if got, want := string(dst.data), "abcd4567wxyz"; got != want {
		t.Fatalf("data = %q, want %q", got, want)
	}
}

func TestCountingWriterSeekFailsForNonSeekableWriter(t *testing.T) {
	w := &countingWriter{writer: &bytes.Buffer{}}

	if _, err := w.Seek(0, io.SeekCurrent); err == nil {
		t.Fatalf("Seek() on non-seekable writer returned nil error")
	}
}

type seekRecorder struct {
	data   []byte
	offset int64
}

func (w *seekRecorder) Write(p []byte) (int, error) {
	start := int(w.offset)
	end := start + len(p)
	if end > len(w.data) {
		w.data = append(w.data, make([]byte, end-len(w.data))...)
	}
	copy(w.data[start:end], p)
	w.offset += int64(len(p))
	return len(p), nil
}

func (w *seekRecorder) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = w.offset + offset
	case io.SeekEnd:
		next = int64(len(w.data)) + offset
	default:
		return 0, io.ErrUnexpectedEOF
	}
	if next < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	w.offset = next
	return next, nil
}
