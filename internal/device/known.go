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
	// Codes translates the codes the device emits into Controls ids.
	Codes Codes
	// Vendor locates the device's configuration channel. The zero value
	// means the device has none KeyForge can drive.
	Vendor VendorInterface
}

// Codes maps what a device emits to the catalog inputs those codes stand
// for. The event mappers read it, so an input id need not be derived from
// the code behind it: the layout can move an input to another code
// without renaming the input that bindings are keyed on.
type Codes struct {
	// Keys maps HID keyboard usages to key input ids. A usage left out is
	// reported under its generic id, key_0x<usage>, which can coincide
	// with a mapped id: a keypad still on F13–F22 (usage 0x68…) reports
	// the same ids until it is provisioned again.
	Keys map[byte]string
	// Encoders maps Consumer Control usages to the encoder input and
	// action they stand for. A usage left out is ignored.
	Encoders map[uint16]InputCode
}

// InputCode is the catalog input and action one code stands for.
type InputCode struct {
	ID     string
	Action protocol.InputAction
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
	// LEDs lists the ids (from Controls) of the inputs with an
	// addressable LED, indexed by LED number.
	LEDs []string
	// Gamma is the exponent vendor.RGB.Corrected applies to colors
	// before they reach the LEDs; 0 writes them unchanged.
	Gamma float64
	// Layout is what Source.Provision writes (one entry per slot) so
	// every physical input emits a distinct code that Codes maps to
	// Controls. PlatformLayout picks it everywhere but Windows.
	Layout []vendor.Slot
	// WindowsLayout replaces Layout on Windows, where keys must have a
	// scan code for Raw Input to report them (see PlatformLayout).
	WindowsLayout []vendor.Slot
	// Factory is the out-of-the-box slot content, written back slot by
	// slot to undo Layout without the firmware's factory-reset command.
	Factory []vendor.Slot
}

// key builds a catalog entry for a key with an LED at (x, y) in the
// device's canonical orientation, in key units.
func key(id string, x, y float64) protocol.Input {
	rgb := true
	return protocol.Input{Id: id, Kind: protocol.InputKindKey, Rgb: &rgb, Layout: &protocol.InputLayout{X: x, Y: y}}
}

