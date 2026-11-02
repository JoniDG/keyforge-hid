package device

import "errors"

// Sentinel errors returned by the device package. Callers can distinguish
// between failure modes with errors.Is.
var (
	ErrInit      = errors.New("device: hidapi initialization failed")
	ErrEnumerate = errors.New("device: hidapi enumeration failed")
	ErrCleanup   = errors.New("device: hidapi cleanup failed")
	ErrOpen      = errors.New("device: open failed")
	ErrRead      = errors.New("device: read failed")
	ErrClose     = errors.New("device: close failed")
	// ErrAccessDenied joins ErrOpen or ErrRead when the OS refuses this
	// process access to the interface (Windows holds mouse and keyboard
	// collections exclusively). It is permanent: reopening the same
	// interface fails again until the device changes.
	ErrAccessDenied = errors.New("device: access denied")
)
