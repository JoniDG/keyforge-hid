package device

import "errors"

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

// SetSeize requests the desired exclusive-access state for subsequent
// HID device opens. enabled=true asks the platform to take exclusive
// control (the OS will not receive the device's reports in parallel);
// enabled=false asks for shared access (the OS gets a copy of every
// report alongside our process).
//
// The call is idempotent and process-scoped: invoking it once before
// opening any device is enough, and any later opens inherit the most
// recently set state.
//
// On darwin the call always succeeds. Note that hidapi on darwin
// initializes its global option to "seize" for backward compatibility
// the first time hid_init runs (which happens automatically on
// Enumerate or Open), so callers that want shared mode MUST call
// SetSeize(false) explicitly — omitting the call leaves seize on.
//
// On stubbed platforms, SetSeize(true) returns ErrSeizeNotImplemented
// and SetSeize(false) returns nil (because shared is the platform
// default already).
func SetSeize(enabled bool) error { return setSeize(enabled) }

// PlatformSeizeSupport returns the seize status of the current build
// for surfacing in logs, banners or UI.
func PlatformSeizeSupport() SeizeSupport { return platformSeizeSupport() }