// encoder builds a catalog entry for an encoder at (x, y) in the device's
// canonical orientation, in key units.
func encoder(id string, x, y float64) protocol.Input {
	return protocol.Input{Id: id, Kind: protocol.InputKindEncoder, Layout: &protocol.InputLayout{X: x, Y: y}}
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
// volume/mute usages, so the inputs are indistinguishable. Layout gives
// every input its own code, picked from usages the HID Usage Tables leave
// reserved so no OS acts on them: the keypad does nothing unless KeyForge
// reads it. Controls describes the device after Source.Provision has
// written Layout; see docs/hid-device-keyforge-keypad.md for the slot
// map and the canonical orientation the layout is given in (horizontal,
// encoders on the right, keys 1–5 on the top row). Entries carry no label: the GUI numbers them
// by position in whatever rotation the user picks.
var SideKeyboardKeypad = KnownDevice{
	VendorID:  0x6D82,
	ProductID: 0xDC83,
	Name:      "SDINNOVATION SIDE-KEYBOARD",
	Inputs: []KnownInput{
		{Role: RoleKeyboard, UsagePage: 0x0001, Usage: 0x0006},
		{Role: RoleEncoder, UsagePage: 0x000c, Usage: 0x0001},
	},
	Controls: []protocol.Input{
		key("key_0x68", 0, 0), key("key_0x69", 1, 0), key("key_0x6a", 2, 0), key("key_0x6b", 3, 0), key("key_0x6c", 4, 0),
		key("key_0x6d", 0, 1), key("key_0x6e", 1, 1), key("key_0x6f", 2, 1), key("key_0x70", 3, 1), key("key_0x71", 4, 1),
		// Half a key of gap between the keys and the encoder column.
		encoder("encoder_0", 5.5, 0),
		encoder("encoder_1", 5.5, 1),
	},
	Codes: Codes{
		Keys: map[byte]string{
			0xA5: "key_0x68", 0xA6: "key_0x69", 0xA7: "key_0x6a", 0xA8: "key_0x6b", 0xA9: "key_0x6c",
			0xAA: "key_0x6d", 0xAB: "key_0x6e", 0xAC: "key_0x6f", 0xAD: "key_0x70", 0xAE: "key_0x71",
		},
		Encoders: map[uint16]InputCode{
			0x0007: {ID: "encoder_0", Action: protocol.InputActionClick},
			0x0008: {ID: "encoder_0", Action: protocol.InputActionRotateCw},
			0x0009: {ID: "encoder_0", Action: protocol.InputActionRotateCcw},
			0x000A: {ID: "encoder_1", Action: protocol.InputActionClick},
			0x000B: {ID: "encoder_1", Action: protocol.InputActionRotateCw},
			0x000C: {ID: "encoder_1", Action: protocol.InputActionRotateCcw},
			// Factory mute/volume: until the keypad is provisioned both
			// encoders emit these and collapse into encoder_0.
			0x00E2: {ID: "encoder_0", Action: protocol.InputActionClick},
			0x00E9: {ID: "encoder_0", Action: protocol.InputActionRotateCw},
			0x00EA: {ID: "encoder_0", Action: protocol.InputActionRotateCcw},
		},
	},
	// Slots 0–21 back the ten keys and the two encoders' click/CW/CCW;
	// the firmware answers for slots past 21 but nothing is wired to
	// them. Only the ten keys have LEDs, numbered like their slots.
	Vendor: VendorInterface{
		UsagePage: 0xFF00,
		Usage:     0x0002,
		Slots:     22,
		LEDs: []string{
			"key_0x68", "key_0x69", "key_0x6a", "key_0x6b", "key_0x6c",
			"key_0x6d", "key_0x6e", "key_0x6f", "key_0x70", "key_0x71",
		},
		// Measured by eye against a screen on the reference keypad: 2.8
		// still washed pastels out and 4.5 overshot (§4.3).
		Gamma: 3.5,
		// Keyboard usages 0xA5–0xAF and Consumer usages 0x07–0x1F are
		// reserved: macOS ignores them (checked on hardware) and Linux maps
		// them to KEY_UNKNOWN (kernel source, not tried on a host yet), yet
		// they reach KeyForge in the raw reports. Keys stay on the keyboard report, which carries up to
		// six held keys; the Consumer report carries one usage, so two
		// keys held there could not be told apart on release.
		Layout: sideKeyboardSlots(
			[10]vendor.Slot{
				vendor.KeyboardSlot(0, 0xA5), vendor.KeyboardSlot(0, 0xA6),
				vendor.KeyboardSlot(0, 0xA7), vendor.KeyboardSlot(0, 0xA8),
				vendor.KeyboardSlot(0, 0xA9), vendor.KeyboardSlot(0, 0xAA),
				vendor.KeyboardSlot(0, 0xAB), vendor.KeyboardSlot(0, 0xAC),
				vendor.KeyboardSlot(0, 0xAD), vendor.KeyboardSlot(0, 0xAE),
			},
			[3]vendor.Slot{vendor.ConsumerSlot(0x0007), vendor.ConsumerSlot(0x0008), vendor.ConsumerSlot(0x0009)},
			[3]vendor.Slot{vendor.ConsumerSlot(0x000A), vendor.ConsumerSlot(0x000B), vendor.ConsumerSlot(0x000C)},
		),
		// F13–F22 have scan codes, so Raw Input reports them, and Windows
		// binds no action to them. They keep their generic ids. On macOS
		// and Linux they do trigger system actions, hence Layout there.
		WindowsLayout: sideKeyboardSlots(
			[10]vendor.Slot{
				vendor.KeyboardSlot(0, 0x68), vendor.KeyboardSlot(0, 0x69),
				vendor.KeyboardSlot(0, 0x6A), vendor.KeyboardSlot(0, 0x6B),
				vendor.KeyboardSlot(0, 0x6C), vendor.KeyboardSlot(0, 0x6D),
				vendor.KeyboardSlot(0, 0x6E), vendor.KeyboardSlot(0, 0x6F),
				vendor.KeyboardSlot(0, 0x70), vendor.KeyboardSlot(0, 0x71),
			},
			[3]vendor.Slot{vendor.ConsumerSlot(0x0007), vendor.ConsumerSlot(0x0008), vendor.ConsumerSlot(0x0009)},
			[3]vendor.Slot{vendor.ConsumerSlot(0x000A), vendor.ConsumerSlot(0x000B), vendor.ConsumerSlot(0x000C)},
		),
		Factory: sideKeyboardSlots(
			[10]vendor.Slot{
				ctrlA, ctrlA, ctrlA, ctrlA, ctrlA,
				ctrlA, ctrlA, ctrlA, ctrlA, ctrlA,
			},
			factoryEncoder,
			factoryEncoder,
		),
	},
}

// PlatformLayout returns the layout Source.Provision writes on the
// current OS.
func (v VendorInterface) PlatformLayout() []vendor.Slot {
	return platformLayout(v)
}

var (
	ctrlA          = vendor.KeyboardSlot(0x01, 0x04)
	factoryEncoder = [3]vendor.Slot{vendor.ConsumerSlot(0x00E2), vendor.ConsumerSlot(0x00E9), vendor.ConsumerSlot(0x00EA)}
)

// sideKeyboardSlots assembles the keypad's 22 slots: keys in slots 0–9,
// slots 10–15 disabled, encoder 1 (click/CW/CCW) in 16–18 and encoder 2
// in 19–21.
func sideKeyboardSlots(keys [10]vendor.Slot, encoder1, encoder2 [3]vendor.Slot) []vendor.Slot {
	slots := make([]vendor.Slot, 0, 22)
	slots = append(slots, keys[:]...)
	for range 6 {
		slots = append(slots, vendor.DisabledSlot())
	}
	slots = append(slots, encoder1[:]...)
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
