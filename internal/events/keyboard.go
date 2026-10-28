package events

import (
	"fmt"
	"sort"
	"time"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// keyboardReportLength is the fixed size of an HID boot keyboard input
// report: 1 modifier byte, 1 reserved byte, then 6 keycode slots.
const keyboardReportLength = 8

// modifierNames maps each bit of the boot keyboard modifier byte to the
// stable input_id used in emitted events. Index = bit position 0..7.
var modifierNames = [8]string{
	"mod_lctrl",
	"mod_lshift",
	"mod_lalt",
	"mod_lmeta",
	"mod_rctrl",
	"mod_rshift",
	"mod_ralt",
	"mod_rmeta",
}

// KeyboardMapper translates a stream of HID boot keyboard reports into
// press/release InputEvents by diffing each report against the previous
// one. It is single-consumer and not safe for concurrent use.
type KeyboardMapper struct {
	deviceID protocol.DeviceID
	clock    func() time.Time
	prev     []byte
}

// Option configures a KeyboardMapper at construction time.
type Option func(*KeyboardMapper)

// WithClock overrides the time source the mapper uses to stamp events.
// Intended for tests that need deterministic timestamps.
func WithClock(clock func() time.Time) Option {
	return func(m *KeyboardMapper) { m.clock = clock }
}

// NewKeyboardMapper returns a KeyboardMapper that emits InputEvents
// tagged with the given device id. The first call to Map treats the
// previous report as all zeros (no keys pressed).
func NewKeyboardMapper(deviceID protocol.DeviceID, opts ...Option) *KeyboardMapper {
	m := &KeyboardMapper{
		deviceID: deviceID,
		clock:    time.Now,
		prev:     make([]byte, keyboardReportLength),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Map returns the InputEvents produced by the transition from the
// previous report to report. The slice is non-nil but may be empty when
// nothing changed. Reports shorter than keyboardReportLength bytes are
// rejected with ErrShortReport.
func (m *KeyboardMapper) Map(report []byte) ([]protocol.InputEvent, error) {
	if len(report) < keyboardReportLength {
		return nil, fmt.Errorf("events.Map (got %d bytes, want %d): %w", len(report), keyboardReportLength, ErrShortReport)
	}

	timestamp := m.clock().UnixMilli()
	events := make([]protocol.InputEvent, 0)

	events = m.appendModifierDiff(events, m.prev[0], report[0], timestamp)
	events = m.appendKeycodeDiff(events, m.prev[2:keyboardReportLength], report[2:keyboardReportLength], timestamp)

	copy(m.prev, report[:keyboardReportLength])
	return events, nil
}

func (m *KeyboardMapper) appendModifierDiff(events []protocol.InputEvent, prev, curr byte, timestamp int64) []protocol.InputEvent {
	for bit := uint8(0); bit < 8; bit++ {
		mask := byte(1) << bit
		was := prev&mask != 0
		now := curr&mask != 0
		switch {
		case was && !now:
			events = append(events, m.newEvent(modifierNames[bit], protocol.InputActionRelease, timestamp))
		case !was && now:
			events = append(events, m.newEvent(modifierNames[bit], protocol.InputActionPress, timestamp))
		}
	}
	return events
}

func (m *KeyboardMapper) appendKeycodeDiff(events []protocol.InputEvent, prev, curr []byte, timestamp int64) []protocol.InputEvent {
	prevSet := keycodeSet(prev)
	currSet := keycodeSet(curr)

	released := sortedDiff(prevSet, currSet)
	for _, code := range released {
		events = append(events, m.newEvent(keycodeName(code), protocol.InputActionRelease, timestamp))
	}

	pressed := sortedDiff(currSet, prevSet)
	for _, code := range pressed {
		events = append(events, m.newEvent(keycodeName(code), protocol.InputActionPress, timestamp))
	}
	return events
}

func (m *KeyboardMapper) newEvent(inputID string, action protocol.InputAction, timestamp int64) protocol.InputEvent {
	return protocol.InputEvent{
		Action:      action,
		DeviceId:    m.deviceID,
		InputId:     inputID,
		Kind:        protocol.InputKindKey,
		TimestampMs: int(timestamp),
	}
}

// keycodeSet returns the set of distinct non-empty keycodes present in
// the given slice. The reserved value 0x00 (no key) is skipped.
func keycodeSet(slots []byte) map[byte]struct{} {
	set := make(map[byte]struct{}, len(slots))
	for _, code := range slots {
		if code == 0x00 {
			continue
		}
		set[code] = struct{}{}
	}
	return set
}

// sortedDiff returns the keys present in a but not in b, ordered
// ascending by byte value for deterministic event emission.
func sortedDiff(a, b map[byte]struct{}) []byte {
	out := make([]byte, 0, len(a))
	for code := range a {
		if _, ok := b[code]; !ok {
			out = append(out, code)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func keycodeName(code byte) string {
	return fmt.Sprintf("key_0x%02x", code)
}
