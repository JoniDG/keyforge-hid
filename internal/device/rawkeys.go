package device

import (
	"bytes"
	"strings"
)

// HID usage of a keyboard top-level collection.
const (
	usagePageGenericDesktop uint16 = 0x0001
	usageKeyboard           uint16 = 0x0006
)

// Windows virtual-key codes of F13 and F24.
const (
	vkF13 uint16 = 0x7C
	vkF24 uint16 = 0x87
)

// usageF13 is the HID keyboard usage of F13; F14…F24 follow it.
const usageF13 byte = 0x68

// bootReportLength and bootReportKeys describe the HID boot keyboard
// report: a modifier byte, a reserved byte and six key slots.
const (
	bootReportLength = 8
	bootReportKeys   = 6
)

// usageForVKey returns the HID keyboard usage of a Windows virtual-key
// code. Only F13–F24 are translated: they are the keys the Windows
// layout uses, and Raw Input reports keys by virtual key, not by usage.
// The virtual key comes from the active keyboard layout; F13–F24 are the
// same in the common ones, and the scan code (MakeCode) is the fallback
// if a layout proves otherwise.
func usageForVKey(vk uint16) (byte, bool) {
	if vk < vkF13 || vk > vkF24 {
		return 0, false
	}
	return usageF13 + byte(vk-vkF13), true // #nosec G115 -- vk-vkF13 <= 11
}

// heldKeys rebuilds boot keyboard reports from individual key
// transitions, so keys read through Raw Input go through the same
// KeyboardMapper as reports read through hidapi.
type heldKeys struct {
	usages []byte
}

// update records usage going down or up and returns the boot report for
// the new state. changed is false when nothing changed (a key repeat,
// the release of a key that was not down, or a seventh held key, which
// the report has no slot for).
func (h *heldKeys) update(usage byte, down bool) (report []byte, changed bool) {
	i := bytes.IndexByte(h.usages, usage)
	switch {
	case down && i < 0 && len(h.usages) < bootReportKeys:
		h.usages = append(h.usages, usage)
	case !down && i >= 0:
		h.usages = append(h.usages[:i], h.usages[i+1:]...)
	default:
		return nil, false
	}
	report = make([]byte, bootReportLength)
	copy(report[2:], h.usages)
	return report, true
}

// sameInterface reports whether a Raw Input device name and a hidapi
// path point at the same HID collection. Windows names both after the
// device instance and only the trailing interface class GUID differs
// (keyboard vs HID), e.g.
//
//	\\?\HID#VID_6D82&PID_DC83&MI_01#7&1a2b&0&0000#{884b96c3-…}  (Raw Input)
//	\\?\HID#VID_6D82&PID_DC83&MI_01#7&1a2b&0&0000#{4d1e55b2-…}  (hidapi)
func sameInterface(hidPath, rawName string) bool {
	trim := func(s string) string {
		if i := strings.LastIndexByte(s, '#'); i >= 0 {
			return s[:i]
		}
		return s
	}
	return trim(hidPath) != "" && strings.EqualFold(trim(hidPath), trim(rawName))
}
