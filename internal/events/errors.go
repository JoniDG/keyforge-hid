package events

import "errors"

// ErrShortReport is returned by Mapper.Map when the input report is
// shorter than the protocol expects (8 bytes for HID boot keyboard).
var ErrShortReport = errors.New("events: input report too short")
