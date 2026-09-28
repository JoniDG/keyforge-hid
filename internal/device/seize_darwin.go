//go:build darwin

package device

import "github.com/sstallion/go-hid"

// setSeize accepts both modes on darwin; the Opener applies the one
// requested right before each open (see applyOpenMode).
func setSeize(bool) error { return nil }

// applyOpenMode sets hidapi's process-wide open flag: seize maps to
// kIOHIDOptionsTypeSeizeDevice, shared to kIOHIDOptionsTypeNone.
//
// It must run after hid.Init and under hidapiMu: hid_init resets the
// flag to seize whenever it has to create the HID manager ("Backward
// compatibility" in hid_darwin.c), which happens on the first init
// and again after every hid.Exit. Setting the flag earlier is undone
// by the next implicit init inside hid.OpenPath, and on macOS 27 a
// seize open of a keyboard TLC without root fails with 0xE00002C1
// (privilege violation).
//
// The option is what decides OS routing: with a shared open the OS
// keeps delivering the keypad's keystrokes to other apps (verified on
// macOS 27), with a seize open it does not.
func applyOpenMode(seize bool) {
	hid.SetOpenExclusive(seize)
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: true,
		Note:      "darwin: seize opens with kIOHIDOptionsTypeSeizeDevice (needs root for keyboard TLCs); shared opens with kIOHIDOptionsTypeNone",
	}
}
