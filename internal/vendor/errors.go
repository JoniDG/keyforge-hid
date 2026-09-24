package vendor

import "errors"

// Sentinel errors returned by the vendor package. Callers can distinguish
// between failure modes with errors.Is.
var (
	ErrOpen            = errors.New("vendor: open failed")
	ErrWrite           = errors.New("vendor: write failed")
	ErrRead            = errors.New("vendor: read failed")
	ErrClose           = errors.New("vendor: close failed")
	ErrNoAck           = errors.New("vendor: device did not acknowledge the command")
	ErrInvalidArgument = errors.New("vendor: invalid argument")
)
