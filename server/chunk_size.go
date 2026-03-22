package server

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/jikkuatwork/cattery/preflight"
)

func resolveServerChunkSize(
	configured time.Duration,
	envValue string,
	stderr io.Writer,
) (time.Duration, error) {
	explicit := ""
	if configured > 0 {
		explicit = strings.TrimSpace(configured.String())
	}

	resolution, err := preflight.ResolveChunkSize(explicit, "server chunk size", envValue)
	if err != nil {
		return 0, err
	}

	preflight.WarnLowMemoryChunkSize(stderr, resolution)
	return resolution.Duration, nil
}

func resolveServerChunkSizeFromEnv(configured time.Duration, stderr io.Writer) (time.Duration, error) {
	return resolveServerChunkSize(configured, os.Getenv(preflight.ChunkSizeEnvVar), stderr)
}
