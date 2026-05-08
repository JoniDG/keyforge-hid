package device

import "errors"

// Sentinel errors returned by the device package. Callers can distinguish
// between failure modes with errors.Is.
var (
	ErrInit      = errors.New("device: hidapi initialization failed")
	ErrEnumerate = errors.New("device: hidapi enumeration failed")
	ErrCleanup   = errors.New("device: hidapi cleanup failed")
)
