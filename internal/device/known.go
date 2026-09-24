package device

import (
	"github.com/JoniDG/keyforge-hid/internal/vendor"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// InputRole names the semantic role of a HID interface exposed by a
// KnownDevice. Consumers route reports to the right decoder based on
// the role tag returned by IdentifiedDevice.Inputs.
type InputRole string

// Roles understood by the registry. Add new ones as device classes grow.
const (
	RoleKeyboard InputRole = "keyboard"
	RoleEncoder  InputRole = "encoder"
)

// KnownInput declares one input interface a KnownDevice exposes,
// identified by its HID (UsagePage, Usage) pair plus the semantic Role
// that input plays. Multiple interfaces on the same device may share a
// (UsagePage, Usage) pair (the Temu keypad exposes two such for boot
// vs. non-boot keyboard reports); every match is returned by Inputs.
type KnownInput struct {
	Role      InputRole
	UsagePage uint16
	Usage     uint16
}

// KnownDevice declares a piece of hardware the registry recognizes.
// VendorID and ProductID together identify the device; Name is a
// human-readable label for UI and logging; Inputs describes the HID
// interfaces the device exposes that KeyForge knows how to decode;
// Controls is the curated catalog of logical inputs a user can bind
// against (see Controls).
type KnownDevice struct {
	VendorID  uint16
	ProductID uint16
	Name      string
	Inputs    []KnownInput
	// Controls is the static, curated list of logical inputs this device
	// exposes to consumers — the bindable surface keyforge-core persists
	// and the GUI renders. It is authored per device rather than derived
	// from the live event stream: under factory firmware several physical
	// inputs collapse onto a single logical id (all ten keys emit the same
	// chord, both encoders emit the same Consumer Control), so the catalog
	// reflects what a user can meaningfully bind, not every interface.
	// Each entry's Id matches the input_id the event mappers emit.
	Controls []protocol.Input
	// Vendor locates the device's configuration channel. The zero value
	// means the device has none KeyForge can drive.
	Vendor VendorInterface
}

// VendorInterface declares a device's vendor-specific configuration
// interface and the index ranges verified on hardware (see
// docs/hid-device-keyforge-keypad.md §4.3 for the slot and LED maps).
type VendorInterface struct {
	UsagePage uint16
	Usage     uint16
	// Slots is the total number of input slots wired to the device's
	// physical inputs.
	Slots int
	// LEDs is the number of keys with an addressable LED.
	LEDs int
	// Layout is what Source.Provision writes (one entry per slot) so
	// every physical input emits a distinct code matching Controls.
	Layout []vendor.Slot
	// Factory is the out-of-the-box slot content, written back slot by
	// slot to undo Layout without the firmware's factory-reset command.
	Factory []vendor.Slot
}

// control builds a logical input catalog entry with a non-empty label.
func control(id string, kind protocol.InputKind, label string) protocol.Input {
	return protocol.Input{Id: id, Kind: kind, Label: &label}
}

// SideKeyboardKeypad is the 10-key + 2-encoder keypad that ships under
// the "SDINNOVATION SIDE-KEYBOARD" name (the reference hardware used
// during early KeyForge development).
//
// Its inputs split across two HID interfaces:
//   - Boot keyboard (UsagePage=0x0001, Usage=0x0006) for the 10 keys.
//   - Consumer Control (UsagePage=0x000c, Usage=0x0001) for the 2
//     encoders.
//
// Out of the box every key emits Ctrl+A and both encoders emit the same
// volume/mute usages, so the inputs are indistinguishable. Controls
// describes the device after Source.Provision has written Layout; see
// docs/hid-device-keyforge-keypad.md for the slot map and the canonical
// orientation the labels are numbered in (horizontal, encoders on the
// right, keys 1–5 on the top row).
var SideKeyboardKeypad = KnownDevice{
	VendorID:  0x6D82,
	ProductID: 0xDC83,
	Name:      "SDINNOVATION SIDE-KEYBOARD",
	Inputs: []KnownInput{
		{Role: RoleKeyboard, UsagePage: 0x0001, Usage: 0x0006},
		{Role: RoleEncoder, UsagePage: 0x000c, Usage: 0x0001},
	},
	Controls: []protocol.Input{
		control("key_0x68", protocol.InputKindKey, "Key 1"),
		control("key_0x69", protocol.InputKindKey, "Key 2"),
		control("key_0x6a", protocol.InputKindKey, "Key 3"),
		control("key_0x6b", protocol.InputKindKey, "Key 4"),
		control("key_0x6c", protocol.InputKindKey, "Key 5"),
		control("key_0x6d", protocol.InputKindKey, "Key 6"),
		control("key_0x6e", protocol.InputKindKey, "Key 7"),
		control("key_0x6f", protocol.InputKindKey, "Key 8"),
		control("key_0x70", protocol.InputKindKey, "Key 9"),
		control("key_0x71", protocol.InputKindKey, "Key 10"),
		control("encoder_0", protocol.InputKindEncoder, "Encoder 1"),
		control("encoder_1", protocol.InputKindEncoder, "Encoder 2"),
	},
	// Slots 0–21 back the ten keys and the two encoders' click/CW/CCW;
	// the firmware answers for slots past 21 but nothing is wired to
	// them. Only the ten keys have LEDs.
	Vendor: VendorInterface{
		UsagePage: 0xFF00,
		Usage:     0x0002,
		Slots:     22,
		LEDs:      10,
		Layout: sideKeyboardSlots(
			[10]vendor.Slot{
				vendor.KeyboardSlot(0, 0x68), vendor.KeyboardSlot(0, 0x69), // F13, F14
				vendor.KeyboardSlot(0, 0x6A), vendor.KeyboardSlot(0, 0x6B), // F15, F16
				vendor.KeyboardSlot(0, 0x6C), vendor.KeyboardSlot(0, 0x6D), // F17, F18
				vendor.KeyboardSlot(0, 0x6E), vendor.KeyboardSlot(0, 0x6F), // F19, F20
				vendor.KeyboardSlot(0, 0x70), vendor.KeyboardSlot(0, 0x71), // F21, F22
			},
			// Encoder 2 moves to media usages so it is told apart from
			// encoder 1 while still doing something sensible without
			// KeyForge running.
			[3]vendor.Slot{vendor.ConsumerSlot(0x00CD), vendor.ConsumerSlot(0x00B5), vendor.ConsumerSlot(0x00B6)},
		),
		Factory: sideKeyboardSlots(
			[10]vendor.Slot{
				ctrlA, ctrlA, ctrlA, ctrlA, ctrlA,
				ctrlA, ctrlA, ctrlA, ctrlA, ctrlA,
			},
			[3]vendor.Slot{vendor.ConsumerSlot(0x00E2), vendor.ConsumerSlot(0x00E9), vendor.ConsumerSlot(0x00EA)},
		),
	},
}

var ctrlA = vendor.KeyboardSlot(0x01, 0x04)

// sideKeyboardSlots assembles the keypad's 22 slots: keys in slots 0–9,
// slots 10–15 disabled, encoder 1 (click/CW/CCW) in 16–18 with the
// factory volume usages it keeps in every layout, and encoder 2 in 19–21.
func sideKeyboardSlots(keys [10]vendor.Slot, encoder2 [3]vendor.Slot) []vendor.Slot {
	slots := make([]vendor.Slot, 0, 22)
	slots = append(slots, keys[:]...)
	for range 6 {
		slots = append(slots, vendor.DisabledSlot())
	}
	slots = append(slots, vendor.ConsumerSlot(0x00E2), vendor.ConsumerSlot(0x00E9), vendor.ConsumerSlot(0x00EA))
	return append(slots, encoder2[:]...)
}

// Registry is an immutable lookup of devices keyed by (VendorID, ProductID).
type Registry struct {
	byKey map[registryKey]KnownDevice
}

type registryKey struct {
	vendor  uint16
	product uint16
}

// NewRegistry builds a Registry from the given KnownDevice list. When
// duplicate (VendorID, ProductID) pairs are passed, the last entry wins
// so callers can override defaults by appending.
func NewRegistry(devices ...KnownDevice) *Registry {
	byKey := make(map[registryKey]KnownDevice, len(devices))
	for _, d := range devices {
		byKey[registryKey{vendor: d.VendorID, product: d.ProductID}] = d
	}
	return &Registry{byKey: byKey}
}

// DefaultRegistry returns a Registry seeded with every device KeyForge
// ships with built-in support for.
func DefaultRegistry() *Registry {
	return NewRegistry(SideKeyboardKeypad)
}

// Lookup returns the KnownDevice registered for the given VendorID and
// ProductID. The boolean is false when no match exists.
func (r *Registry) Lookup(vendor, product uint16) (KnownDevice, bool) {
	d, ok := r.byKey[registryKey{vendor: vendor, product: product}]
	return d, ok
}
