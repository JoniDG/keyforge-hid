package device

// KnownDevice declares a piece of hardware the registry recognizes.
// VendorID and ProductID together identify the device; Name is a
// human-readable label for UI and logging.
type KnownDevice struct {
	VendorID  uint16
	ProductID uint16
	Name      string
}

// SideKeyboardKeypad is the 10-key + 2-encoder keypad that ships under
// the "SDINNOVATION SIDE-KEYBOARD" name (the reference hardware used
// during early KeyForge development).
var SideKeyboardKeypad = KnownDevice{
	VendorID:  0x6D82,
	ProductID: 0xDC83,
	Name:      "SDINNOVATION SIDE-KEYBOARD",
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
