package events

import (
	"fmt"
	"time"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// encoderReportLength is the fixed size of a Consumer Control report
// from the reference keypad: 1 report-ID byte plus a 16-bit
// little-endian usage code in bytes 1-2.
const encoderReportLength = 3

// consumerControlReportID is the report-ID prefix that distinguishes
// Consumer Control reports from other Top-Level Collections sharing
// the same physical USB interface on the reference keypad.
const consumerControlReportID byte = 0x03

// encoderInput is the logical encoder and action a Consumer Control
// usage stands for.
type encoderInput struct {
	id     string
	action protocol.InputAction
}

// encoderUsages maps the HID Consumer Control usages (USB HID Usage
// Tables, Consumer Page 0x0c) the reference keypad's encoders emit.
// Encoder 1 keeps the factory usages; the KeyForge layout written by
// Source.Provision moves encoder 2 to media usages so the two can be
// told apart. Under the factory mapping both encoders emit the encoder 1
// usages and collapse into encoder_0.
var encoderUsages = map[uint16]encoderInput{
	0x00E2: {id: "encoder_0", action: protocol.InputActionClick},     // Mute
	0x00E9: {id: "encoder_0", action: protocol.InputActionRotateCw},  // Volume Increment
	0x00EA: {id: "encoder_0", action: protocol.InputActionRotateCcw}, // Volume Decrement
	0x00CD: {id: "encoder_1", action: protocol.InputActionClick},     // Play/Pause
	0x00B5: {id: "encoder_1", action: protocol.InputActionRotateCw},  // Scan Next Track
	0x00B6: {id: "encoder_1", action: protocol.InputActionRotateCcw}, // Scan Previous Track
}

// EncoderMapper translates Consumer Control reports from a keypad's
// encoder interface into protocol.InputEvent values. It is stateless
// across reports (unlike KeyboardMapper): each detent of rotation and
// each click are delivered by the firmware as one press+release pair,
// and the mapper emits exactly one event per press. The release pair
// has usage=0 and falls through the "unknown usage" path silently.
type EncoderMapper struct {
	deviceID protocol.DeviceID
	clock    func() time.Time
}

// EncoderOption configures an EncoderMapper at construction time.
type EncoderOption func(*EncoderMapper)

// WithEncoderClock overrides the time source the mapper uses to stamp
// events. Intended for tests that need deterministic timestamps.
func WithEncoderClock(clock func() time.Time) EncoderOption {
	return func(m *EncoderMapper) { m.clock = clock }
}

// NewEncoderMapper returns an EncoderMapper that emits InputEvents
// tagged with the given device id.
func NewEncoderMapper(deviceID protocol.DeviceID, opts ...EncoderOption) *EncoderMapper {
	m := &EncoderMapper{
		deviceID: deviceID,
		clock:    time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Map returns the InputEvent (if any) produced by one Consumer
// Control report. The slice is always non-nil and at most one element
// long. Reports shorter than encoderReportLength are rejected with
// ErrShortReport; reports with an unexpected report-ID prefix or an
// unknown usage code return an empty slice and no error so callers
// can drain multiplexed report streams without special-casing.
func (m *EncoderMapper) Map(report []byte) ([]protocol.InputEvent, error) {
	if len(report) < encoderReportLength {
		return nil, fmt.Errorf("events.EncoderMapper.Map (got %d bytes, want %d): %w", len(report), encoderReportLength, ErrShortReport)
	}
	if report[0] != consumerControlReportID {
		return []protocol.InputEvent{}, nil
	}

	usage := uint16(report[1]) | uint16(report[2])<<8
	in, ok := encoderUsages[usage]
	if !ok {
		return []protocol.InputEvent{}, nil
	}

	return []protocol.InputEvent{{
		Action:      in.action,
		DeviceId:    m.deviceID,
		InputId:     in.id,
		Kind:        protocol.InputKindEncoder,
		TimestampMs: int(m.clock().UnixMilli()),
	}}, nil
}
