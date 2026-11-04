package events

import (
	"testing"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConsumerMapper(t *testing.T, epochMs int64) *ConsumerMapper {
	t.Helper()
	return NewConsumerMapper(testDeviceID, device.SideKeyboardKeypad.Codes, WithConsumerClock(fixedClock(epochMs)))
}

func TestConsumerMapper_Map_OnReportShorterThanThreeBytes_ShouldReturnErrShortReport(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE9})

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrShortReport)
}

func TestConsumerMapper_Map_OnUnexpectedReportID_ShouldReturnEmpty(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x01, 0xE9, 0x00})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestConsumerMapper_Map_OnVolumeIncrement_ShouldEmitRotateCw(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 1746662400123)

	got, err := m.Map([]byte{0x03, 0xE9, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputEvent{
		Action:      protocol.InputActionRotateCw,
		DeviceId:    testDeviceID,
		InputId:     "encoder_0",
		Kind:        protocol.InputKindEncoder,
		TimestampMs: 1746662400123,
	}, got[0])
}

func TestConsumerMapper_Map_OnVolumeDecrement_ShouldEmitRotateCcw(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xEA, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRotateCcw, got[0].Action)
	assert.Equal(t, "encoder_0", got[0].InputId)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
}

func TestConsumerMapper_Map_OnMute_ShouldEmitClick(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE2, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionClick, got[0].Action)
	assert.Equal(t, "encoder_0", got[0].InputId)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
}

func TestConsumerMapper_Map_OnReleaseReport_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0x00, 0x00})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestConsumerMapper_Map_OnUnknownUsage_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0x30, 0x00}) // Power: not an encoder usage

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestConsumerMapper_Map_OnEachEncoderUsage_ShouldEmitItsEncoderAndAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		usage  [2]byte
		id     string
		action protocol.InputAction
	}{
		{name: "encoder 1 click", usage: [2]byte{0x07, 0x00}, id: "encoder_0", action: protocol.InputActionClick},
		{name: "encoder 1 cw", usage: [2]byte{0x08, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCw},
		{name: "encoder 1 ccw", usage: [2]byte{0x09, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCcw},
		{name: "encoder 2 click", usage: [2]byte{0x0A, 0x00}, id: "encoder_1", action: protocol.InputActionClick},
		{name: "encoder 2 cw", usage: [2]byte{0x0B, 0x00}, id: "encoder_1", action: protocol.InputActionRotateCw},
		{name: "encoder 2 ccw", usage: [2]byte{0x0C, 0x00}, id: "encoder_1", action: protocol.InputActionRotateCcw},
		{name: "factory click (mute)", usage: [2]byte{0xE2, 0x00}, id: "encoder_0", action: protocol.InputActionClick},
		{name: "factory cw (volume up)", usage: [2]byte{0xE9, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCw},
		{name: "factory ccw (volume down)", usage: [2]byte{0xEA, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCcw},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTestConsumerMapper(t, 0)

			got, err := m.Map([]byte{0x03, tt.usage[0], tt.usage[1]})

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.id, got[0].InputId)
			assert.Equal(t, tt.action, got[0].Action)
			assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
		})
	}
}

func TestConsumerMapper_Map_WhenCodesNil_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := NewConsumerMapper(testDeviceID, device.Codes{})

	got, err := m.Map([]byte{0x03, 0xE9, 0x00})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestConsumerMapper_Map_OnSixteenBitUsage_ShouldDecodeLittleEndian(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	// 0x00E9 (volume increment) encoded as low byte first.
	got, err := m.Map([]byte{0x03, 0xE9, 0x00})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRotateCw, got[0].Action)

	// 0xE900 (unknown usage in our table) — high byte set should not
	// be confused with the low-byte E9.
	got, err = m.Map([]byte{0x03, 0x00, 0xE9})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestConsumerMapper_Map_OnReportLongerThanThreeBytes_ShouldStillDecodeFirstUsage(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE9, 0x00, 0xFF, 0xAA})

	require.NoError(t, err)
	require.Len(t, got, 1, "extra bytes past byte 2 must be ignored")
	assert.Equal(t, protocol.InputActionRotateCw, got[0].Action)
}

func TestConsumerMapper_Map_OnSuccessivePresses_ShouldEmitOneEventEach(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	out := make([]protocol.InputAction, 0, 6)
	sequence := [][]byte{
		{0x03, 0xE9, 0x00}, // CW
		{0x03, 0x00, 0x00}, // release
		{0x03, 0xEA, 0x00}, // CCW
		{0x03, 0x00, 0x00}, // release
		{0x03, 0xE2, 0x00}, // click
		{0x03, 0x00, 0x00}, // release
	}
	for _, r := range sequence {
		evs, err := m.Map(r)
		require.NoError(t, err)
		for _, e := range evs {
			out = append(out, e.Action)
		}
	}

	assert.Equal(t, []protocol.InputAction{
		protocol.InputActionRotateCw,
		protocol.InputActionRotateCcw,
		protocol.InputActionClick,
	}, out)
}

