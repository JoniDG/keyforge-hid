package events

import (
	"fmt"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// consumerReportLength is the fixed size of a Consumer Control report
// from the reference keypad: 1 report-ID byte plus a 16-bit
// little-endian usage code in bytes 1-2.
const consumerReportLength = 3

// consumerControlReportID is the report-ID prefix that distinguishes
// Consumer Control reports from other Top-Level Collections sharing
// the same physical USB interface on the reference keypad.
const consumerControlReportID byte = 0x03

// ConsumerMapper translates Consumer Control reports from a keypad into
// protocol.InputEvent values. Each report carries the one usage held
// down, or 0 when nothing is.
//
// Encoder usages (device.Codes.Encoders) stand for instantaneous
// gestures: each detent and each click arrives as one press+release
// pair, and the mapper emits exactly one event per press.
//
// Key usages (device.Codes.ConsumerKeys) emit press and release. The
// report holds a single usage, so a key counts as released as soon as a
// report stops carrying it: holding key 1 and pressing key 2 releases
// key 1, and letting go of key 1 afterwards emits nothing.
type ConsumerMapper struct {
	deviceID protocol.DeviceID
	codes    device.Codes
	clock    func() time.Time
	// held is the key usage the last report carried, 0 for none.
	held uint16
}

// ConsumerOption configures a ConsumerMapper at construction time.
type ConsumerOption func(*ConsumerMapper)

// WithConsumerClock overrides the time source the mapper uses to stamp
// events. Intended for tests that need deterministic timestamps.
func WithConsumerClock(clock func() time.Time) ConsumerOption {
	return func(m *ConsumerMapper) { m.clock = clock }
}

// NewConsumerMapper returns a ConsumerMapper that emits InputEvents
// tagged with the given device id for the usages in codes.Encoders and
// codes.ConsumerKeys.
func NewConsumerMapper(deviceID protocol.DeviceID, codes device.Codes, opts ...ConsumerOption) *ConsumerMapper {
	m := &ConsumerMapper{
		deviceID: deviceID,
		codes:    codes,
		clock:    time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Map returns the InputEvents produced by one Consumer Control report:
// at most a key release followed by a key press or an encoder gesture.
// The slice is always non-nil. Reports shorter than
// consumerReportLength are rejected with ErrShortReport; reports with
// an unexpected report-ID prefix return an empty slice and no error so
// callers can drain multiplexed report streams without special-casing.
// Unknown usages emit nothing but still release a held key.
func (m *ConsumerMapper) Map(report []byte) ([]protocol.InputEvent, error) {
	if len(report) < consumerReportLength {
		return nil, fmt.Errorf("events.ConsumerMapper.Map (got %d bytes, want %d): %w", len(report), consumerReportLength, ErrShortReport)
	}
	if report[0] != consumerControlReportID {
		return []protocol.InputEvent{}, nil
	}

	usage := uint16(report[1]) | uint16(report[2])<<8
	if usage == m.held {
		return []protocol.InputEvent{}, nil
	}

	timestamp := int(m.clock().UnixMilli())
	events := make([]protocol.InputEvent, 0, 2)
	if m.held != 0 {
		events = append(events, m.newEvent(m.codes.ConsumerKeys[m.held], protocol.InputKindKey, protocol.InputActionRelease, timestamp))
		m.held = 0
	}
	if id, ok := m.codes.ConsumerKeys[usage]; ok {
		m.held = usage
		return append(events, m.newEvent(id, protocol.InputKindKey, protocol.InputActionPress, timestamp)), nil
	}
	if in, ok := m.codes.Encoders[usage]; ok {
		events = append(events, m.newEvent(in.ID, protocol.InputKindEncoder, in.Action, timestamp))
	}
	return events, nil
}

func (m *ConsumerMapper) newEvent(inputID string, kind protocol.InputKind, action protocol.InputAction, timestamp int) protocol.InputEvent {
	return protocol.InputEvent{
		Action:      action,
		DeviceId:    m.deviceID,
		InputId:     inputID,
		Kind:        kind,
		TimestampMs: timestamp,
	}
}
