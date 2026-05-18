package device

import "errors"

// SeizeSupport describes a platform's ability to take exclusive
// control of a HID device's input stream, so the OS does not receive
// its reports in parallel with the application. Use it to gate UI
// messaging and decide whether to surface a warning to the user.
type SeizeSupport struct {
	// Supported is true when the current build has a working seize
	// implementation. When false, EnableSeize returns
	// ErrSeizeNotImplemented.
	Supported bool
	// Note is a one-line human description of the platform's seize
	// status (intent on supported platforms, planned approach on
	// stubbed ones). Always non-empty.
	Note string
}

// ErrSeizeNotImplemented is returned by EnableSeize on builds whose
// platform implementation is still a stub. Callers can treat it as a
// warning and continue in shared mode when seize is optional.
var ErrSeizeNotImplemented = errors.New("device: seize not implemented on this platform")

// EnableSeize asks the underlying HID stack to open subsequent devices
// in exclusive (seized) mode, preventing the OS from delivering their
// reports to other consumers in parallel. The call is idempotent and
// process-scoped: enabling it once before opening any device is
// enough.
//
// Returns ErrSeizeNotImplemented on platforms whose support is
// stubbed. Callers can decide whether that is fatal or a warning.
func EnableSeize() error { return enableSeize() }

// PlatformSeizeSupport returns the seize status of the current build
// for surfacing in logs, banners or UI.
func PlatformSeizeSupport() SeizeSupport { return platformSeizeSupport() }
