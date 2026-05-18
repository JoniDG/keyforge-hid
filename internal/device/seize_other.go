//go:build !darwin && !linux && !windows

package device

// enableSeize is a no-op stub for build targets KeyForge does not
// recognize. New OS support starts here: copy this file to seize_<os>.go
// with the appropriate build tag and replace the body with the real
// implementation.
func enableSeize() error {
	return ErrSeizeNotImplemented
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: false,
		Note:      "unsupported platform — no seize implementation",
	}
}
