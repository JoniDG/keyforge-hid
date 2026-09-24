package vendor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyboardSlot_ShouldStoreModifiersThenUsage(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Slot{Type: SlotKeyboard, Codes: [3]byte{0x01, 0x04, 0x00}}, KeyboardSlot(0x01, 0x04))
}

func TestConsumerSlot_ShouldStoreUsageLittleEndian(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Slot{Type: SlotConsumer, Codes: [3]byte{0x23, 0x02, 0x00}}, ConsumerSlot(0x0223))
}

func TestDisabledSlot_ShouldHaveZeroCodes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Slot{Type: SlotDisabled}, DisabledSlot())
}

func TestSlot_String_ShouldDescribeEachType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slot Slot
		want string
	}{
		{slot: DisabledSlot(), want: "disabled"},
		{slot: KeyboardSlot(0x01, 0x04), want: "keyboard mods=0x01 usage=0x04"},
		{slot: ConsumerSlot(0x00E9), want: "consumer usage=0x00e9"},
		{slot: Slot{Type: 0x60, Codes: [3]byte{0x02, 0x00, 0x00}}, want: "type=0x60 codes=02 00 00"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.slot.String())
	}
}
