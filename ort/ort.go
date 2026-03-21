// Package ort manages the shared ONNX Runtime lifecycle.
package ort

/*
#include <malloc.h>
*/
import "C"

import (
	"os"
	"runtime/debug"

	ortgo "github.com/yalue/onnxruntime_go"
	"golang.org/x/sys/unix"
)

// Init initializes the ONNX Runtime environment with the given shared library.
// Can be called again after Shutdown to re-initialize (full dlopen/dlclose cycle).
// Suppresses ORT's native stderr warnings (e.g. unknown CPU vendor).
func Init(libPath string) error {
	if ortgo.IsInitialized() {
		return nil
	}

	ortgo.SetSharedLibraryPath(libPath)

	// Redirect stderr to /dev/null during ORT init to suppress C-level warnings.
	stderrFd := int(os.Stderr.Fd())
	origStderr, err := unix.Dup(stderrFd)
	if err == nil {
		devNull, err2 := os.Open(os.DevNull)
		if err2 == nil {
			unix.Dup2(int(devNull.Fd()), stderrFd)
			devNull.Close()
			defer unix.Dup2(origStderr, stderrFd)
			defer unix.Close(origStderr)
		}
	}

	return ortgo.InitializeEnvironment()
}

// Shutdown destroys the ONNX Runtime environment and unloads the shared library
// via dlclose. Calls malloc_trim to return freed C heap pages to the OS.
// Can be followed by Init to reload.
func Shutdown() {
	if ortgo.IsInitialized() {
		ortgo.DestroyEnvironment()
	}
	// Force glibc to release free heap pages back to the OS.
	C.malloc_trim(0)
	// Also release Go's unused memory.
	debug.FreeOSMemory()
}

// IsInitialized returns whether the ORT environment is loaded.
func IsInitialized() bool {
	return ortgo.IsInitialized()
}
