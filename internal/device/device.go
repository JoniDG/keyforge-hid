// Package device handles HID device enumeration, identification (VID/PID),
// and lifecycle (open/close). It does not interpret input reports —
// see the events package for that.
package device

// Info describes a HID device discovered during enumeration. It mirrors
// the fields exposed by hidapi's hid_device_info but stays decoupled from
// any specific HID library so consumers can depend on this type alone.
type Info struct {
	Path         string
	VendorID     uint16
	ProductID    uint16
	Release      uint16
	Manufacturer string
	Product      string
	Serial       string
	UsagePage    uint16
	Usage        uint16
	Interface    int
	BusType      BusType
}

// BusType identifies the underlying transport for a HID device.
type BusType uint8

// Bus types reported by hidapi.
const (
	BusUnknown BusType = iota
	BusUSB
	BusBluetooth
	BusI2C
	BusSPI
)

// String returns a human-readable label for the bus type.
func (b BusType) String() string {
	switch b {
	case BusUSB:
		return "USB"
	case BusBluetooth:
		return "Bluetooth"
	case BusI2C:
		return "I2C"
	case BusSPI:
		return "SPI"
	default:
		return "Unknown"
	}
}
