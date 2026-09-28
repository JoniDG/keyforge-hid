//go:build !darwin && !linux && !windows

package device

// setSeize is a no-op stub for build targets KeyForge does not
// recognize. New OS support starts here: copy this file to
// seize_<os>.go with the appropriate build tag and replace the body
// with the real implementation. SetSeize(false) succeeds because
// every unknown platform is assumed to default to shared mode.
func setSeize(enabled bool) error {
	if !enabled {
		return nil
	}
	return ErrSeizeNotImplemented
}

// applyOpenMode is a no-op: hidapi exposes no open-mode flag here.
func applyOpenMode(bool) {}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: false,
		Note:      "unsupported platform — no seize implementation",
	}
}
