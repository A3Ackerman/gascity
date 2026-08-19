//go:build !linux && !darwin

package proctable

import "github.com/gastownhall/gascity/internal/runtime"

// Scan is unavailable on platforms without process environment scanning support.
func Scan(runtime.ProcessTarget) ([]runtime.LiveRuntime, error) {
	return []runtime.LiveRuntime{}, nil
}

// IsScanRoot reports false on platforms without process environment scanning
// support.
func IsScanRoot(int) bool {
	return false
}
