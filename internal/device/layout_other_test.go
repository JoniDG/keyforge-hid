//go:build !windows

package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendorInterface_PlatformLayout_WhenNotWindows_ShouldReturnLayout(t *testing.T) {
	t.Parallel()
	v := SideKeyboardKeypad.Vendor

	assert.Equal(t, v.Layout, v.PlatformLayout())
}

func TestIdentifiedDevice_Inputs_WhenNotWindows_ShouldKeepKeyboards(t *testing.T) {
	t.Parallel()
	d := IdentifiedDevice{
		Known: SideKeyboardKeypad,
		Interfaces: []Info{
			{UsagePage: 0x0001, Usage: 0x0006, Path: "keyboard"},
			{UsagePage: 0x000c, Usage: 0x0001, Path: "consumer"},
		},
	}

	got := d.Inputs()

	require.Len(t, got, 2)
	assert.Equal(t, RoleKeyboard, got[0].Role)
	assert.Equal(t, RoleConsumer, got[1].Role)
}
