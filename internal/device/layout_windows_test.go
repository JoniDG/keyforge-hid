//go:build windows

package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendorInterface_PlatformLayout_WhenWindows_ShouldReturnWindowsLayout(t *testing.T) {
	t.Parallel()
	v := SideKeyboardKeypad.Vendor

	assert.Equal(t, v.WindowsLayout, v.PlatformLayout())
}

func TestIdentifiedDevice_Inputs_WhenWindows_ShouldLeaveKeyboardsOut(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{
		Known: SideKeyboardKeypad,
		Interfaces: []Info{
			{UsagePage: 0x0001, Usage: 0x0006, Path: "keyboard"},
			{UsagePage: 0x000c, Usage: 0x0001, Path: "consumer"},
		},
	}

	got := d.Inputs()

	require.Len(t, got, 1)
	assert.Equal(t, RoleConsumer, got[0].Role)
}
