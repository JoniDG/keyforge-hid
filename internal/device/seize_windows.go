//go:build windows

package device

// setSeize on Windows rejects seize: the OS already holds the keypad's
// keyboard and mouse collections exclusively, and keeping it from
// acting on the Consumer collection takes a kernel-mode filter driver.
// It is not needed either: the provisioned layout
// (VendorInterface.WindowsLayout) emits only reserved Consumer usages,
// which Windows does not act on.
//
// SetSeize(false) is a no-op: the HID class driver already shares the
// collections KeyForge reads.
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
		Note:      "windows: not supported (not needed: the provisioned layout emits codes Windows does not act on)",
	}
}
