//go:build windows

package device

// enableSeize on Windows is intentionally a stub. hidapi on Windows
// opens the HID device via the user-mode HID class driver, but
// keyboard / consumer-control TLCs are also surfaced through the
// legacy keyboard stack (WM_KEYDOWN, system audio key dispatch).
// Closing that legacy channel requires the Raw Input API.
//
// Planned implementation when Windows hardware is available for
// testing:
//  1. Resolve the keypad's interface path via SetupAPI.
//  2. RegisterRawInputDevices with RIDEV_NOLEGACY | RIDEV_INPUTSINK on
//     the device's top-level usage(s). RIDEV_NOLEGACY suppresses the
//     synthesized WM_KEYDOWN / WM_CHAR messages and (for keyboard/mouse
//     usages) blocks legacy delivery to other apps.
//  3. Consumer Control (volume / mute) does not respect RIDEV_NOLEGACY
//     fully; the fallback is a kernel-mode HID filter driver. That is
//     out of scope for KeyForge's user-mode core and only matters once
//     the project is hardened for Windows release.
//
// Vendor-specific collections (RGB) bypass the legacy stack and need
// no extra work.
func enableSeize() error {
	return ErrSeizeNotImplemented
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: false,
		Note:      "windows: not implemented (planned: RegisterRawInputDevices with RIDEV_NOLEGACY)",
	}
}
