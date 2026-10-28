package events

import (
	"testing"
	"time"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEncoderMapper(t *testing.T, epochMs int64) *EncoderMapper {
	t.Helper()
	return NewEncoderMapper(testDeviceID, WithEncoderClock(fixedClock(epochMs)))
}

func TestEncoderMapper_Map_OnReportShorterThanThreeBytes_ShouldReturnErrShortReport(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE9})

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrShortReport)
}

func TestEncoderMapper_Map_OnUnexpectedReportID_ShouldReturnEmpty(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x01, 0xE9, 0x00})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestEncoderMapper_Map_OnVolumeIncrement_ShouldEmitRotateCw(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 1746662400123)

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

func TestEncoderMapper_Map_OnVolumeDecrement_ShouldEmitRotateCcw(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xEA, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRotateCcw, got[0].Action)
	assert.Equal(t, "encoder_0", got[0].InputId)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
}

func TestEncoderMapper_Map_OnMute_ShouldEmitClick(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE2, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionClick, got[0].Action)
	assert.Equal(t, "encoder_0", got[0].InputId)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
}

func TestEncoderMapper_Map_OnReleaseReport_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0x00, 0x00})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestEncoderMapper_Map_OnUnknownUsage_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0x30, 0x00}) // Power: not an encoder usage

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestEncoderMapper_Map_OnEachEncoderUsage_ShouldEmitItsEncoderAndAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		usage  [2]byte
		id     string
		action protocol.InputAction
	}{
		{name: "encoder 1 click (mute)", usage: [2]byte{0xE2, 0x00}, id: "encoder_0", action: protocol.InputActionClick},
		{name: "encoder 1 cw (volume up)", usage: [2]byte{0xE9, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCw},
		{name: "encoder 1 ccw (volume down)", usage: [2]byte{0xEA, 0x00}, id: "encoder_0", action: protocol.InputActionRotateCcw},
		{name: "encoder 2 click (play/pause)", usage: [2]byte{0xCD, 0x00}, id: "encoder_1", action: protocol.InputActionClick},
		{name: "encoder 2 cw (next track)", usage: [2]byte{0xB5, 0x00}, id: "encoder_1", action: protocol.InputActionRotateCw},
		{name: "encoder 2 ccw (previous track)", usage: [2]byte{0xB6, 0x00}, id: "encoder_1", action: protocol.InputActionRotateCcw},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTestEncoderMapper(t, 0)

			got, err := m.Map([]byte{0x03, tt.usage[0], tt.usage[1]})

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.id, got[0].InputId)
			assert.Equal(t, tt.action, got[0].Action)
			assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
		})
	}
}

func TestEncoderMapper_Map_OnSixteenBitUsage_ShouldDecodeLittleEndian(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

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

func TestEncoderMapper_Map_OnReportLongerThanThreeBytes_ShouldStillDecodeFirstUsage(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

	got, err := m.Map([]byte{0x03, 0xE9, 0x00, 0xFF, 0xAA})

	require.NoError(t, err)
	require.Len(t, got, 1, "extra bytes past byte 2 must be ignored")
	assert.Equal(t, protocol.InputActionRotateCw, got[0].Action)
}

func TestEncoderMapper_Map_OnSuccessivePresses_ShouldEmitOneEventEach(t *testing.T) {
	t.Parallel()
	m := newTestEncoderMapper(t, 0)

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

func TestEncoderMapper_Map_ShouldStampEventsWithCurrentClockReading(t *testing.T) {
	t.Parallel()
	now := int64(1746662400000)
	m := newTestEncoderMapper(t, now)

	got, err := m.Map([]byte{0x03, 0xE9, 0x00})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int(now), got[0].TimestampMs)
}

func TestNewEncoderMapper_WithoutOptions_ShouldUseRealClock(t *testing.T) {
	t.Parallel()
	m := NewEncoderMapper(testDeviceID)

	before := time.Now().UnixMilli()
	got, err := m.Map([]byte{0x03, 0xE9, 0x00})
	after := time.Now().UnixMilli()

	require.NoError(t, err)
	require.Len(t, got, 1)
	stamp := int64(got[0].TimestampMs)
	assert.GreaterOrEqual(t, stamp, before)
	assert.LessOrEqual(t, stamp, after)
}
