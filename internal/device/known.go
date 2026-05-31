package device

import "github.com/JoniDG/keyforge-protocol/go/protocol"

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
//     encoders; the factory firmware maps both encoders to Volume
//     Increment/Decrement (and mute on click), so they look like a
//     single virtual encoder until the vendor protocol is reversed
//     in Fase 7.a.
var SideKeyboardKeypad = KnownDevice{
	VendorID:  0x6D82,
	ProductID: 0xDC83,
	Name:      "SDINNOVATION SIDE-KEYBOARD",
	Inputs: []KnownInput{
		{Role: RoleKeyboard, UsagePage: 0x0001, Usage: 0x0006},
		{Role: RoleEncoder, UsagePage: 0x000c, Usage: 0x0001},
	},
	// Factory firmware collapses all ten keys onto the Ctrl+A chord and
	// both encoders onto a single Consumer Control stream, so the bindable
	// catalog stays small until the vendor protocol is reversed (Fase 7.a)
	// and the keys/encoders can be reprogrammed to distinct codes. Pressing
	// any key emits the chord as two events in this order — the Left Ctrl
	// modifier then keycode 0x04 — so both are declared. See
	// docs/hid-device-keyforge-keypad.md.
	Controls: []protocol.Input{
		control("mod_lctrl", protocol.InputKindKey, "Left Ctrl"),
		control("key_0x04", protocol.InputKindKey, "Key"),
		control("encoder_0", protocol.InputKindEncoder, "Encoder"),
	},
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
