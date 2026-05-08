// Package events translates raw HID input reports into typed InputEvent
// values matching the keyforge-protocol contract.
package events

import (
	"fmt"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// DeviceIDFor builds a protocol.DeviceID for the given USB identifiers,
// matching the schema regex `^VID_[0-9A-Fa-f]{4}_PID_[0-9A-Fa-f]{4}(_.+)?$`.
// When serial is empty, the trailing segment is omitted.
func DeviceIDFor(vendorID, productID uint16, serial string) protocol.DeviceID {
	if serial == "" {
		return protocol.DeviceID(fmt.Sprintf("VID_%04X_PID_%04X", vendorID, productID))
	}
	return protocol.DeviceID(fmt.Sprintf("VID_%04X_PID_%04X_%s", vendorID, productID, serial))
}
