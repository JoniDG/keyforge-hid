//go:build darwin

package device

import "github.com/sstallion/go-hid"

// enableSeize sets hidapi's process-wide exclusive-open flag. After
// this call, every hid.OpenPath invocation requests
// kIOHIDOptionsTypeSeizeDevice from IOKit, which prevents the OS HID
// services (keyboard layer, consumer-control routing) from delivering
// those reports to other apps. The flag persists for the lifetime of
// the process and is safe to set multiple times.
func enableSeize() error {
	hid.SetOpenExclusive(true)
	return nil
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: true,
		Note:      "darwin: opens HID devices with kIOHIDOptionsTypeSeizeDevice via hidapi",
	}
}
