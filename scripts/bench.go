// bench.go — parallel load test for cattery server.
//
// Usage: go run scripts/bench.go [-n 10] [-workers 1] [-port 7100]
//
// Sends N parallel TTS requests and reports timing + memory stats.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type ttsReq struct {
	Text  string `json:"text"`
	Voice string `json:"voice,omitempty"`
}

type result struct {
	id       int
	status   int
	size     int
	duration time.Duration
	err      error
}

func main() {
	n := 10
	port := os.Getenv("BENCH_PORT")
	if port == "" {
		port = "7199"
	}
	baseURL := fmt.Sprintf("http://localhost:%s", port)

	// Varied-length texts to simulate real load
	texts := []string{
		"Hello world, this is a test.",
		"The quick brown fox jumps over the lazy dog near the riverbank.",
		"She sells seashells by the seashore.",
		"To be or not to be, that is the question.",
		"In a hole in the ground there lived a hobbit.",
		"It was the best of times, it was the worst of times.",
		"All that glitters is not gold.",
		"A journey of a thousand miles begins with a single step.",
		"The only thing we have to fear is fear itself.",
		"I think therefore I am, said the philosopher quietly.",
	}

	// Check server is up
	resp, err := http.Get(baseURL + "/v1/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "server not reachable at %s: %v\n", baseURL, err)
		os.Exit(1)
	}
	resp.Body.Close()

	// Fetch heap before
	heapBefore := fetchHeap(baseURL)

	fmt.Printf("Sending %d parallel requests to %s\n\n", n, baseURL)

	results := make([]result, n)
	var wg sync.WaitGroup
	t0 := time.Now()

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			body, _ := json.Marshal(ttsReq{
				Text: texts[id%len(texts)],
			})

			start := time.Now()
			resp, err := http.Post(baseURL+"/v1/tts", "application/json", bytes.NewReader(body))
			if err != nil {
				results[id] = result{id: id, err: err}
				return
			}
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			results[id] = result{
				id:       id,
				status:   resp.StatusCode,
				size:     len(data),
				duration: time.Since(start),
			}
		}(i)
	}

	wg.Wait()
	wallTime := time.Since(t0)

	// Fetch heap after
	heapAfter := fetchHeap(baseURL)

	// Print results
	fmt.Printf("%-4s %-8s %-10s %s\n", "#", "Status", "Size", "Latency")
	fmt.Printf("%-4s %-8s %-10s %s\n", "---", "------", "----", "-------")

	var durations []time.Duration
	ok := 0
	fail := 0
	totalBytes := 0

	for _, r := range results {
		if r.err != nil {
			fmt.Printf("%-4d %-8s %-10s %v\n", r.id, "ERR", "-", r.err)
			fail++
			continue
		}
		status := fmt.Sprintf("%d", r.status)
		if r.status == 200 {
			ok++
		} else {
			fail++
		}
		totalBytes += r.size
		durations = append(durations, r.duration)
		fmt.Printf("%-4d %-8s %-10s %s\n", r.id, status, fmtBytes(r.size), r.duration.Round(time.Millisecond))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	fmt.Println()
	fmt.Println("--- Summary ---")
	fmt.Printf("Requests:    %d ok, %d failed\n", ok, fail)
	fmt.Printf("Wall time:   %s\n", wallTime.Round(time.Millisecond))
	if len(durations) > 0 {
		fmt.Printf("Latency min: %s\n", durations[0].Round(time.Millisecond))
		fmt.Printf("Latency p50: %s\n", durations[len(durations)/2].Round(time.Millisecond))
		fmt.Printf("Latency p95: %s\n", durations[int(float64(len(durations))*0.95)].Round(time.Millisecond))
		fmt.Printf("Latency max: %s\n", durations[len(durations)-1].Round(time.Millisecond))
	}
	fmt.Printf("Total audio: %s\n", fmtBytes(totalBytes))
	if wallTime > 0 {
		fmt.Printf("Throughput:  %.1f req/s\n", float64(ok)/wallTime.Seconds())
	}

	fmt.Println()
	fmt.Println("--- Memory ---")
	fmt.Printf("Heap before: %s\n", heapBefore)
	fmt.Printf("Heap after:  %s\n", heapAfter)
}

func fetchHeap(baseURL string) string {
	// Try debug=1 format first (text), then binary
	resp, err := http.Get(baseURL + "/debug/pprof/heap?debug=1")
	if err != nil {
		return "unavailable"
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	// Parse HeapInuse or HeapAlloc from pprof debug output
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		s := string(line)
		if bytes.Contains(line, []byte("HeapInuse")) ||
			bytes.Contains(line, []byte("HeapAlloc")) ||
			bytes.Contains(line, []byte("Sys")) {
			return s
		}
	}
	// Fallback: show first few lines
	preview := string(data)
	if len(preview) > 200 {
		preview = preview[:200]
	}
	return fmt.Sprintf("raw: %s", preview)
}

func fmtBytes(b int) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
