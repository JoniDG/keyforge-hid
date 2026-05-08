package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentifier_Identify_OnEmptyInput_ShouldReturnEmptySlice(t *testing.T) {
	t.Parallel()
	i := NewIdentifier(DefaultRegistry())

	got := i.Identify(nil)

	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestIdentifier_Identify_WhenAllUnknown_ShouldReturnDevicesWithRecognizedFalse(t *testing.T) {
	t.Parallel()
	infos := []Info{
		{VendorID: 0x1111, ProductID: 0x2222, Path: "p1"},
		{VendorID: 0x1111, ProductID: 0x2222, Path: "p2"},
	}
	i := NewIdentifier(NewRegistry())

	got := i.Identify(infos)

	require.Len(t, got, 1)
	assert.False(t, got[0].Recognized)
	assert.Equal(t, KnownDevice{}, got[0].Known)
	assert.Equal(t, uint16(0x1111), got[0].VendorID)
	assert.Equal(t, uint16(0x2222), got[0].ProductID)
	assert.Len(t, got[0].Interfaces, 2)
}

func TestIdentifier_Identify_WhenAllKnown_ShouldFlagRecognizedAndPopulateMetadata(t *testing.T) {
	t.Parallel()
	infos := []Info{
		{VendorID: SideKeyboardKeypad.VendorID, ProductID: SideKeyboardKeypad.ProductID, Path: "p1"},
		{VendorID: SideKeyboardKeypad.VendorID, ProductID: SideKeyboardKeypad.ProductID, Path: "p2"},
	}
	i := NewIdentifier(DefaultRegistry())

	got := i.Identify(infos)

	require.Len(t, got, 1)
	assert.True(t, got[0].Recognized)
	assert.Equal(t, SideKeyboardKeypad, got[0].Known)
	assert.Len(t, got[0].Interfaces, 2)
}

func TestIdentifier_Identify_WhenMixed_ShouldReturnAllDevicesWithProperFlags(t *testing.T) {
	t.Parallel()
	infos := []Info{
		{VendorID: 0xAAAA, ProductID: 0xBBBB, Path: "unknown-1"},
		{VendorID: SideKeyboardKeypad.VendorID, ProductID: SideKeyboardKeypad.ProductID, Path: "known-1"},
		{VendorID: 0xAAAA, ProductID: 0xBBBB, Path: "unknown-2"},
		{VendorID: SideKeyboardKeypad.VendorID, ProductID: SideKeyboardKeypad.ProductID, Path: "known-2"},
	}
	i := NewIdentifier(DefaultRegistry())

	got := i.Identify(infos)

	require.Len(t, got, 2, "two distinct (vid,pid) pairs")
	assert.False(t, got[0].Recognized, "first pair seen is the unknown one")
	assert.Equal(t, uint16(0xAAAA), got[0].VendorID)
	assert.Len(t, got[0].Interfaces, 2)
	assert.True(t, got[1].Recognized)
	assert.Equal(t, SideKeyboardKeypad, got[1].Known)
	assert.Len(t, got[1].Interfaces, 2)
}

func TestIdentifier_Identify_ShouldPreserveFirstSeenOrderAcrossDevices(t *testing.T) {
	t.Parallel()
	infos := []Info{
		{VendorID: 0x0003, ProductID: 0x0001, Path: "c"},
		{VendorID: 0x0001, ProductID: 0x0001, Path: "a"},
		{VendorID: 0x0002, ProductID: 0x0001, Path: "b"},
		{VendorID: 0x0001, ProductID: 0x0001, Path: "a2"},
	}
	i := NewIdentifier(NewRegistry())

	got := i.Identify(infos)

	require.Len(t, got, 3)
	assert.Equal(t, uint16(0x0003), got[0].VendorID)
	assert.Equal(t, uint16(0x0001), got[1].VendorID)
	assert.Equal(t, uint16(0x0002), got[2].VendorID)
}

func TestIdentifier_Identify_ShouldPreserveInterfaceOrderWithinDevice(t *testing.T) {
	t.Parallel()
	vid, pid := uint16(0x0001), uint16(0x0001)
	infos := []Info{
		{VendorID: vid, ProductID: pid, Path: "first"},
		{VendorID: vid, ProductID: pid, Path: "second"},
		{VendorID: vid, ProductID: pid, Path: "third"},
	}
	i := NewIdentifier(NewRegistry())

	got := i.Identify(infos)

	require.Len(t, got, 1)
	require.Len(t, got[0].Interfaces, 3)
	assert.Equal(t, "first", got[0].Interfaces[0].Path)
	assert.Equal(t, "second", got[0].Interfaces[1].Path)
	assert.Equal(t, "third", got[0].Interfaces[2].Path)
}

func TestNewIdentifier_ShouldReturnNonNilImpl(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, NewIdentifier(DefaultRegistry()))
}

func TestIdentifiedDevice_PrimaryInput_WhenKeyboardInterfacePresent_ShouldReturnIt(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{
		VendorID:  SideKeyboardKeypad.VendorID,
		ProductID: SideKeyboardKeypad.ProductID,
		Interfaces: []Info{
			{UsagePage: 0xFF00, Usage: 0x0002, Path: "vendor-specific"},
			{UsagePage: 0x0001, Usage: 0x0006, Path: "keyboard"},
		},
	}

	got, err := d.PrimaryInput()

	require.NoError(t, err)
	assert.Equal(t, "keyboard", got.Path)
}

func TestIdentifiedDevice_PrimaryInput_WhenMultipleKeyboardInterfaces_ShouldReturnFirstSeen(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{
		Interfaces: []Info{
			{UsagePage: 0x0001, Usage: 0x0006, Path: "first"},
			{UsagePage: 0x0001, Usage: 0x0006, Path: "second"},
		},
	}

	got, err := d.PrimaryInput()

	require.NoError(t, err)
	assert.Equal(t, "first", got.Path)
}

func TestIdentifiedDevice_PrimaryInput_WhenNoKeyboardInterface_ShouldReturnErrNoPrimaryInterface(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{
		VendorID:  0xAAAA,
		ProductID: 0xBBBB,
		Interfaces: []Info{
			{UsagePage: 0xFF00, Usage: 0x0002, Path: "vendor-only"},
		},
	}

	_, err := d.PrimaryInput()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPrimaryInterface)
}

func TestIdentifiedDevice_PrimaryInput_WhenNoInterfaces_ShouldReturnErrNoPrimaryInterface(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{}

	_, err := d.PrimaryInput()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoPrimaryInterface)
}
