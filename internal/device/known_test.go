package device

import (
	"testing"

	"github.com/JoniDG/keyforge-hid/internal/vendor"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Lookup_WhenDeviceRegistered_ShouldReturnIt(t *testing.T) {
	t.Parallel()
	r := NewRegistry(SideKeyboardKeypad)

	got, ok := r.Lookup(SideKeyboardKeypad.VendorID, SideKeyboardKeypad.ProductID)

	assert.True(t, ok)
	assert.Equal(t, SideKeyboardKeypad, got)
}

func TestRegistry_Lookup_WhenDeviceUnknown_ShouldReturnZeroAndFalse(t *testing.T) {
	t.Parallel()
	r := NewRegistry(SideKeyboardKeypad)

	got, ok := r.Lookup(0x1234, 0x5678)

	assert.False(t, ok)
	assert.Equal(t, KnownDevice{}, got)
}

func TestRegistry_Lookup_OnEmptyRegistry_ShouldReturnFalse(t *testing.T) {
	t.Parallel()
	r := NewRegistry()

	_, ok := r.Lookup(SideKeyboardKeypad.VendorID, SideKeyboardKeypad.ProductID)

	assert.False(t, ok)
}

func TestNewRegistry_OnDuplicateKey_LastWriteWins(t *testing.T) {
	t.Parallel()
	first := KnownDevice{VendorID: 0x6D82, ProductID: 0xDC83, Name: "first"}
	second := KnownDevice{VendorID: 0x6D82, ProductID: 0xDC83, Name: "second"}
	r := NewRegistry(first, second)

	got, ok := r.Lookup(0x6D82, 0xDC83)

	assert.True(t, ok)
	assert.Equal(t, "second", got.Name)
}

func TestDefaultRegistry_ShouldRecognizeSideKeyboardKeypad(t *testing.T) {
	t.Parallel()
	r := DefaultRegistry()

	got, ok := r.Lookup(SideKeyboardKeypad.VendorID, SideKeyboardKeypad.ProductID)

	assert.True(t, ok)
	assert.Equal(t, SideKeyboardKeypad, got)
}

func TestSideKeyboardKeypad_HasExpectedVIDPID(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint16(0x6D82), SideKeyboardKeypad.VendorID)
	assert.Equal(t, uint16(0xDC83), SideKeyboardKeypad.ProductID)
	assert.Equal(t, "SDINNOVATION SIDE-KEYBOARD", SideKeyboardKeypad.Name)
}

func TestSideKeyboardKeypad_DeclaresKeyboardAndEncoderInputs(t *testing.T) {
	t.Parallel()
	require.Len(t, SideKeyboardKeypad.Inputs, 2)

	assert.Equal(t, KnownInput{Role: RoleKeyboard, UsagePage: 0x0001, Usage: 0x0006}, SideKeyboardKeypad.Inputs[0])
	assert.Equal(t, KnownInput{Role: RoleEncoder, UsagePage: 0x000c, Usage: 0x0001}, SideKeyboardKeypad.Inputs[1])
}

func TestSideKeyboardKeypad_DeclaresVendorInterface(t *testing.T) {
	t.Parallel()
	v := SideKeyboardKeypad.Vendor
	assert.Equal(t, uint16(0xFF00), v.UsagePage)
	assert.Equal(t, uint16(0x0002), v.Usage)
	assert.Equal(t, 22, v.Slots)
	assert.Equal(t, 10, v.LEDs)
	assert.Len(t, v.Layout, v.Slots)
	assert.Len(t, v.Factory, v.Slots)
}

func TestSideKeyboardKeypad_Layout_ShouldGiveEveryInputADistinctCode(t *testing.T) {
	t.Parallel()
	layout := SideKeyboardKeypad.Vendor.Layout

	for i := range 10 {
		assert.Equal(t, vendor.KeyboardSlot(0x00, byte(0x68+i)), layout[i], "key slot %d should be F%d", i, 13+i)
	}
	for i := 10; i < 16; i++ {
		assert.Equal(t, vendor.DisabledSlot(), layout[i], "slot %d", i)
	}
	assert.Equal(t, []vendor.Slot{
		vendor.ConsumerSlot(0x00E2), vendor.ConsumerSlot(0x00E9), vendor.ConsumerSlot(0x00EA), // encoder 1
		vendor.ConsumerSlot(0x00CD), vendor.ConsumerSlot(0x00B5), vendor.ConsumerSlot(0x00B6), // encoder 2
	}, layout[16:22])
}

func TestSideKeyboardKeypad_Factory_ShouldMatchOutOfTheBoxSlots(t *testing.T) {
	t.Parallel()
	factory := SideKeyboardKeypad.Vendor.Factory

	for i := range 10 {
		assert.Equal(t, vendor.KeyboardSlot(0x01, 0x04), factory[i], "key slot %d should be Ctrl+A", i)
	}
	for i := 10; i < 16; i++ {
		assert.Equal(t, vendor.DisabledSlot(), factory[i], "slot %d", i)
	}
	for _, base := range []int{16, 19} {
		assert.Equal(t, []vendor.Slot{
			vendor.ConsumerSlot(0x00E2), vendor.ConsumerSlot(0x00E9), vendor.ConsumerSlot(0x00EA),
		}, factory[base:base+3], "encoder at slot %d", base)
	}
}

func TestSideKeyboardKeypad_DeclaresLogicalControlCatalog(t *testing.T) {
	t.Parallel()
	want := []struct {
		id    string
		kind  protocol.InputKind
		label string
	}{
		{"key_0x68", protocol.InputKindKey, "Key 1"},
		{"key_0x69", protocol.InputKindKey, "Key 2"},
		{"key_0x6a", protocol.InputKindKey, "Key 3"},
		{"key_0x6b", protocol.InputKindKey, "Key 4"},
		{"key_0x6c", protocol.InputKindKey, "Key 5"},
		{"key_0x6d", protocol.InputKindKey, "Key 6"},
		{"key_0x6e", protocol.InputKindKey, "Key 7"},
		{"key_0x6f", protocol.InputKindKey, "Key 8"},
		{"key_0x70", protocol.InputKindKey, "Key 9"},
		{"key_0x71", protocol.InputKindKey, "Key 10"},
		{"encoder_0", protocol.InputKindEncoder, "Encoder 1"},
		{"encoder_1", protocol.InputKindEncoder, "Encoder 2"},
	}
	require.Len(t, SideKeyboardKeypad.Controls, len(want))
	for i, w := range want {
		got := SideKeyboardKeypad.Controls[i]
		assert.Equal(t, w.id, got.Id)
		assert.Equal(t, w.kind, got.Kind)
		require.NotNil(t, got.Label)
		assert.Equal(t, w.label, *got.Label)
	}
}

func TestControl_ShouldSetLabelPointer(t *testing.T) {
	t.Parallel()
	got := control("encoder_0", protocol.InputKindEncoder, "Encoder")

	assert.Equal(t, "encoder_0", got.Id)
	assert.Equal(t, protocol.InputKindEncoder, got.Kind)
	require.NotNil(t, got.Label)
	assert.Equal(t, "Encoder", *got.Label)
}
