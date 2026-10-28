//go:build !windows

package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVendorInterface_PlatformLayout_WhenNotWindows_ShouldReturnLayout(t *testing.T) {
	t.Parallel()
	v := SideKeyboardKeypad.Vendor

	assert.Equal(t, v.Layout, v.PlatformLayout())
}