func TestConsumerMapper_Map_ShouldStampEventsWithCurrentClockReading(t *testing.T) {
	t.Parallel()
	now := int64(1746662400000)
	m := newTestConsumerMapper(t, now)

	got, err := m.Map([]byte{0x03, 0xE9, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int(now), got[0].TimestampMs)
}

func TestNewConsumerMapper_WithoutOptions_ShouldUseRealClock(t *testing.T) {
	t.Parallel()
	m := NewConsumerMapper(testDeviceID, device.SideKeyboardKeypad.Codes)

	before := time.Now().UnixMilli()
	got, err := m.Map([]byte{0x03, 0xE9, 0x00})
	after := time.Now().UnixMilli()

	require.NoError(t, err)
	require.Len(t, got, 1)
	stamp := int64(got[0].TimestampMs)
	assert.GreaterOrEqual(t, stamp, before)
	assert.LessOrEqual(t, stamp, after)
}

// mapAll feeds reports through m and returns "<input_id> <action>" per
// event, in order.
func mapAll(t *testing.T, m *ConsumerMapper, reports ...[]byte) []string {
	t.Helper()
	out := []string{}
	for _, r := range reports {
		evs, err := m.Map(r)
		require.NoError(t, err)
		for _, e := range evs {
			out = append(out, e.InputId+" "+string(e.Action))
		}
	}
	return out
}

func TestConsumerMapper_Map_OnKeyUsage_ShouldEmitKeyPress(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 1746662400123)

	got, err := m.Map([]byte{0x03, 0x0D, 0x00})

	require.NoError(t, err)
	assert.Equal(t, []protocol.InputEvent{{
		Action:      protocol.InputActionPress,
		DeviceId:    testDeviceID,
		InputId:     "key_0x68",
		Kind:        protocol.InputKindKey,
		TimestampMs: 1746662400123,
	}}, got)
}

func TestConsumerMapper_Map_OnEachKeyUsage_ShouldPressAndReleaseItsKey(t *testing.T) {
	t.Parallel()
	ids := []string{"key_0x68", "key_0x69", "key_0x6a", "key_0x6b", "key_0x6c", "key_0x6d", "key_0x6e", "key_0x6f", "key_0x70", "key_0x71"}
	for i, id := range ids {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			m := newTestConsumerMapper(t, 0)

			got := mapAll(t, m, []byte{0x03, byte(0x0D + i), 0x00}, []byte{0x03, 0x00, 0x00}) // #nosec G115 -- i < 10

			assert.Equal(t, []string{id + " press", id + " release"}, got)
		})
	}
}

func TestConsumerMapper_Map_WhenSameKeyReportRepeats_ShouldEmitOnePress(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got := mapAll(t, m, []byte{0x03, 0x0D, 0x00}, []byte{0x03, 0x0D, 0x00}, []byte{0x03, 0x00, 0x00}, []byte{0x03, 0x00, 0x00})

	assert.Equal(t, []string{"key_0x68 press", "key_0x68 release"}, got)
}

// The report holds one usage: pressing key 2 while key 1 is held
// replaces it, and the release of either sends 00 00.
func TestConsumerMapper_Map_WhenKeysOverlap_ShouldReleaseTheFirstOnTheSecondPress(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got := mapAll(t, m,
		[]byte{0x03, 0x0D, 0x00}, // key 1 down
		[]byte{0x03, 0x0E, 0x00}, // key 2 down, key 1 still held
		[]byte{0x03, 0x00, 0x00}, // key 2 up
		[]byte{0x03, 0x00, 0x00}, // key 1 up
	)

	assert.Equal(t, []string{"key_0x68 press", "key_0x68 release", "key_0x69 press", "key_0x69 release"}, got)
}

func TestConsumerMapper_Map_WhenEncoderTurnsWhileKeyHeld_ShouldReleaseKeyThenEmitGesture(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got := mapAll(t, m, []byte{0x03, 0x0D, 0x00}, []byte{0x03, 0x08, 0x00}, []byte{0x03, 0x00, 0x00})

	assert.Equal(t, []string{"key_0x68 press", "key_0x68 release", "encoder_0 rotate_cw"}, got)
}

func TestConsumerMapper_Map_WhenUnknownUsageWhileKeyHeld_ShouldOnlyReleaseKey(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got := mapAll(t, m, []byte{0x03, 0x0D, 0x00}, []byte{0x03, 0x30, 0x00})

	assert.Equal(t, []string{"key_0x68 press", "key_0x68 release"}, got)
}

func TestConsumerMapper_Map_WhenOtherReportIDWhileKeyHeld_ShouldKeepKeyHeld(t *testing.T) {
	t.Parallel()
	m := newTestConsumerMapper(t, 0)

	got := mapAll(t, m, []byte{0x03, 0x0D, 0x00}, []byte{0x01, 0x00, 0x00}, []byte{0x03, 0x00, 0x00})

	assert.Equal(t, []string{"key_0x68 press", "key_0x68 release"}, got)
}
