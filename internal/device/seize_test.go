package device

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnableSeize_OnSupportedPlatform_ShouldReturnNil(t *testing.T) {
	t.Parallel()

	err := EnableSeize()

	if PlatformSeizeSupport().Supported {
		require.NoError(t, err)
	} else {
		require.ErrorIs(t, err, ErrSeizeNotImplemented)
	}
}

func TestEnableSeize_ShouldBeIdempotent(t *testing.T) {
	t.Parallel()

	first := EnableSeize()
	second := EnableSeize()

	assert.Equal(t, first, second, "calling EnableSeize twice must yield the same result")
}

func TestPlatformSeizeSupport_NoteShouldBeNonEmpty(t *testing.T) {
	t.Parallel()

	support := PlatformSeizeSupport()

	assert.NotEmpty(t, support.Note, "Note should describe the current platform's seize state")
}

func TestPlatformSeizeSupport_ShouldMatchCurrentGOOS(t *testing.T) {
	t.Parallel()

	support := PlatformSeizeSupport()

	switch runtime.GOOS {
	case "darwin":
		assert.True(t, support.Supported, "darwin must report supported=true")
		assert.Contains(t, support.Note, "darwin")
	case "linux":
		assert.False(t, support.Supported, "linux stub must report supported=false")
		assert.Contains(t, support.Note, "linux")
	case "windows":
		assert.False(t, support.Supported, "windows stub must report supported=false")
		assert.Contains(t, support.Note, "windows")
	default:
		assert.False(t, support.Supported, "unknown platform must report supported=false")
	}
}
