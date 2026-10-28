package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageForVKey_WhenFunctionKeyF13ToF24_ShouldReturnItsUsage(t *testing.T) {
	t.Parallel()
	for i := range 12 {
		got, ok := usageForVKey(vkF13 + uint16(i))
		require.True(t, ok, "F%d", 13+i)
		assert.Equal(t, byte(0x68+i), got, "F%d", 13+i)
	}
}

func TestUsageForVKey_WhenOutsideF13ToF24_ShouldReturnFalse(t *testing.T) {
	t.Parallel()
	for _, vk := range []uint16{0x41, vkF13 - 1, vkF24 + 1} {
		_, ok := usageForVKey(vk)
		assert.False(t, ok, "vk 0x%02x", vk)
	}
}

func TestHeldKeys_Update_WhenKeysGoDownAndUp_ShouldReportHeldKeys(t *testing.T) {
	t.Parallel()
	var h heldKeys

	r1, ok := h.update(0x68, true)
	require.True(t, ok)
	assert.Equal(t, []byte{0, 0, 0x68, 0, 0, 0, 0, 0}, r1)

	r2, ok := h.update(0x69, true)
	require.True(t, ok)
	assert.Equal(t, []byte{0, 0, 0x68, 0x69, 0, 0, 0, 0}, r2)

	// Releasing the first key keeps the second one held.
	r3, ok := h.update(0x68, false)
	require.True(t, ok)
	assert.Equal(t, []byte{0, 0, 0x69, 0, 0, 0, 0, 0}, r3)

	r4, ok := h.update(0x69, false)
	require.True(t, ok)
	assert.Equal(t, make([]byte, 8), r4)
}

func TestHeldKeys_Update_WhenNothingChanges_ShouldReturnFalse(t *testing.T) {
	t.Parallel()
	var h heldKeys
	_, ok := h.update(0x68, true)
	require.True(t, ok)

	_, repeat := h.update(0x68, true)
	_, stray := h.update(0x70, false)

	assert.False(t, repeat, "a key repeat is not a change")
	assert.False(t, stray, "releasing a key that is not down is not a change")
}

func TestHeldKeys_Update_WhenSixKeysHeld_ShouldIgnoreTheSeventh(t *testing.T) {
	t.Parallel()
	var h heldKeys
	for i := range 6 {
		_, ok := h.update(byte(0x68+i), true)
		require.True(t, ok)
	}

	_, ok := h.update(0x6E, true)

	assert.False(t, ok)
	assert.Len(t, h.usages, 6)
}

func TestSameInterface_WhenComparingNames_ShouldMatchOnInstanceIgnoringClassGUIDAndCase(t *testing.T) {
	t.Parallel()
	const hidPath = `\\?\HID#VID_6D82&PID_DC83&MI_01#7&1a2b&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`
	tests := []struct {
		name    string
		hidPath string
		rawName string
		want    bool
	}{
		{"same instance, keyboard class", hidPath, `\\?\HID#VID_6D82&PID_DC83&MI_01#7&1A2B&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`, true},
		{"other interface", hidPath, `\\?\HID#VID_6D82&PID_DC83&MI_00&Col01#7&9f&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`, false},
		{"other device", hidPath, `\\?\HID#VID_046D&PID_C31C&MI_00#7&1a2b&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`, false},
		{"empty path", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sameInterface(tt.hidPath, tt.rawName))
		})
	}
}
