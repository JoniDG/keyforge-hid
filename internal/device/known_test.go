package device

import (
	"testing"

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
	assert.Equal(t, VendorInterface{UsagePage: 0xFF00, Usage: 0x0002, Slots: 22, LEDs: 10}, SideKeyboardKeypad.Vendor)
}

func TestSideKeyboardKeypad_DeclaresLogicalControlCatalog(t *testing.T) {
	t.Parallel()
	require.Len(t, SideKeyboardKeypad.Controls, 3)

	mod := SideKeyboardKeypad.Controls[0]
	assert.Equal(t, "mod_lctrl", mod.Id)
	assert.Equal(t, protocol.InputKindKey, mod.Kind)
	require.NotNil(t, mod.Label)
	assert.Equal(t, "Left Ctrl", *mod.Label)

	key := SideKeyboardKeypad.Controls[1]
	assert.Equal(t, "key_0x04", key.Id)
	assert.Equal(t, protocol.InputKindKey, key.Kind)
	require.NotNil(t, key.Label)
	assert.Equal(t, "Key", *key.Label)

	enc := SideKeyboardKeypad.Controls[2]
	assert.Equal(t, "encoder_0", enc.Id)
	assert.Equal(t, protocol.InputKindEncoder, enc.Kind)
	require.NotNil(t, enc.Label)
	assert.Equal(t, "Encoder", *enc.Label)
}

func TestControl_ShouldSetLabelPointer(t *testing.T) {
	t.Parallel()
	got := control("encoder_0", protocol.InputKindEncoder, "Encoder")

	assert.Equal(t, "encoder_0", got.Id)
	assert.Equal(t, protocol.InputKindEncoder, got.Kind)
	require.NotNil(t, got.Label)
	assert.Equal(t, "Encoder", *got.Label)
}
