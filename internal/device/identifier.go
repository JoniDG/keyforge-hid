package device

// IdentifiedDevice groups every Info reported by enumeration that
// shares a (VendorID, ProductID) pair, plus the registry metadata when
// the device is recognized.
//
// When Recognized is false, Known is the zero KnownDevice and callers
// should treat the device as anonymous hardware.
type IdentifiedDevice struct {
	VendorID   uint16
	ProductID  uint16
	Recognized bool
	Known      KnownDevice
	Interfaces []Info
}

// Identifier groups enumerated Info entries by (VendorID, ProductID)
// and tags each group with registry metadata when available.
type Identifier interface {
	// Identify returns one IdentifiedDevice per distinct (VID, PID)
	// pair found in infos, in the order each pair was first seen.
	// Interfaces inside each device preserve the order from infos.
	Identify(infos []Info) []IdentifiedDevice
}

// NewIdentifier returns an Identifier backed by the given Registry.
func NewIdentifier(registry *Registry) Identifier {
	return &registryIdentifier{registry: registry}
}

type registryIdentifier struct {
	registry *Registry
}

func (i *registryIdentifier) Identify(infos []Info) []IdentifiedDevice {
	if len(infos) == 0 {
		return []IdentifiedDevice{}
	}

	indexByKey := make(map[registryKey]int, len(infos))
	devices := make([]IdentifiedDevice, 0, len(infos))

	for _, info := range infos {
		key := registryKey{vendor: info.VendorID, product: info.ProductID}
		if idx, seen := indexByKey[key]; seen {
			devices[idx].Interfaces = append(devices[idx].Interfaces, info)
			continue
		}

		known, recognized := i.registry.Lookup(info.VendorID, info.ProductID)
		devices = append(devices, IdentifiedDevice{
			VendorID:   info.VendorID,
			ProductID:  info.ProductID,
			Recognized: recognized,
			Known:      known,
			Interfaces: []Info{info},
		})
		indexByKey[key] = len(devices) - 1
	}

	return devices
}
