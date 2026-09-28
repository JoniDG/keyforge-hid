package device

import (
	"errors"
	"sync"
	"sync/atomic"
)

// SeizeSupport describes a platform's ability to take exclusive
// control of a HID device's input stream, so the OS does not receive
// its reports in parallel with the application. Use it to gate UI
// messaging and decide whether to surface a warning to the user.
type SeizeSupport struct {
	// Supported is true when the current build can actually take
	// exclusive control. When false, SetSeize(true) returns
	// ErrSeizeNotImplemented; SetSeize(false) is always a no-op
	// because the platform's default is already shared.
	Supported bool
	// Note is a one-line human description of the platform's seize
	// status (intent on supported platforms, planned approach on
	// stubbed ones). Always non-empty.
	Note string
}

// ErrSeizeNotImplemented is returned by SetSeize(true) on builds whose
// platform implementation is still a stub. Callers can treat it as a
// warning and fall back to shared mode when seize is optional.
var ErrSeizeNotImplemented = errors.New("device: seize not implemented on this platform")

// hidapiMu serializes hidapi's lifecycle calls (Init/Exit in the
// enumerator) with device opens. On darwin, hid_exit drops the HID
// manager and the next hid_init resets the process-wide open mode to
// seize, so an Exit landing between applying the mode and OpenPath
// would silently turn a shared open into a seize. OpenPath cannot be
// cancelled, so an open stuck inside hidapi also blocks enumeration.
var hidapiMu sync.Mutex

// seizeRequested is the open mode SetSeize last accepted. Openers read
// it right before each open; the zero value is shared.
var seizeRequested atomic.Bool

// SetSeize requests the desired exclusive-access state for subsequent
// HID device opens. enabled=true asks the platform to take exclusive
// control (the OS will not receive the device's reports in parallel);
// enabled=false asks for shared access (the OS gets a copy of every
// report alongside our process).
//
// The call is idempotent and process-scoped: devices opened through
// the Opener after the call use the most recently accepted state, and
// shared is the state until SetSeize(true) succeeds. Devices already
// open keep the mode they were opened with.
//
// On stubbed platforms, SetSeize(true) returns ErrSeizeNotImplemented
// and leaves the state unchanged; SetSeize(false) returns nil (because
// shared is the platform default already).
func SetSeize(enabled bool) error {
	if err := setSeize(enabled); err != nil {
		return err
	}
	seizeRequested.Store(enabled)
	return nil
}

// PlatformSeizeSupport returns the seize status of the current build
// for surfacing in logs, banners or UI.
func PlatformSeizeSupport() SeizeSupport { return platformSeizeSupport() }
