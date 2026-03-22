package main

import (
	"io"
	"os"
	"time"

	"github.com/jikkuatwork/cattery/preflight"
)

func resolveCommandChunkSize(flagValue string, stderr io.Writer) (time.Duration, error) {
	return resolveCommandChunkSizeWithEnv(flagValue, os.Getenv(preflight.ChunkSizeEnvVar), stderr)
}

func resolveCommandChunkSizeWithEnv(
	flagValue string,
	envValue string,
	stderr io.Writer,
) (time.Duration, error) {
	resolution, err := preflight.ResolveChunkSize(flagValue, "--chunk-size", envValue)
	if err != nil {
		return 0, err
	}

	preflight.WarnLowMemoryChunkSize(stderr, resolution)
	return resolution.Duration, nil
}
