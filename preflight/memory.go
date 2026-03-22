package preflight

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ChunkSizeEnvVar     = "CATTERY_CHUNK_SIZE"
	MinChunkSize        = 10 * time.Second
	DefaultChunkSize    = 30 * time.Second
	MaxChunkSize        = 60 * time.Second
	LowMemoryChunkMB    = 512
	oneGBInMB           = 1024
	chunkSizeBoundsText = "10s and 60s"
)

// ChunkSizeSource describes where the active chunk size came from.
type ChunkSizeSource string

const (
	ChunkSizeSourceAuto     ChunkSizeSource = "auto"
	ChunkSizeSourceEnv      ChunkSizeSource = "env"
	ChunkSizeSourceExplicit ChunkSizeSource = "explicit"
)

// ChunkSizeResolution holds the resolved chunk size and its origin.
type ChunkSizeResolution struct {
	Duration          time.Duration
	Source            ChunkSizeSource
	AvailableMemoryMB int
}

var lowMemoryWarningOnce sync.Once

// AvailableMemoryMB returns available system memory in MB, or -1 if unknown.
func AvailableMemoryMB() int {
	switch runtime.GOOS {
	case "linux":
		return linuxAvailableMemoryMB()
	default:
		return -1
	}
}

// AutoChunkSize maps available RAM to a conservative streaming chunk size.
func AutoChunkSize(availableMB int) time.Duration {
	switch {
	case availableMB < 0:
		return DefaultChunkSize
	case availableMB <= 512:
		return MinChunkSize
	case availableMB <= oneGBInMB:
		return 15 * time.Second
	case availableMB <= 2*oneGBInMB:
		return 20 * time.Second
	case availableMB <= 4*oneGBInMB:
		return 30 * time.Second
	case availableMB <= 8*oneGBInMB:
		return 45 * time.Second
	default:
		return MaxChunkSize
	}
}

// ParseChunkSizeOverride parses an explicit chunk-size override.
func ParseChunkSizeOverride(source, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s is empty", strings.TrimSpace(source))
	}

	duration, err := parseChunkSizeValue(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", strings.TrimSpace(source), value, err)
	}
	if duration < MinChunkSize || duration > MaxChunkSize {
		return 0, fmt.Errorf("%s must be between %s", strings.TrimSpace(source), chunkSizeBoundsText)
	}

	return duration, nil
}

// ResolveChunkSize applies explicit, env, then auto precedence.
func ResolveChunkSize(explicitValue, explicitSource, envValue string) (ChunkSizeResolution, error) {
	explicitValue = strings.TrimSpace(explicitValue)
	if explicitValue != "" {
		duration, err := ParseChunkSizeOverride(explicitSource, explicitValue)
		if err != nil {
			return ChunkSizeResolution{}, err
		}
		return ChunkSizeResolution{
			Duration:          duration,
			Source:            ChunkSizeSourceExplicit,
			AvailableMemoryMB: -1,
		}, nil
	}

	envValue = strings.TrimSpace(envValue)
	if envValue != "" {
		duration, err := ParseChunkSizeOverride(ChunkSizeEnvVar, envValue)
		if err != nil {
			return ChunkSizeResolution{}, err
		}
		return ChunkSizeResolution{
			Duration:          duration,
			Source:            ChunkSizeSourceEnv,
			AvailableMemoryMB: -1,
		}, nil
	}

	availableMB := AvailableMemoryMB()
	return ChunkSizeResolution{
		Duration:          clampChunkSize(AutoChunkSize(availableMB)),
		Source:            ChunkSizeSourceAuto,
		AvailableMemoryMB: availableMB,
	}, nil
}

// ShouldWarnLowMemory reports whether auto mode fell back to the minimum chunk.
func (r ChunkSizeResolution) ShouldWarnLowMemory() bool {
	return r.Source == ChunkSizeSourceAuto &&
		r.AvailableMemoryMB >= 0 &&
		r.AvailableMemoryMB <= LowMemoryChunkMB &&
		r.Duration == MinChunkSize
}

// WarnLowMemoryChunkSize prints the low-memory warning once per process.
func WarnLowMemoryChunkSize(w io.Writer, r ChunkSizeResolution) {
	if w == nil || !r.ShouldWarnLowMemory() {
		return
	}

	lowMemoryWarningOnce.Do(func() {
		fmt.Fprintf(
			w,
			"warning: low memory (%d MB available) - using minimum 10s chunk size where supported\n",
			r.AvailableMemoryMB,
		)
	})
}

// GuardMemoryError normalizes known runtime OOM failures into one-line errors.
func GuardMemoryError(action string, fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if memErr := normalizeMemoryFailure(action, recovered); memErr != nil {
				err = memErr
				return
			}
			panic(recovered)
		}
	}()

	err = fn()
	if memErr := normalizeMemoryFailure(action, err); memErr != nil {
		return memErr
	}
	return err
}

// IsMemoryError reports whether err already represents a normalized OOM error.
func IsMemoryError(err error) bool {
	if err == nil {
		return false
	}
	return isMemoryFailureText(err.Error())
}

// linuxAvailableMemoryMB reads MemAvailable from /proc/meminfo.
func linuxAvailableMemoryMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return -1
	}
	return parseLinuxMemAvailableMB(data)
}

func parseLinuxMemAvailableMB(data []byte) int {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return -1
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			return -1
		}
		return kb / 1024
	}
	return -1
}

func parseChunkSizeValue(value string) (time.Duration, error) {
	if isBareInteger(value) {
		seconds, err := strconv.Atoi(value)
		if err != nil {
			return 0, err
		}
		return time.Duration(seconds) * time.Second, nil
	}
	return time.ParseDuration(value)
}

func isBareInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func clampChunkSize(duration time.Duration) time.Duration {
	if duration < MinChunkSize {
		return MinChunkSize
	}
	if duration > MaxChunkSize {
		return MaxChunkSize
	}
	return duration
}

func normalizeMemoryFailure(action string, value any) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case error:
		if !isMemoryFailureText(v.Error()) {
			return nil
		}
	case string:
		if !isMemoryFailureText(v) {
			return nil
		}
	default:
		if !isMemoryFailureText(fmt.Sprint(v)) {
			return nil
		}
	}

	action = strings.TrimSpace(action)
	if action == "" {
		action = "runtime work"
	}
	return fmt.Errorf("out of memory during %s", action)
}

func isMemoryFailureText(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}

	for _, needle := range []string{
		"out of memory",
		"cannot allocate memory",
		"failed to allocate memory",
		"memory allocation failed",
		"bad_alloc",
		"bad alloc",
		"enomem",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
