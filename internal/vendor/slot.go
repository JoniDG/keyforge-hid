package vendor

import "fmt"

// SlotType tells the firmware how to interpret a slot's three code bytes.
type SlotType byte

// Slot types observed on the reference keypad. The vendor configurator
// knows more (mouse move, macro, open website, custom combination); they
// are left out until they are verified on hardware.
const (
	SlotDisabled SlotType = 0x13
	SlotKeyboard SlotType = 0x20
	SlotConsumer SlotType = 0x30
)

// Slot is what one physical input emits, as stored by the firmware.
type Slot struct {
	Type  SlotType
	Codes [3]byte
}

// KeyboardSlot emits a keystroke: mods is the HID modifier bitmap
// (bit0 LCtrl … bit7 RGUI) and usage the HID Keyboard/Keypad usage.
func KeyboardSlot(mods, usage byte) Slot {
	return Slot{Type: SlotKeyboard, Codes: [3]byte{mods, usage, 0}}
}

// ConsumerSlot emits a Consumer Control usage (volume, media keys, …).
func ConsumerSlot(usage uint16) Slot {
	return Slot{Type: SlotConsumer, Codes: [3]byte{byte(usage), byte(usage >> 8), 0}}
}

// DisabledSlot makes the input emit nothing.
func DisabledSlot() Slot {
	return Slot{Type: SlotDisabled}
}

func (s Slot) String() string {
	switch s.Type {
	case SlotDisabled:
		return "disabled"
	case SlotKeyboard:
		return fmt.Sprintf("keyboard mods=0x%02x usage=0x%02x", s.Codes[0], s.Codes[1])
	case SlotConsumer:
		return fmt.Sprintf("consumer usage=0x%04x", uint16(s.Codes[0])|uint16(s.Codes[1])<<8)
	default:
		return fmt.Sprintf("type=0x%02x codes=%02x %02x %02x", byte(s.Type), s.Codes[0], s.Codes[1], s.Codes[2])
	}
}
