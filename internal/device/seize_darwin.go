//go:build darwin

package device

import "github.com/sstallion/go-hid"

// setSeize toggles hidapi's process-wide exclusive-open flag. With
// enabled=true, every subsequent hid.OpenPath requests
// kIOHIDOptionsTypeSeizeDevice from IOKit so the OS HID services stop
// delivering those reports to other apps. With enabled=false, opens
// use kIOHIDOptionsTypeNone and the OS keeps receiving its copy.
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
		Note:      "darwin: hidapi opens with kIOHIDOptionsTypeSeizeDevice when seize is enabled; shared mode requires an explicit SetSeize(false) call",
	}
}
