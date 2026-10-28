package events

import (
	"testing"
	"time"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDeviceID protocol.DeviceID = "VID_6D82_PID_DC83"

func fixedClock(epochMs int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(epochMs) }
}

func newTestMapper(t *testing.T, epochMs int64) *KeyboardMapper {
	t.Helper()
	return NewKeyboardMapper(testDeviceID, nil, WithClock(fixedClock(epochMs)))
}

func TestKeyboardMapper_Map_OnReportShorterThanEightBytes_ShouldReturnErrShortReport(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0, 0, 0, 0, 0, 0, 0})

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrShortReport)
}

func TestKeyboardMapper_Map_OnAllZeros_ShouldReturnEmptySlice(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0, 0, 0, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestKeyboardMapper_Map_WhenSingleKeyPressed_ShouldEmitOnePressEvent(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 1746662400123)

	got, err := m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputEvent{
		Action:      protocol.InputActionPress,
		DeviceId:    testDeviceID,
		InputId:     "key_0x04",
		Kind:        protocol.InputKindKey,
		TimestampMs: 1746662400123,
	}, got[0])
}

func TestKeyboardMapper_Map_WhenKeyReleased_ShouldEmitOneReleaseEvent(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)
	_, _ = m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})

	got, err := m.Map([]byte{0, 0, 0, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRelease, got[0].Action)
	assert.Equal(t, "key_0x04", got[0].InputId)
}

func TestKeyboardMapper_Map_WhenMultipleKeysPressedTogether_ShouldEmitOneEventPerKeySortedAscending(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0, 0, 0x06, 0x04, 0x05, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "key_0x04", got[0].InputId)
	assert.Equal(t, "key_0x05", got[1].InputId)
	assert.Equal(t, "key_0x06", got[2].InputId)
	for _, e := range got {
		assert.Equal(t, protocol.InputActionPress, e.Action)
	}
}

func TestKeyboardMapper_Map_WhenSingleModifierPressed_ShouldEmitPressForThatBitOnly(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0x01, 0, 0, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "mod_lctrl", got[0].InputId)
	assert.Equal(t, protocol.InputActionPress, got[0].Action)
}

func TestKeyboardMapper_Map_WhenAllModifierBitsSet_ShouldEmitOnePressPerBitInOrder(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0xFF, 0, 0, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 8)
	wantOrder := []string{"mod_lctrl", "mod_lshift", "mod_lalt", "mod_lmeta", "mod_rctrl", "mod_rshift", "mod_ralt", "mod_rmeta"}
	for i, e := range got {
		assert.Equal(t, wantOrder[i], e.InputId)
		assert.Equal(t, protocol.InputActionPress, e.Action)
	}
}

func TestKeyboardMapper_Map_WhenModifierAndKeyTogether_ShouldEmitModifierBeforeKey(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0x01, 0, 0x04, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "mod_lctrl", got[0].InputId)
	assert.Equal(t, "key_0x04", got[1].InputId)
}

func TestKeyboardMapper_Map_WhenKeyReplaced_ShouldEmitReleaseThenPress(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)
	_, _ = m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})

	got, err := m.Map([]byte{0, 0, 0x05, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, protocol.InputActionRelease, got[0].Action)
	assert.Equal(t, "key_0x04", got[0].InputId)
	assert.Equal(t, protocol.InputActionPress, got[1].Action)
	assert.Equal(t, "key_0x05", got[1].InputId)
}

func TestKeyboardMapper_Map_WhenSameKeysInDifferentOrder_ShouldEmitNothing(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)
	_, _ = m.Map([]byte{0, 0, 0x04, 0x05, 0, 0, 0, 0})

	got, err := m.Map([]byte{0, 0, 0x05, 0x04, 0, 0, 0, 0})

	require.NoError(t, err)
	assert.Empty(t, got, "boot keyboard report bytes 2-7 are a set, order must not produce events")
}

func TestKeyboardMapper_Map_WhenZerosInsideBytes_ShouldSkipThemAsEmptySlots(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0, 0, 0x04, 0, 0x05, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "key_0x04", got[0].InputId)
	assert.Equal(t, "key_0x05", got[1].InputId)
}

func TestKeyboardMapper_Map_ShouldStampEventsWithCurrentClockReading(t *testing.T) {
	t.Parallel()
	now := int64(1746662400000)
	m := newTestMapper(t, now)

	got, err := m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int(now), got[0].TimestampMs)
}

func TestKeyboardMapper_Map_AcceptsReportsLongerThanEightBytes(t *testing.T) {
	t.Parallel()
	m := newTestMapper(t, 0)

	got, err := m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0, 0xFF, 0xAB})

	require.NoError(t, err)
	require.Len(t, got, 1, "extra bytes past byte 7 must be ignored")
	assert.Equal(t, "key_0x04", got[0].InputId)
}

func TestKeyboardMapper_Map_WhenUsageIsInKeys_ShouldUseItsInputID(t *testing.T) {
	t.Parallel()
	m := NewKeyboardMapper(testDeviceID, map[byte]string{0xA5: "key_0x68"}, WithClock(fixedClock(0)))

	pressed, err := m.Map([]byte{0, 0, 0xA5, 0x04, 0, 0, 0, 0})
	require.NoError(t, err)
	released, err := m.Map([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	require.NoError(t, err)

	ids := func(evs []protocol.InputEvent) []string {
		out := make([]string, 0, len(evs))
		for _, e := range evs {
			out = append(out, string(e.Action)+" "+e.InputId)
		}
		return out
	}
	// 0x04 is not in keys, so it keeps its generic id.
	assert.Equal(t, []string{"press key_0x04", "press key_0x68"}, ids(pressed))
	assert.Equal(t, []string{"release key_0x04", "release key_0x68"}, ids(released))
}

func TestNewKeyboardMapper_WithoutOptions_ShouldUseRealClock(t *testing.T) {
	t.Parallel()
	m := NewKeyboardMapper(testDeviceID, nil)

	before := time.Now().UnixMilli()
	got, err := m.Map([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	after := time.Now().UnixMilli()

	require.NoError(t, err)
	require.Len(t, got, 1)
	stamp := int64(got[0].TimestampMs)
	assert.GreaterOrEqual(t, stamp, before)
	assert.LessOrEqual(t, stamp, after)
}
