package device

import (
	"testing"

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
