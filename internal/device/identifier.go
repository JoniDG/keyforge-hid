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

// MatchedInput pairs an enumerated Info with the semantic Role declared
// for it by the KnownDevice's registry entry.
type MatchedInput struct {
	Info Info
	Role InputRole
}

// Inputs returns every interface of the device that matches one of the
// (UsagePage, Usage) pairs declared in Known.Inputs, tagged with the
// matching Role.
//
// Results are ordered by KnownInput declaration order (outer) and then
// by the original Info order within each match group (inner) so the
// stream is deterministic across kernel re-enumeration. Each Info is
// returned at most once: if two KnownInputs declare the same usage,
// the first one wins.
//
// When the device is not recognized or has no declared inputs, the
// result is an empty (non-nil) slice.
func (d IdentifiedDevice) Inputs() []MatchedInput {
	if len(d.Known.Inputs) == 0 {
		return []MatchedInput{}
	}
	out := make([]MatchedInput, 0, len(d.Interfaces))
	consumed := make(map[int]bool, len(d.Interfaces))
	for _, ki := range d.Known.Inputs {
		for i, info := range d.Interfaces {
			if consumed[i] {
				continue
			}
			if info.UsagePage == ki.UsagePage && info.Usage == ki.Usage {
				out = append(out, MatchedInput{Info: info, Role: ki.Role})
				consumed[i] = true
			}
		}
	}
	return out
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
