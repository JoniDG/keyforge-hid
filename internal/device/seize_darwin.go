//go:build darwin

package device

import "github.com/sstallion/go-hid"

// setSeize toggles hidapi's process-wide open flag. With enabled=true,
// every subsequent hid.OpenPath asks IOKit for
// kIOHIDOptionsTypeSeizeDevice; with enabled=false, it uses
// kIOHIDOptionsTypeNone.
//
// Empirical caveat on macOS Sequoia (verified against the reference
// keypad's boot keyboard and Consumer Control TLCs): IOHIDDeviceOpen
// appears to claim the TLC's event routing regardless of which
// options bit is passed. Both -shared and the default seize path
// observed the same outcome — the OS HID services did not deliver
// volume/mute/keyboard events to other consumers in either mode. The
// effective gate against parallel OS delivery is opening the TLC at
// all, not the seize option.
//
// We still call hid.SetOpenExclusive(true) on the seize path so the
// intent is recorded in code and so future macOS releases that honor
// the option distinctly do the right thing.
//
// hidapi auto-initialises its global to seize on darwin (see hid_init
// in hid_darwin.c — "Backward compatibility"). Callers that want
// shared mode MUST call this with false explicitly; merely not calling
// it leaves the flag at the seize default.
func setSeize(enabled bool) error {
	hid.SetOpenExclusive(enabled)
	return nil
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: true,
		Note:      "darwin: requests kIOHIDOptionsTypeSeizeDevice via hidapi; macOS claims TLC routing on IOHIDDeviceOpen regardless, so this flag is documented intent rather than the observable gate",
	}
}
